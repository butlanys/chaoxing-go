#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把 ddddocr 的 common_old.onnx 转成 chaoxing-go 内嵌的紧凑模型文件。

**重要**：Python 版 chaoxing 用的是 `DdddOcr(show_ad=False)`，走的是
model_loader.load_ocr_model(old=False, beta=False) -> **common_old.onnx**。
该文件是 ORT 动态量化版（ConvInteger / DynamicQuantizeLSTM / MatMulInteger），
与同目录的 common.onnx（浮点 beta 模型）**不是同一个模型**：实测 394 张真实验证码
上两者输出 0 张一致，common.onnx 给出的是乱码。所以必须以 common_old.onnx 为准。

做三件事：
1. 断言图结构与预期一致（ddddocr 换模型时立刻报错，而不是静默出错）
2. 把输出层 MatMulInteger 的权重按列裁到实际需要的字符集
   （超星验证码前端正则就是 /[0-9a-zA-Z]{4}/），8.02MB -> 62KB
3. 原样保留 int8/uint8 权重（本来就是 1 字节，无需再压缩）

用法:
    python3 scripts/convert_ddddocr.py --out internal/ocr/data/ddddocr.bin
    python3 scripts/convert_ddddocr.py --out /tmp/x.bin --charset full
"""
import argparse
import collections
import os
import string
import struct
import sys

import numpy as np
import onnx
import onnx.numpy_helper as nh

sys.path.insert(0, "/usr/local/lib/python3.13/dist-packages")

DEFAULT_ONNX = "/usr/local/lib/python3.13/dist-packages/ddddocr/common.onnx"

# 两种模型及其配套字符集。**模型与字符集是严格配套的**：
# 用错字符集会得到"能跑但全是乱码"的结果（我一开始就踩了这个坑）。
MODELS = {
    # 浮点模型：DdddOcr(beta=True) 使用，配 CHARSET_BETA
    "float": ("/usr/local/lib/python3.13/dist-packages/ddddocr/common.onnx", "beta"),
    # 量化模型：DdddOcr(show_ad=False) 使用（Python 原版 chaoxing 的配置），配 CHARSET_OLD
    "quant": ("/usr/local/lib/python3.13/dist-packages/ddddocr/common_old.onnx", "old"),
}

# 浮点模型的算子分布
EXPECTED_OPS_FLOAT = {
    "Conv": 21, "Mul": 13, "Sigmoid": 11, "Constant": 9, "Unsqueeze": 9,
    "Add": 8, "Shape": 8, "Gather": 8, "Concat": 5, "Reshape": 4,
    "Transpose": 2, "ConstantOfShape": 2, "LSTM": 1, "Gemm": 1,
}

# 期望的算子分布。ddddocr 换模型时会变，此时直接报错要求人工确认。
EXPECTED_OPS = {
    "Mul": 77, "Cast": 43, "Add": 30, "Reshape": 25,
    "DynamicQuantizeLinear": 22, "Div": 21, "Floor": 21, "ConvInteger": 21,
    "Sigmoid": 11, "Unsqueeze": 5, "Gather": 4, "Concat": 4,
    "Transpose": 2, "Shape": 2, "ConstantOfShape": 2,
    "DynamicQuantizeLSTM": 1, "MatMulInteger": 1,
}

# 输出层：浮点模型是 Gemm 的 [8210, 1024] 权重（按行裁剪），
# 量化模型是 MatMulInteger 的 [1024, 8210] 权重（按列裁剪）
OUT_W_FLOAT = "135"
OUT_B_FLOAT = "136"

# 输出层：权重按列裁剪（[1024, 8210]，8210 才是类别维），偏置按元素裁剪
OUT_W = "135_quantized"
OUT_B = "136"

DT_F32, DT_F16, DT_I64, DT_U8, DT_I8 = 0, 1, 2, 3, 4

ATTR_INT, ATTR_FLOAT, ATTR_STRING, ATTR_INTS, ATTR_FLOATS, ATTR_TENSOR = 1, 2, 3, 4, 5, 6


class Writer:
    def __init__(self):
        self.buf = bytearray()

    def u8(self, v):
        self.buf += struct.pack("<B", v)

    def u32(self, v):
        self.buf += struct.pack("<I", v)

    def u64(self, v):
        self.buf += struct.pack("<Q", v)

    def i64(self, v):
        self.buf += struct.pack("<q", v)

    def f32(self, v):
        self.buf += struct.pack("<f", v)

    def raw(self, b):
        self.buf += b

    def s(self, text):
        b = text.encode("utf-8")
        if len(b) > 255:
            raise ValueError(f"字符串过长: {len(b)}")
        self.u8(len(b))
        self.raw(b)

    def strings(self, items):
        self.u32(len(items))
        for it in items:
            self.s(it)


def encode_tensor(arr, use_f16=False, f16_min_elems=512):
    """按 dtype 序列化。浮点模型的大张量可转 float16 存储（加载时展开回 float32），
    体积减半；量化模型的 int8/uint8 权重本来就是 1 字节，不做转换。"""
    if arr.dtype in (np.float32, np.float64):
        if use_f16 and arr.size >= f16_min_elems:
            return arr.astype(np.float16).tobytes(), DT_F16
        return arr.astype(np.float32).tobytes(), DT_F32
    if arr.dtype == np.int64:
        return arr.astype(np.int64).tobytes(), DT_I64
    if arr.dtype == np.uint8:
        return arr.astype(np.uint8).tobytes(), DT_U8
    if arr.dtype == np.int8:
        return arr.astype(np.int8).tobytes(), DT_I8
    if arr.dtype == np.float16:
        return arr.astype(np.float16).tobytes(), DT_F16
    raise SystemExit(f"不支持的权重类型 {arr.dtype}")


def build_charset(charset, mode):
    """返回 (保留的原始类索引, 新字符集)。索引 0 恒为 CTC blank。"""
    if mode == "full":
        return list(range(len(charset))), list(charset)

    want = string.digits + string.ascii_letters
    pos = {c: i for i, c in enumerate(charset)}
    missing = [c for c in want if c not in pos]
    if missing:
        raise SystemExit(f"字符集里缺少这些字符, 无法裁剪: {missing}")
    keep = [0] + [pos[c] for c in want]          # 0 号是 blank
    return keep, [charset[i] for i in keep]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", choices=["float", "quant"], default="float",
                    help="float=DdddOcr(beta=True) 用的 common.onnx（推荐，无量化放大）; "
                         "quant=DdddOcr(show_ad=False) 用的 common_old.onnx（Python 原版配置）")
    ap.add_argument("--onnx", default=None, help="手动指定 onnx 路径，覆盖 --model")
    ap.add_argument("--out", required=True)
    ap.add_argument("--charset", choices=["ascii", "full"], default="ascii",
                    help="ascii=裁剪到 [0-9a-zA-Z]+blank（超星验证码用）; full=保留 8210 类")
    ap.add_argument("--f16", action="store_true",
                    help="浮点模型的大张量以 float16 存储，体积减半（加载时展开回 float32）")
    args = ap.parse_args()

    default_onnx, charset_kind = MODELS[args.model]
    onnx_path = args.onnx or default_onnx
    m = onnx.load(onnx_path)
    g = m.graph

    # ---------- 1. 架构断言 ----------
    got = collections.Counter(n.op_type for n in g.node)
    want_ops = EXPECTED_OPS_FLOAT if args.model == "float" else EXPECTED_OPS
    if dict(got) != want_ops:
        print("!! 模型结构与预期不符，请人工确认后再更新 EXPECTED_OPS", file=sys.stderr)
        print("   实际:", dict(got), file=sys.stderr)
        print("   预期:", want_ops, file=sys.stderr)
        raise SystemExit(1)
    print(f"[1/5] 架构断言通过 ({args.model}): {len(g.node)} 个节点, {len(got)} 种算子")
    print(f"      onnx: {onnx_path}")

    inits = {p.name: nh.to_array(p) for p in g.initializer}

    # ---------- 2. 裁剪输出层 ----------
    from ddddocr import charsets as CS
    if charset_kind == "beta":
        full_charset = list(CS.CHARSET_BETA)
    else:
        full_charset = list(CS.CHARSET_OLD)
    keep, charset = build_charset(full_charset, args.charset)

    if args.model == "float":
        w_name, b_name = OUT_W_FLOAT, OUT_B_FLOAT
        W = inits[w_name]
        if W.shape[0] != len(full_charset):
            raise SystemExit(f"输出层形状 {W.shape} 与字符集 {len(full_charset)} 不符")
        before = W.nbytes + inits[b_name].nbytes
        if args.charset != "full":
            # Gemm 权重是 [类别数, 特征维]，按行裁剪
            inits[w_name] = np.ascontiguousarray(W[keep, :])
            inits[b_name] = np.ascontiguousarray(inits[b_name][keep])
        after = inits[w_name].nbytes + inits[b_name].nbytes
    else:
        w_name, b_name = OUT_W, OUT_B
        W = inits[w_name]
        if W.shape[1] != len(full_charset):
            raise SystemExit(f"输出层形状 {W.shape} 与字符集 {len(full_charset)} 不符")
        before = W.nbytes + inits[b_name].nbytes
        if args.charset != "full":
            # MatMulInteger 权重是 [特征维, 类别数]，按列裁剪
            inits[w_name] = np.ascontiguousarray(W[:, keep])
            inits[b_name] = np.ascontiguousarray(inits[b_name][keep])
        after = inits[w_name].nbytes + inits[b_name].nbytes

    print(f"[2/5] 输出层裁剪: {len(full_charset)} 类 -> {len(charset)} 类 "
          f"({before/1048576:.2f}MB -> {after/1024:.1f}KB), 字符集来源 CHARSET_{charset_kind.upper()}")

    # ---------- 3. 序列化 ----------
    w = Writer()
    w.raw(b"CXOCR\x00\x00\x00")
    w.u32(1)

    w.u32(len(inits))
    total = 0
    for name, arr in inits.items():
        w.s(name)
        w.u8(arr.ndim)
        for d in arr.shape:
            w.i64(int(d))
        data, dt = encode_tensor(arr, args.f16)
        w.u8(dt)
        w.u64(len(data))
        w.raw(data)
        total += len(data)

    attr_proto = onnx.AttributeProto
    w.u32(len(g.node))
    for n in g.node:
        w.s(n.op_type)
        # 输入按原样保序写出（LSTM 有 '' 空占位，必须保留位置）
        w.u8(len(n.input))
        for i in n.input:
            w.s(i)
        w.u8(len(n.output))
        for o in n.output:
            w.s(o)
        w.u8(len(n.attribute))
        for a in n.attribute:
            w.s(a.name)
            if a.type == attr_proto.INT:
                w.u8(ATTR_INT); w.i64(a.i)
            elif a.type == attr_proto.FLOAT:
                w.u8(ATTR_FLOAT); w.f32(a.f)
            elif a.type == attr_proto.STRING:
                w.u8(ATTR_STRING); w.s(a.s.decode("utf-8"))
            elif a.type == attr_proto.INTS:
                w.u8(ATTR_INTS); w.u32(len(a.ints))
                for v in a.ints:
                    w.i64(v)
            elif a.type == attr_proto.FLOATS:
                w.u8(ATTR_FLOATS); w.u32(len(a.floats))
                for v in a.floats:
                    w.f32(v)
            elif a.type == attr_proto.TENSOR:
                t = nh.to_array(a.t)
                w.u8(ATTR_TENSOR)
                w.u8(t.ndim)
                for d in t.shape:
                    w.i64(int(d))
                data, dt = encode_tensor(t, args.f16)
                w.u8(dt)
                w.u64(len(data))
                w.raw(data)
            else:
                raise SystemExit(f"不支持的属性类型 {a.type} (节点 {n.op_type}.{a.name})")

    w.strings([i.name for i in g.input])
    w.strings([o.name for o in g.output])
    w.u32(len(charset))
    for c in charset:
        w.s(c)

    os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
    with open(args.out, "wb") as f:
        f.write(w.buf)
    print(f"[3/5] 已写出 {args.out}: {len(w.buf)} 字节 ({len(w.buf)/1048576:.2f}MB)")
    print(f"      权重净字节 {total/1048576:.2f}MB, 字符集 {len(charset)} 项")

    big = sorted(inits.items(), key=lambda kv: -kv[1].nbytes)[:6]
    print("[4/5] 最大权重:")
    for name, arr in big:
        print(f"      {name:<22s} {str(arr.shape):<18s} {arr.nbytes/1048576:6.2f}MB {arr.dtype}")

    # ---------- 5. 导出裁剪后的 onnx，供无损性验证 ----------
    if args.charset != "full":
        out_onnx = os.path.splitext(args.out)[0] + ".pruned.onnx"
        new_inits = []
        for p in g.initializer:
            if p.name == w_name:
                new_inits.append(nh.from_array(inits[w_name], name=p.name))
            elif p.name == b_name:
                new_inits.append(nh.from_array(inits[b_name], name=p.name))
            else:
                new_inits.append(p)
        del g.initializer[:]
        g.initializer.extend(new_inits)
        onnx.save(m, out_onnx)
        print(f"[5/5] 已导出裁剪模型用于验证: {out_onnx}")
    else:
        print("[5/5] 未裁剪, 跳过验证模型导出")


if __name__ == "__main__":
    main()

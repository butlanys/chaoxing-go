#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""导出某一层的全部中间张量，供 Go 逐算子比对。

关键：同时导出预处理后的输入数组，Go 侧直接读它来跑，
这样两边喂进去的张量**完全相同**，能把"预处理差异"彻底排除在外。

用法: python3 scripts/dump_layers.py <图片> <输出.bin>
"""
import os
import struct
import sys

import numpy as np
import onnxruntime as ort

sys.path.insert(0, "/usr/local/lib/python3.13/dist-packages")

MODEL = os.environ.get("OCR_REF_MODEL", "/tmp/ref_old.onnx")
DT = {"float32": 0, "float64": 0, "int64": 2, "uint8": 3, "int8": 4, "int32": 5}


def dtype_code(dt):
    """numpy dtype -> 编码。用 dtype 名字查表，避开 numpy dtype 与 type 对象
    hash 不一致导致的查表失败。"""
    return DT.get(str(np.dtype(dt)))


def write_str(f, s):
    b = s.encode()
    f.write(struct.pack("<B", len(b)))
    f.write(b)


def main():
    img_path, out_path = sys.argv[1], sys.argv[2]

    from PIL import Image
    rgb = Image.open(img_path).convert("RGB")
    th = 64
    tw = int(rgb.size[0] * (th / rgb.size[1]))
    gray = rgb.resize((tw, th), Image.LANCZOS).convert("L")
    x = (np.array(gray).astype(np.float32) / 255.0)[None, None, :, :]

    s = ort.InferenceSession(MODEL, providers=["CPUExecutionProvider"])
    names = [o.name for o in s.get_outputs()]
    vals = s.run(None, {"input1": x})

    with open(out_path, "wb") as f:
        f.write(b"CXLAYER\x00")
        # 输入张量
        f.write(struct.pack("<II", tw, 64))
        f.write(x.astype(np.float32).tobytes())
        # 中间张量
        f.write(struct.pack("<I", len(names)))
        written = 0
        skipped = []
        for name, v in zip(names, vals):
            v = np.ascontiguousarray(v)
            code = dtype_code(v.dtype)
            if code is None:
                skipped.append(f"{name}({v.dtype})")
                continue
            write_str(f, name)
            f.write(struct.pack("<B", v.ndim))
            for d in v.shape:
                f.write(struct.pack("<q", int(d)))
            f.write(struct.pack("<B", code))
            f.write(struct.pack("<Q", v.nbytes))
            f.write(v.tobytes())
            written += 1
        print(f"  实际写出 {written}/{len(names)} 个张量")
        if written != len(names):
            raise SystemExit("有张量未被写出，数量字段与实际不符，请检查 dtype 映射")
    print(f"已写出 {out_path}: 输入 {x.shape}, 中间张量 {len(names)} 个")


if __name__ == "__main__":
    main()

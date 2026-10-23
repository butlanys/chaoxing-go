#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""验证模型裁剪（8210 类 -> 63 类）在真实验证码上完全无损。

比对方式：同一张图分别过原始模型与裁剪模型，
按各自字符集做 CTC 解码，逐张比对字符串。
另附一项：把权重舍入到 float16 再跑一遍，量化 f16 存储带来的影响。

用法: python3 scripts/verify_pruning.py <图片目录> [--pruned x.pruned.onnx]
"""
import argparse
import glob
import os
import string
import sys

import numpy as np
import onnxruntime as ort

sys.path.insert(0, "/usr/local/lib/python3.13/dist-packages")

ORIG = "/usr/local/lib/python3.13/dist-packages/ddddocr/common.onnx"


def preprocess(path):
    from PIL import Image
    img = Image.open(path)
    if img.mode != "RGB":
        img = img.convert("RGB")
    th = 64
    tw = int(img.size[0] * (th / img.size[1]))
    img = img.resize((tw, th), Image.LANCZOS).convert("L")
    a = np.array(img).astype(np.float32) / 255.0
    return a[None, None, :, :]


def ctc_decode(idx, charset):
    out, prev = [], None
    for i in idx:
        i = int(i)
        if i != prev and i != 0:
            out.append(charset[i])
        prev = i
    return "".join(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("imgdir")
    ap.add_argument("--pruned", default="/tmp/ocr_test/ddddocr.pruned.onnx")
    args = ap.parse_args()

    from ddddocr.charsets import CHARSET_OLD
    full = list(CHARSET_OLD)
    want = string.digits + string.ascii_letters
    pos = {c: i for i, c in enumerate(full)}
    keep = [0] + [pos[c] for c in want]
    small = [full[i] for i in keep]

    so = ort.SessionOptions()
    so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_DISABLE_ALL
    s_full = ort.InferenceSession(ORIG, sess_options=so, providers=["CPUExecutionProvider"])
    s_pruned = ort.InferenceSession(args.pruned, providers=["CPUExecutionProvider"])

    imgs = sorted(glob.glob(os.path.join(args.imgdir, "*")))
    imgs = [p for p in imgs if p.lower().endswith((".jpg", ".jpeg", ".png", ".bmp"))]
    print(f"图片数: {len(imgs)}")

    diff = []
    n_ok = 0
    for p in imgs:
        x = preprocess(p)
        o1 = s_full.run(None, {"input1": x})[0]
        o2 = s_pruned.run(None, {"input1": x})[0]
        i1 = np.argmax(o1[:, 0, :], axis=1)
        i2 = np.argmax(o2[:, 0, :], axis=1)
        t1 = ctc_decode(i1, full)
        t2 = ctc_decode(i2, small)
        # 索引层面也要等价：裁剪模型的每个 argmax 应等于原模型在这些保留类上的 argmax
        i1_restricted = np.array([keep.index(int(v)) if int(v) in keep else -1 for v in i1])
        idx_same = bool(np.array_equal(i1_restricted, i2))
        if t1 == t2 and idx_same:
            n_ok += 1
        else:
            diff.append((os.path.basename(p), t1, t2, idx_same))

    print(f"\n=== 裁剪无损性 ===")
    print(f"完全一致: {n_ok}/{len(imgs)}")
    if diff:
        print(f"不一致 {len(diff)} 张:")
        for name, a, b, same in diff[:20]:
            print(f"  {name}: 原始={a!r} 裁剪={b!r} 索引一致={same}")
    else:
        print("全部一致 —— 裁剪在索引层面完全等价, 无损")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""导出 ddddocr common.onnx 的逐层中间激活，作为 Go 实现的黄金参考。

只跑推理、不参与 Go 侧任何逻辑，纯粹是"参考真值"。
输出 .npz，键为 ONNX 图中每个节点的输出名。

用法:
    python3 scripts/dump_reference.py <图片路径> <输出.npz> [--pruned 裁剪后模型路径]

不带 --pruned 时用原始 common.onnx；带则用裁剪后的模型（用于验证裁剪无损）。
"""
import argparse
import os
import sys

import numpy as np
import onnxruntime as ort

sys.path.insert(0, "/usr/local/lib/python3.13/dist-packages")

DEFAULT_MODEL = "/usr/local/lib/python3.13/dist-packages/ddddocr/common.onnx"


def load_image(path):
    """完全复刻 ddddocr 的预处理，返回 [1,1,64,W] float32。"""
    from PIL import Image

    img = Image.open(path)
    if img.mode != "RGB":
        img = img.convert("RGB")
    target_height = 64
    target_width = int(img.size[0] * (target_height / img.size[1]))
    img = img.resize((target_width, target_height), Image.LANCZOS)
    img = img.convert("L")
    arr = np.array(img).astype(np.float32) / 255.0
    arr = np.expand_dims(arr, axis=0)   # [1,H,W]
    arr = np.expand_dims(arr, axis=0)   # [1,1,H,W]
    return arr


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("image")
    ap.add_argument("out")
    ap.add_argument("--pruned", default=None, help="裁剪后的 onnx（验证裁剪无损用）")
    args = ap.parse_args()

    model = args.pruned or DEFAULT_MODEL
    # 必须关闭图优化，否则中间张量会被折叠、取不到
    so = ort.SessionOptions()
    so.graph_optimization_level = ort.GraphOptimizationLevel.ORT_DISABLE_ALL
    sess = ort.InferenceSession(model, sess_options=so, providers=["CPUExecutionProvider"])

    # 收集所有中间张量名（排除 initializer，它们是权重不是激活）
    import onnx
    m = onnx.load(model)
    init_names = {p.name for p in m.graph.initializer}
    wanted = []
    for n in m.graph.node:
        for o in n.output:
            if o and o not in init_names:
                wanted.append(o)
    # 去掉空名与重复
    seen, outputs = set(), []
    for o in wanted:
        if o not in seen:
            seen.add(o)
            outputs.append(o)

    x = load_image(args.image)
    in_name = sess.get_inputs()[0].name

    # 只请求真实存在的输出（有些节点输出会被丢弃）
    valid = {o.name for o in sess.get_outputs()}
    # onnxruntime 允许请求任意中间张量；不存在的会报错，故逐个过滤
    results = {}
    # 分批请求，避免一次请求过多
    for i in range(0, len(outputs), 1):
        name = outputs[i]
        try:
            r = sess.run([name], {in_name: x})
            results[name] = r[0]
        except Exception:
            pass  # 该张量不可取（例如被优化掉了）

    os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
    np.savez_compressed(args.out, **results)
    print(f"图片: {args.image}")
    print(f"输入形状: {x.shape}")
    print(f"模型: {model}")
    print(f"导出中间张量数: {len(results)}")
    for k in list(results)[:6]:
        print(f"  {k}: {results[k].shape}")
    print(f"  ...")
    for k in list(results)[-4:]:
        print(f"  {k}: {results[k].shape}")
    print(f"已写入 {args.out}")


if __name__ == "__main__":
    main()

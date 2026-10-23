#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把 common.onnx 的全部中间张量提升为图输出，得到"可逐层取数"的参考模型。

onnxruntime 默认不让取中间张量（Invalid output name），
onnx.utils.extract_model 又要求 value_info 带 shape。
这里先做 shape inference 拿到全部类型信息，再直接追加为 graph.output。

用法: python3 scripts/export_intermediates.py <输入.onnx> <输出.onnx>
"""
import sys

import onnx
from onnx import shape_inference


def main():
    src, dst = sys.argv[1], sys.argv[2]
    m = onnx.load(src)
    init_names = {p.name for p in m.graph.initializer}

    mi = shape_inference.infer_shapes(m, strict_mode=False)
    have = {v.name: v for v in list(mi.graph.value_info)
            + list(mi.graph.input) + list(mi.graph.output)}

    inter = []
    seen = set()
    for n in mi.graph.node:
        for o in n.output:
            if o and o not in init_names and o not in seen:
                seen.add(o)
                inter.append(o)

    out_names = {o.name for o in mi.graph.output}
    added = 0
    for name in inter:
        if name in out_names:
            continue
        vi = have.get(name)
        if vi is None:
            print(f"警告: {name} 无类型信息, 跳过")
            continue
        mi.graph.output.append(vi)
        added += 1

    onnx.save(mi, dst)
    print(f"输入: {src}")
    print(f"输出: {dst}")
    print(f"新增图输出: {added} (中间张量共 {len(inter)})")


if __name__ == "__main__":
    main()

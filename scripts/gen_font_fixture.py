#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""交叉验证脚本: 用 fontTools + 原版 hash_glyph 生成参考数据, 供 Go 实现比对。

生成:
  internal/font/testdata/sample.ttf          —— 合成 TTF(含多个简单字形)
  internal/font/testdata/sample_hashes.json  —— 原版算法算出的 {字形名: md5}

用法: python3 scripts/gen_font_fixture.py
"""
import hashlib
import json
import os
import sys

from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen

HERE = os.path.dirname(os.path.abspath(__file__))
OUT_DIR = os.path.join(HERE, "..", "internal", "font", "testdata")


def hash_glyph(glyph):
    """原版 api/cxsecret_font.py 的 hash_glyph, 逐字照搬"""
    if glyph.numberOfContours <= 0:
        return ""

    pos_data = []
    last_index = 0

    for i in range(glyph.numberOfContours):
        end_point = glyph.endPtsOfContours[i]
        for j in range(last_index, end_point + 1):
            x, y = glyph.coordinates[j]
            flag = glyph.flags[j] & 0x01
            pos_data.append(f"{x}{y}{flag}")
        last_index = end_point + 1

    pos_bin = "".join(pos_data)
    return hashlib.md5(pos_bin.encode()).hexdigest()


# 各字形的轮廓点(绝对坐标)。刻意覆盖:
#  - 单轮廓/多轮廓
#  - 坐标增量落在 int8 与 int16 两种编码区间
#  - 出现重复坐标(触发 flag 的 x/y same 位)
SHAPES = {
    "uni4E00": [(0, 0), (500, 0), (500, 100), (0, 100)],
    "uni4E8C": [(100, 0), (400, 0), (400, 50), (100, 50)],
    "uni4E09": [
        (0, 0), (300, 0), (300, 40), (0, 40),          # 第一轮廓
        (50, 100), (250, 100), (250, 140), (50, 140),  # 第二轮廓
    ],
    "uni4E5F": [(10, 10), (900, 10), (900, 900), (10, 900)],
}


def build_font(path):
    glyph_order = [".notdef"] + list(SHAPES.keys())
    fb = FontBuilder(1000, isTTF=True)
    fb.setupGlyphOrder(glyph_order)
    fb.setupCharacterMap({0x4E00: "uni4E00", 0x4E8C: "uni4E8C",
                          0x4E09: "uni4E09", 0x4E5F: "uni4E5F"})

    glyphs = {}
    pen = TTGlyphPen(None)
    glyphs[".notdef"] = pen.glyph()

    for name, coords in SHAPES.items():
        pen = TTGlyphPen(None)
        pen.moveTo(coords[0])
        for c in coords[1:]:
            pen.lineTo(c)
        pen.closePath()
        glyphs[name] = pen.glyph()

    fb.setupGlyf(glyphs)
    fb.setupHorizontalMetrics({n: (500, 0) for n in glyph_order})
    fb.setupHorizontalHeader(ascent=800, descent=-200)
    fb.setupNameTable({"familyName": "CxTestFont", "styleName": "Regular"})
    fb.setupOS2()
    fb.setupPost()
    fb.save(path)


def main():
    os.makedirs(OUT_DIR, exist_ok=True)
    ttf_path = os.path.join(OUT_DIR, "sample.ttf")
    build_font(ttf_path)

    from fontTools.ttLib.ttFont import TTFont
    with TTFont(ttf_path, lazy=False) as font:
        table = font["glyf"]
        ref = {}
        for name in table.glyphOrder:
            if name.startswith("uni"):
                h = hash_glyph(table.glyphs[name])
                if h:
                    ref[name] = h

    json_path = os.path.join(OUT_DIR, "sample_hashes.json")
    with open(json_path, "w", encoding="utf-8") as fp:
        json.dump(ref, fp, ensure_ascii=False, indent=2, sort_keys=True)

    print(f"TTF  -> {ttf_path} ({os.path.getsize(ttf_path)} 字节)")
    print(f"参考 -> {json_path}")
    for k, v in sorted(ref.items()):
        print(f"  {k}: {v}")
    if len(ref) != len(SHAPES):
        print(f"警告: 期望 {len(SHAPES)} 个字形, 实际 {len(ref)}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

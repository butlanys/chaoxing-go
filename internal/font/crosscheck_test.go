package font

import (
	"encoding/json"
	"os"
	"testing"
)

// 本文件是可选的真实字体交叉验证钩子（默认跳过，不影响常规 go test）。
//
// 用法：对系统里任意一份真实 TTF 导出 Go 侧的逐字形 hash / uni 映射表，
// 再用 Python 参考实现比对：
//
//	CHAOXING_REAL_FONT=/path/to/font.ttf CHAOXING_REAL_FONT_OUT=/tmp/go.json \
//	  go test -run TestDumpRealFontHashes ./internal/font/
//	# Python:
//	#   from fontTools.ttLib import TTFont; import api.cxsecret_font as cx
//	#   [cx.hash_glyph(t.glyphs[g]) for g in t.glyphOrder]  与 /tmp/go.json 逐条比较
//
//	CHAOXING_REAL_FONT=/path/to/font.ttf CHAOXING_REAL_FONT_MAP=/tmp/go_map.json \
//	  go test -run TestDumpRealFontMap ./internal/font/
//	# Python: api.cxsecret_font.font2map("/path/to/font.ttf") 与 /tmp/go_map.json 比较

// TestDumpRealFontHashes 导出逐字形(按 gid) hash。
func TestDumpRealFontHashes(t *testing.T) {
	in := os.Getenv("CHAOXING_REAL_FONT")
	out := os.Getenv("CHAOXING_REAL_FONT_OUT")
	if in == "" || out == "" {
		t.Skip("未设置 CHAOXING_REAL_FONT / CHAOXING_REAL_FONT_OUT，跳过真实字体导出")
	}
	data, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parseSFNT(data)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := f.tableData("head")
	maxp, _ := f.tableData("maxp")
	loca, _ := f.tableData("loca")
	glyf, _ := f.tableData("glyf")
	numGlyphs := be16(maxp[maxpNumGlyphsOff : maxpNumGlyphsOff+2])
	offsets, err := parseLoca(loca, numGlyphs, i16(head[headIndexToLocFormatOff:headIndexToLocFormatOff+2]))
	if err != nil {
		t.Fatal(err)
	}
	hashes := make([]string, numGlyphs)
	simple := 0
	for gid := 0; gid < numGlyphs; gid++ {
		g, err := parseGlyph(glyf[offsets[gid]:offsets[gid+1]])
		if err != nil {
			t.Fatalf("gid %d: %v", gid, err)
		}
		if g.NumberOfContours > 0 {
			simple++
		}
		hashes[gid] = HashGlyph(g)
	}
	buf, _ := json.Marshal(hashes)
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("numGlyphs=%d 简单字形=%d -> %s", numGlyphs, simple, out)
}

// TestDumpRealFontMap 导出 ParseTTFGlyphs 的完整 uni 映射表。
func TestDumpRealFontMap(t *testing.T) {
	in := os.Getenv("CHAOXING_REAL_FONT")
	out := os.Getenv("CHAOXING_REAL_FONT_MAP")
	if in == "" || out == "" {
		t.Skip("未设置 CHAOXING_REAL_FONT / CHAOXING_REAL_FONT_MAP，跳过真实字体映射导出")
	}
	data, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseTTFGlyphs(data)
	if err != nil {
		t.Fatal(err)
	}
	buf, _ := json.Marshal(m)
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("uni 字形数=%d -> %s", len(m), out)
}

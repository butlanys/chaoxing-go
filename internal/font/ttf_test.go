package font

import (
	"crypto/md5"
	"encoding/hex"
	"reflect"
	"testing"
)

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestParseTTFGlyphsGolden 解析合成字体，与 Python 参考实现算出的 hash 逐字比对。
// 同时验证：空字形(.notdef)、复合字形(uni4E10)、非 uni 名字(A) 都被正确过滤。
func TestParseTTFGlyphsGolden(t *testing.T) {
	for _, tc := range []struct {
		name string
		opt  testFontOpts
	}{
		{"短 loca + post + cmap", testFontOpts{withPost: true, withCmap: true}},
		{"长 loca + post + cmap", testFontOpts{longLoca: true, withPost: true, withCmap: true}},
		{"短 loca + 仅 post", testFontOpts{withPost: true}},
		{"长 loca + 仅 post", testFontOpts{longLoca: true, withPost: true}},
	} {
		ttf := buildTestTTF(tc.opt)
		got, err := ParseTTFGlyphs(ttf)
		if err != nil {
			t.Fatalf("[%s] ParseTTFGlyphs 失败: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got, goldenGlyphHashes) {
			t.Errorf("[%s] 解析结果 = %v, 期望 %v", tc.name, got, goldenGlyphHashes)
		}
		// 过滤校验
		for _, absent := range []string{".notdef", "A", "uni4E10"} {
			if _, ok := got[absent]; ok {
				t.Errorf("[%s] 不应包含字形 %q（空字形/复合字形/非 uni 名）", tc.name, absent)
			}
		}
	}
}

// TestHashInputRecomputed 用测试内独立拼接的点串复算 md5，确认 hash 输入格式与 Python 一致。
func TestHashInputRecomputed(t *testing.T) {
	for name, pointData := range testGlyphPointData {
		want := goldenGlyphHashes[name]
		if got := md5Hex(pointData); got != want {
			t.Errorf("%s: md5(%q) = %s, 期望 %s", name, pointData, got, want)
		}
	}
}

// TestParseTTFGlyphsCmapFallback 无 post 表时用 cmap 推导字形名（fontTools 同款兜底策略）。
func TestParseTTFGlyphsCmapFallback(t *testing.T) {
	got, err := ParseTTFGlyphs(buildTestTTF(testFontOpts{withCmap: true}))
	if err != nil {
		t.Fatalf("ParseTTFGlyphs 失败: %v", err)
	}
	for name, want := range goldenGlyphHashes {
		if got[name] != want {
			t.Errorf("cmap 兜底 %s = %q, 期望 %q", name, got[name], want)
		}
	}
	// glyphID 4 在 cmap 中映射到 0x41 -> 名字 "uni41"（post 表存在时它叫 "A" 会被过滤）
	if h, ok := got["uni41"]; !ok || h != md5Hex("001501001") {
		t.Errorf("cmap 兜底应包含 uni41 -> %s, 实际 %q (ok=%v)", md5Hex("001501001"), h, ok)
	}
	if _, ok := got[".notdef"]; ok {
		t.Errorf("cmap 未映射的 .notdef 不应出现")
	}
}

// TestParseTTFGlyphsNoNames 既无 post 也无 cmap 时返回空 map 且不报错。
func TestParseTTFGlyphsNoNames(t *testing.T) {
	got, err := ParseTTFGlyphs(buildTestTTF(testFontOpts{}))
	if err != nil {
		t.Fatalf("ParseTTFGlyphs 失败: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("无字形名表时应返回空 map, 实际 %v", got)
	}
}

// TestParseTTFGlyphsErrors 结构损坏时返回 error（不 panic）。
func TestParseTTFGlyphsErrors(t *testing.T) {
	valid := buildTestTTF(testFontOpts{withPost: true, withCmap: true})
	cases := []struct {
		name string
		data []byte
	}{
		{"空数据", nil},
		{"过短", []byte{0, 1, 0, 0}},
		{"截断的表目录", valid[:20]},
		{"截断的字体", valid[:len(valid)/2]},
	}
	for _, tc := range cases {
		if _, err := ParseTTFGlyphs(tc.data); err == nil {
			t.Errorf("[%s] 期望返回 error", tc.name)
		}
	}
}

// TestHashGlyphEdgeCases HashGlyph 的边界行为（对齐 Python hash_glyph）。
func TestHashGlyphEdgeCases(t *testing.T) {
	if got := HashGlyph(nil); got != "" {
		t.Errorf("nil 字形应返回空串, 实际 %q", got)
	}
	if got := HashGlyph(&Glyph{NumberOfContours: 0}); got != "" {
		t.Errorf("numberOfContours=0 应返回空串, 实际 %q", got)
	}
	// 复合字形
	if got := HashGlyph(&Glyph{NumberOfContours: -1}); got != "" {
		t.Errorf("复合字形应返回空串, 实际 %q", got)
	}
	// 手工字形：2 条轮廓 (0,0)(10,20) | (30,40)，on-curve 位分别为 1,0,1
	g := &Glyph{
		NumberOfContours: 2,
		EndPtsOfContours: []int{1, 2},
		Coords:           [][2]int{{0, 0}, {10, 20}, {30, 40}},
		Flags:            []byte{0x01, 0x02, 0x01},
	}
	want := md5Hex("001" + "10200" + "30401")
	if got := HashGlyph(g); got != want {
		t.Errorf("HashGlyph = %s, 期望 %s", got, want)
	}
	// 数据不完整时返回空串而不是 panic
	bad := &Glyph{NumberOfContours: 2, EndPtsOfContours: []int{1, 5}, Coords: [][2]int{{1, 2}}, Flags: []byte{1}}
	if got := HashGlyph(bad); got != "" {
		t.Errorf("数据不完整应返回空串, 实际 %q", got)
	}
}

// TestParseGlyphCoordinates 直接校验解析出的绝对坐标与 flags（含 repeat 与 int16 delta）。
func TestParseGlyphCoordinates(t *testing.T) {
	ttf := buildTestTTF(testFontOpts{withPost: true})
	f, err := parseSFNT(ttf)
	if err != nil {
		t.Fatal(err)
	}
	head, _ := f.tableData("head")
	maxp, _ := f.tableData("maxp")
	loca, _ := f.tableData("loca")
	glyf, _ := f.tableData("glyf")
	offsets, err := parseLoca(loca, be16(maxp[maxpNumGlyphsOff:maxpNumGlyphsOff+2]), i16(head[headIndexToLocFormatOff:headIndexToLocFormatOff+2]))
	if err != nil {
		t.Fatal(err)
	}
	wantCoords := map[int][][2]int{
		1: {{0, 0}, {100, 0}, {100, 50}},
		2: {{0, 0}, {100, 0}, {200, 0}, {300, 0}, {400, 50}},
		3: {{0, 0}, {600, 0}, {0, 800}},
		4: {{0, 0}, {50, 100}},
		6: {{0, 0}, {200, -200}},
	}
	for _, gid := range []int{1, 2, 3, 4, 6} {
		g, err := parseGlyph(glyf[offsets[gid]:offsets[gid+1]])
		if err != nil {
			t.Fatalf("gid %d 解析失败: %v", gid, err)
		}
		if !reflect.DeepEqual(g.Coords, wantCoords[gid]) {
			t.Errorf("gid %d 坐标 = %v, 期望 %v", gid, g.Coords, wantCoords[gid])
		}
		for i, fl := range g.Flags {
			if fl&0x01 == 0 {
				t.Errorf("gid %d 第 %d 点非 on-curve", gid, i)
			}
		}
	}
	// 复合字形
	g5, err := parseGlyph(glyf[offsets[5]:offsets[6]])
	if err != nil {
		t.Fatal(err)
	}
	if g5.NumberOfContours != -1 || len(g5.Coords) != 0 {
		t.Errorf("复合字形应只返回轮廓数, 实际 %+v", g5)
	}
	// 空字形
	g0, err := parseGlyph(glyf[offsets[0]:offsets[1]])
	if err != nil {
		t.Fatal(err)
	}
	if g0.NumberOfContours != 0 {
		t.Errorf("空字形 numberOfContours 应为 0, 实际 %d", g0.NumberOfContours)
	}
}

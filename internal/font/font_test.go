package font

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// 手工构造一份最小但合法的 TTF，用于验证解析器（不依赖任何外部字体文件）
//
// 字形布局（numGlyphs = 6）：
//   0 ".notdef"  空字形（loca 相邻偏移相等）
//   1 "uni4E00"  1 条轮廓 3 个点，全部 int8 delta，无 repeat
//   2 "uni4E8C"  2 条轮廓 5 个点，flags 使用 0x08 repeat 压缩
//   3 "uni4E09"  1 条轮廓 3 个点，含正/负 int16 delta
//   4 "A"        1 条轮廓 2 个点（名字不以 uni 开头，应被过滤）
//   5 "uni4E10"  复合字形（numberOfContours = -1，应被跳过）
// ---------------------------------------------------------------------------

type testFontOpts struct {
	longLoca bool // loca 用长格式
	withPost bool // 是否带 post 2.0 字形名表
	withCmap bool // 是否带 cmap
}

// 期望的 hash 输入串（按 contour 顺序把 x y flag&1 直接拼接）
var testGlyphPointData = map[string]string{
	"uni4E00": "00110001100501",
	"uni4E8C": "001100012000130001400501",
	"uni4E09": "0016000108001",
	"uni4E28": "001200-2001",
}

// goldenGlyphHashes 由 Python 参考实现（fontTools TTFont + api.cxsecret_font.hash_glyph，
// 以及 font2map(data-url) 两条路径）对同一份合成字体实际运行得到，作为跨实现对齐基准。
var goldenGlyphHashes = map[string]string{
	"uni4E00": "4c8bdc2455bec9d8cded8c11b07dfdf8",
	"uni4E8C": "9fa39d637ea34ca1c542ad1faa259116",
	"uni4E09": "a3ebfd68fba9749ae9564e74fe5f81cd",
	"uni4E28": "e0e9855bd41e894f18832e5300402e3f",
}

func be16b(v int) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(v))
	return b
}

func be16s(v int) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(int16(v)))
	return b
}

func be32b(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// simpleGlyphData 组装一个简单字形的 glyf 数据。
// flags 传入的已是「含 repeat 压缩」的原始编码字节，xBytes/yBytes 为对应的 delta 字节流。
func simpleGlyphData(endPts []int, flags, xBytes, yBytes []byte) []byte {
	var b bytes.Buffer
	b.Write(be16s(len(endPts)))             // numberOfContours
	b.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0}) // xMin yMin xMax yMax
	for _, e := range endPts {
		b.Write(be16b(e))
	}
	b.Write(be16b(0)) // instructionLength = 0
	b.Write(flags)
	b.Write(xBytes)
	b.Write(yBytes)
	return b.Bytes()
}

// compositeGlyphData 组装一个复合字形（应被跳过）。
func compositeGlyphData(componentGID int) []byte {
	var b bytes.Buffer
	b.Write(be16s(-1)) // numberOfContours < 0 表示复合字形
	b.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0})
	b.Write(be16b(0x0003)) // ARG_1_AND_2_ARE_WORDS | ARGS_ARE_XY_VALUES
	b.Write(be16b(componentGID))
	b.Write(be16s(0))
	b.Write(be16s(0))
	return b.Bytes()
}

func testGlyphData() [][]byte {
	return [][]byte{
		nil, // 0 .notdef 空字形
		// 1 uni4E00: (0,0) (100,0) (100,50)
		//   flags: dx=0&dy=0 -> 0x31; dx=+100&dy=0 -> 0x33; dx=0&dy=+50 -> 0x35
		simpleGlyphData([]int{2}, []byte{0x31, 0x33, 0x35}, []byte{100}, []byte{50}),
		// 2 uni4E8C: (0,0) (100,0) (200,0) (300,0) (400,50)
		//   0x31 | 0x3B(0x33 带 repeat 位, repeat=2) | 0x37
		simpleGlyphData([]int{1, 4}, []byte{0x31, 0x3B, 0x02, 0x37},
			[]byte{100, 100, 100, 100}, []byte{50}),
		// 3 uni4E09: (0,0) (600,0) (0,800)，正负 int16 delta
		//   p1: x=+600(int16) y 同前 -> 0x21; p2: x=-600(int16) y=+800(int16) -> 0x01
		simpleGlyphData([]int{2}, []byte{0x31, 0x21, 0x01},
			[]byte{0x02, 0x58, 0xFD, 0xA8}, []byte{0x03, 0x20}),
		// 4 A: (0,0) (50,100)
		simpleGlyphData([]int{1}, []byte{0x31, 0x37}, []byte{50}, []byte{100}),
		// 5 uni4E10 复合字形
		compositeGlyphData(1),
		// 6 uni4E28: (0,0) (200,-200)
		//   short delta 为「无符号字节 + 符号位」：200 需要按 unsigned 解释（>127）
		simpleGlyphData([]int{1}, []byte{0x31, 0x17}, []byte{200}, []byte{200}),
	}
}

func testGlyphNames() []string {
	return []string{".notdef", "uni4E00", "uni4E8C", "uni4E09", "A", "uni4E10", "uni4E28"}
}

// cmap 码点 -> glyphID
var testCmapPairs = [][2]int{{0x0041, 4}, {0x4E00, 1}, {0x4E28, 6}, {0x4E8C, 2}, {0x4E09, 3}}

type rawTable struct {
	tag  string
	data []byte
}

func buildTestHead(indexToLocFormat int) []byte {
	var b bytes.Buffer
	b.Write(be32b(0x00010000)) // version
	b.Write(be32b(0x00010000)) // fontRevision
	b.Write(be32b(0))          // checkSumAdjustment（装配后再回填）
	b.Write(be32b(0x5F0F3CF5)) // magicNumber
	b.Write(be16b(0))          // flags
	b.Write(be16b(1000))       // unitsPerEm
	b.Write(make([]byte, 16))  // created + modified
	b.Write(be16s(0))          // xMin
	b.Write(be16s(0))          // yMin
	b.Write(be16s(1000))       // xMax
	b.Write(be16s(1000))       // yMax
	b.Write(be16b(0))          // macStyle
	b.Write(be16b(8))          // lowestRecPPEM
	b.Write(be16s(2))          // fontDirectionHint
	b.Write(be16s(indexToLocFormat))
	b.Write(be16s(0)) // glyphDataFormat
	return b.Bytes()
}

func buildTestMaxp(numGlyphs int) []byte {
	var b bytes.Buffer
	b.Write(be32b(0x00010000)) // version 1.0
	b.Write(be16b(numGlyphs))
	for i := 0; i < 13; i++ { // maxPoints ... maxComponentDepth
		b.Write(be16b(0))
	}
	return b.Bytes()
}

func buildTestHhea(numGlyphs int) []byte {
	var b bytes.Buffer
	b.Write(be32b(0x00010000))
	b.Write(be16s(800))  // ascender
	b.Write(be16s(-200)) // descender
	b.Write(be16s(0))    // lineGap
	b.Write(be16b(600))  // advanceWidthMax
	b.Write(be16s(0))    // minLeftSideBearing
	b.Write(be16s(0))    // minRightSideBearing
	b.Write(be16s(600))  // xMaxExtent
	b.Write(be16s(1))    // caretSlopeRise
	b.Write(be16s(0))    // caretSlopeRun
	b.Write(be16s(0))    // caretOffset
	b.Write(make([]byte, 8))
	b.Write(be16s(0)) // metricDataFormat
	b.Write(be16b(numGlyphs))
	return b.Bytes()
}

func buildTestHmtx(numGlyphs int) []byte {
	var b bytes.Buffer
	for i := 0; i < numGlyphs; i++ {
		b.Write(be16b(600)) // advanceWidth
		b.Write(be16s(0))   // lsb
	}
	return b.Bytes()
}

// buildTestCmap 生成 cmap（一个 (3,1) format 4 子表，每个码点一个 segment）。
func buildTestCmap() []byte {
	pairs := append([][2]int{}, testCmapPairs...)
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	segCount := len(pairs) + 1 // 末尾 0xFFFF 段
	var sub bytes.Buffer
	length := 14 + segCount*8
	sub.Write(be16b(4)) // format
	sub.Write(be16b(length))
	sub.Write(be16b(0)) // language
	sub.Write(be16b(segCount * 2))
	sub.Write(be16b(0)) // searchRange（最小实现留 0）
	sub.Write(be16b(0)) // entrySelector
	sub.Write(be16b(0)) // rangeShift
	for _, p := range pairs {
		sub.Write(be16b(p[0])) // endCode
	}
	sub.Write(be16b(0xFFFF))
	sub.Write(be16b(0)) // reservedPad
	for _, p := range pairs {
		sub.Write(be16b(p[0])) // startCode
	}
	sub.Write(be16b(0xFFFF))
	for _, p := range pairs {
		sub.Write(be16s(p[1] - p[0])) // idDelta = gid - code
	}
	sub.Write(be16s(1)) // 0xFFFF 段: 0xFFFF + 1 = 0 -> gid 0
	for i := 0; i < segCount; i++ {
		sub.Write(be16b(0)) // idRangeOffset
	}

	var b bytes.Buffer
	b.Write(be16b(0))  // version
	b.Write(be16b(1))  // numTables
	b.Write(be16b(3))  // platformID
	b.Write(be16b(1))  // encodingID
	b.Write(be32b(12)) // 子表偏移
	b.Write(sub.Bytes())
	return b.Bytes()
}

// buildTestPost 生成 post 2.0（字形名全部走自定义名，index = 258 + i）。
func buildTestPost(names []string) []byte {
	var b bytes.Buffer
	b.Write(be32b(0x00020000)) // version 2.0
	b.Write(be32b(0))          // italicAngle
	b.Write(be16s(0))          // underlinePosition
	b.Write(be16s(0))          // underlineThickness
	b.Write(be32b(0))          // isFixedPitch
	b.Write(be32b(0))          // minMemType42
	b.Write(be32b(0))          // maxMemType42
	b.Write(be32b(0))          // minMemType1
	b.Write(be32b(0))          // maxMemType1
	b.Write(be16b(len(names))) // numberOfGlyphs
	for i := range names {
		b.Write(be16b(258 + i)) // glyphNameIndex
	}
	for _, n := range names {
		b.WriteByte(byte(len(n)))
		b.WriteString(n)
	}
	return b.Bytes()
}

func buildTestLoca(offsets []int, long bool) []byte {
	var b bytes.Buffer
	for _, o := range offsets {
		if long {
			b.Write(be32b(uint32(o)))
		} else {
			b.Write(be16b(o / 2))
		}
	}
	return b.Bytes()
}

// assembleSFNT 按 sfnt 规范装配表目录并回填 head.checkSumAdjustment。
func assembleSFNT(tables []rawTable) []byte {
	sort.Slice(tables, func(i, j int) bool { return tables[i].tag < tables[j].tag })
	n := len(tables)
	headerLen := 12 + n*16
	offsets := make([]int, n)
	pos := headerLen
	for i, t := range tables {
		offsets[i] = pos
		pos += len(t.data)
		for pos%4 != 0 {
			pos++
		}
	}
	buf := make([]byte, pos)
	binary.BigEndian.PutUint32(buf[0:4], 0x00010000) // sfntVersion
	binary.BigEndian.PutUint16(buf[4:6], uint16(n))
	// searchRange / entrySelector / rangeShift
	pow := 1
	entrySelector := 0
	for pow*2 <= n {
		pow *= 2
		entrySelector++
	}
	binary.BigEndian.PutUint16(buf[6:8], uint16(pow*16))
	binary.BigEndian.PutUint16(buf[8:10], uint16(entrySelector))
	binary.BigEndian.PutUint16(buf[10:12], uint16(n*16-pow*16))

	headOffset := -1
	for i, t := range tables {
		rec := 12 + i*16
		copy(buf[rec:rec+4], t.tag)
		binary.BigEndian.PutUint32(buf[rec+4:rec+8], tableChecksum(t.data))
		binary.BigEndian.PutUint32(buf[rec+8:rec+12], uint32(offsets[i]))
		binary.BigEndian.PutUint32(buf[rec+12:rec+16], uint32(len(t.data)))
		copy(buf[offsets[i]:], t.data)
		if t.tag == "head" {
			headOffset = offsets[i]
		}
	}
	// 回填 checkSumAdjustment = 0xB1B0AFBA - checksum(整个字体)
	if headOffset >= 0 {
		adj := uint32(0xB1B0AFBA) - tableChecksum(buf)
		binary.BigEndian.PutUint32(buf[headOffset+8:headOffset+12], adj)
	}
	return buf
}

func tableChecksum(data []byte) uint32 {
	var sum uint32
	padded := data
	if len(padded)%4 != 0 {
		padded = append(append([]byte{}, data...), make([]byte, 4-len(data)%4)...)
	}
	for i := 0; i < len(padded); i += 4 {
		sum += binary.BigEndian.Uint32(padded[i : i+4])
	}
	return sum
}

func buildTestTTF(opt testFontOpts) []byte {
	glyphData := testGlyphData()
	names := testGlyphNames()

	// glyf + loca（每个字形 4 字节对齐）
	offsets := make([]int, len(glyphData)+1)
	var glyf bytes.Buffer
	for i, gd := range glyphData {
		offsets[i] = glyf.Len()
		glyf.Write(gd)
		for glyf.Len()%4 != 0 {
			glyf.WriteByte(0)
		}
	}
	offsets[len(glyphData)] = glyf.Len()

	indexToLocFormat := 0
	if opt.longLoca {
		indexToLocFormat = 1
	}

	tables := []rawTable{
		{"head", buildTestHead(indexToLocFormat)},
		{"hhea", buildTestHhea(len(glyphData))},
		{"hmtx", buildTestHmtx(len(glyphData))},
		{"maxp", buildTestMaxp(len(glyphData))},
		{"loca", buildTestLoca(offsets, opt.longLoca)},
		{"glyf", glyf.Bytes()},
	}
	if opt.withPost {
		tables = append(tables, rawTable{"post", buildTestPost(names)})
	}
	if opt.withCmap {
		tables = append(tables, rawTable{"cmap", buildTestCmap()})
	}
	return assembleSFNT(tables)
}

// TestDumpTestTTF 仅在设置 CHAOXING_TTF_DUMP 时导出合成字体与 Go 侧 hash，
// 供 Python fontTools 交叉验证使用。
func TestDumpTestTTF(t *testing.T) {
	out := os.Getenv("CHAOXING_TTF_DUMP")
	if out == "" {
		t.Skip("未设置 CHAOXING_TTF_DUMP，跳过导出")
	}
	result := map[string]map[string]string{}
	for _, name := range []string{"short.ttf", "long.ttf"} {
		opt := testFontOpts{longLoca: name == "long.ttf", withPost: true, withCmap: true}
		raw := buildTestTTF(opt)
		if err := os.WriteFile(out+"/"+name, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := ParseTTFGlyphs(raw)
		if err != nil {
			t.Fatalf("%s 解析失败: %v", name, err)
		}
		result[name] = m
	}
	buf, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(out+"/go_hashes.json", buf, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("已导出合成字体与 Go 侧 hash 到 %s", out)
}

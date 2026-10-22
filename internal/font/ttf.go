package font

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Glyph 是一个 TTF 简单字形（与 fontTools 的 Glyph 对齐）。
type Glyph struct {
	// NumberOfContours 轮廓数；<=0 表示空字形或复合字形
	NumberOfContours int
	// EndPtsOfContours 每条轮廓的结束点下标
	EndPtsOfContours []int
	// Coords 绝对坐标（已累加 delta），[i][0]=x, [i][1]=y
	Coords [][2]int
	// Flags 原始 flags 字节（已展开 repeat 压缩），hash 时取 &0x01
	Flags []byte
}

// 简单字形解析用到的 flags 位
const (
	flagOnCurve      = 0x01 // 点在曲线上（hash 只用这一位）
	flagXShortVector = 0x02
	flagYShortVector = 0x04
	flagRepeat       = 0x08
	flagXSameOrPos   = 0x10
	flagYSameOrPos   = 0x20
)

// HashGlyph 复刻 Python 版 hash_glyph（api/cxsecret_font.py:113）。
//
//	numberOfContours <= 0 -> ""
//	pos_data[i] = fmt.Sprintf("%d%d%d", x, y, flag&1)，全部拼接后取 md5 hex
func HashGlyph(g *Glyph) string {
	if g == nil || g.NumberOfContours <= 0 {
		return ""
	}
	var b strings.Builder
	lastIndex := 0
	for i := 0; i < g.NumberOfContours; i++ {
		if i >= len(g.EndPtsOfContours) {
			break
		}
		endPoint := g.EndPtsOfContours[i]
		for j := lastIndex; j <= endPoint; j++ {
			if j >= len(g.Coords) || j >= len(g.Flags) {
				// 数据不完整，按 Python 会 IndexError；这里保守中止，不 panic
				return ""
			}
			b.WriteString(strconv.Itoa(g.Coords[j][0]))
			b.WriteString(strconv.Itoa(g.Coords[j][1]))
			b.WriteString(strconv.Itoa(int(g.Flags[j] & flagOnCurve)))
		}
		lastIndex = endPoint + 1
	}
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// 最小 TTF 解析器（仅标准库 encoding/binary）
// ---------------------------------------------------------------------------

type sfntTable struct {
	offset uint32
	length uint32
}

type sfntFont struct {
	data   []byte
	tables map[string]sfntTable
}

func be16(b []byte) int    { return int(binary.BigEndian.Uint16(b)) }
func be32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }
func i16(b []byte) int     { return int(int16(binary.BigEndian.Uint16(b))) }

// parseSFNT 解析 sfnt 表目录（TTF/OTF 共用结构）。
func parseSFNT(data []byte) (*sfntFont, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("字体数据过短(%d 字节)，不是合法 TTF", len(data))
	}
	numTables := be16(data[4:6])
	if 12+numTables*16 > len(data) {
		return nil, fmt.Errorf("表目录越界: numTables=%d", numTables)
	}
	f := &sfntFont{data: data, tables: make(map[string]sfntTable, numTables)}
	for i := 0; i < numTables; i++ {
		rec := 12 + i*16
		tag := string(data[rec : rec+4])
		off := be32(data[rec+8 : rec+12])
		length := be32(data[rec+12 : rec+16])
		if int(off) > len(data) {
			return nil, fmt.Errorf("表 %q 偏移越界: %d", tag, off)
		}
		f.tables[tag] = sfntTable{offset: off, length: length}
	}
	return f, nil
}

// tableData 返回指定表的字节切片（长度按实际数据截断）。
func (f *sfntFont) tableData(tag string) ([]byte, bool) {
	t, ok := f.tables[tag]
	if !ok {
		return nil, false
	}
	end := int(t.offset + t.length)
	if end > len(f.data) {
		end = len(f.data)
	}
	if int(t.offset) > end {
		return nil, false
	}
	return f.data[t.offset:end], true
}

// indexToLocFormat / numGlyphs 等关键字段位置（TTF 规范固定偏移）
const (
	headIndexToLocFormatOff = 50
	maxpNumGlyphsOff        = 4
)

// parseLoca 解析 loca 表，返回 numGlyphs+1 个字形数据偏移。
func parseLoca(loca []byte, numGlyphs, indexToLocFormat int) ([]uint32, error) {
	want := numGlyphs + 1
	offsets := make([]uint32, 0, want)
	if indexToLocFormat == 0 { // 短格式: uint16，实际值需 *2
		if len(loca) < want*2 {
			return nil, fmt.Errorf("loca 表长度不足: 需要 %d 字节, 实际 %d", want*2, len(loca))
		}
		for i := 0; i < want; i++ {
			offsets = append(offsets, uint32(be16(loca[i*2:i*2+2]))*2)
		}
		return offsets, nil
	}
	// 长格式: uint32
	if len(loca) < want*4 {
		return nil, fmt.Errorf("loca 表长度不足: 需要 %d 字节, 实际 %d", want*4, len(loca))
	}
	for i := 0; i < want; i++ {
		offsets = append(offsets, be32(loca[i*4:i*4+4]))
	}
	return offsets, nil
}

// parseGlyph 解析单个字形的 glyf 数据。
// numberOfContours < 0（复合字形）或数据为空时只返回轮廓数，不做进一步解析。
func parseGlyph(b []byte) (*Glyph, error) {
	g := &Glyph{}
	if len(b) == 0 {
		// 空字形（loca 中相邻偏移相等），numberOfContours 视为 0
		return g, nil
	}
	if len(b) < 10 {
		return nil, fmt.Errorf("字形数据过短: %d 字节", len(b))
	}
	nc := i16(b[0:2])
	g.NumberOfContours = nc
	if nc <= 0 {
		// 复合字形直接跳过（Python 侧同样返回 ""）
		return g, nil
	}
	p := 10
	// endPtsOfContours
	if p+nc*2 > len(b) {
		return nil, fmt.Errorf("endPtsOfContours 越界")
	}
	endPts := make([]int, nc)
	for i := 0; i < nc; i++ {
		endPts[i] = be16(b[p+i*2 : p+i*2+2])
	}
	g.EndPtsOfContours = endPts
	p += nc * 2

	numPoints := endPts[nc-1] + 1
	if numPoints <= 0 {
		return g, nil
	}

	// instructionLength + instructions（跳过）
	if p+2 > len(b) {
		return nil, fmt.Errorf("instructionLength 越界")
	}
	instrLen := be16(b[p : p+2])
	p += 2 + instrLen
	if p > len(b) {
		return nil, fmt.Errorf("instructions 越界")
	}

	// flags（含 0x08 repeat 压缩），展开到 numPoints 个点
	flags := make([]byte, 0, numPoints)
	for len(flags) < numPoints {
		if p >= len(b) {
			return nil, fmt.Errorf("flags 数据不足: 已解析 %d/%d", len(flags), numPoints)
		}
		f := b[p]
		p++
		flags = append(flags, f)
		if f&flagRepeat != 0 {
			if p >= len(b) {
				return nil, fmt.Errorf("flags repeat 计数越界")
			}
			repeat := int(b[p])
			p++
			for k := 0; k < repeat && len(flags) < numPoints; k++ {
				flags = append(flags, f)
			}
		}
	}
	if len(flags) > numPoints {
		flags = flags[:numPoints]
	}

	// x 坐标 delta（short 形式为「无符号字节 + 符号位」，见 OpenType 规范与
	// fontTools decompileCoordinatesRaw 的 "B" 格式，不是 int8）
	xs := make([]int, numPoints)
	x := 0
	for i := 0; i < numPoints; i++ {
		f := flags[i]
		switch {
		case f&flagXShortVector != 0:
			if p >= len(b) {
				return nil, fmt.Errorf("x 坐标(short)越界")
			}
			dx := int(b[p])
			p++
			if f&flagXSameOrPos == 0 {
				dx = -dx
			}
			x += dx
		case f&flagXSameOrPos != 0:
			// 与上一个点相同，delta = 0
		default:
			if p+2 > len(b) {
				return nil, fmt.Errorf("x 坐标(int16)越界")
			}
			x += i16(b[p : p+2])
			p += 2
		}
		xs[i] = x
	}

	// y 坐标 delta
	ys := make([]int, numPoints)
	y := 0
	for i := 0; i < numPoints; i++ {
		f := flags[i]
		switch {
		case f&flagYShortVector != 0:
			if p >= len(b) {
				return nil, fmt.Errorf("y 坐标(short)越界")
			}
			dy := int(b[p])
			p++
			if f&flagYSameOrPos == 0 {
				dy = -dy
			}
			y += dy
		case f&flagYSameOrPos != 0:
		default:
			if p+2 > len(b) {
				return nil, fmt.Errorf("y 坐标(int16)越界")
			}
			y += i16(b[p : p+2])
			p += 2
		}
		ys[i] = y
	}

	g.Coords = make([][2]int, numPoints)
	for i := 0; i < numPoints; i++ {
		g.Coords[i] = [2]int{xs[i], ys[i]}
	}
	g.Flags = flags
	return g, nil
}

// ---------------------------------------------------------------------------
// 字形名（post 表 / cmap 兜底）
// ---------------------------------------------------------------------------

// post 表版本
const (
	postVersion2 = 0x00020000
	postVersion3 = 0x00030000
	// post 2.0 中 index < 258 的名字来自 Macintosh 标准名表，
	// 该表里不存在以 "uni" 开头的名字，因此这些下标直接忽略（不影响结果）。
	postStandardNamesCount = 258
)

// parsePostNames 解析 post 2.0 表，返回 glyphID -> 字形名。
// post 3.0（无名字）或不支持时返回 nil, false。
func parsePostNames(post []byte, numGlyphs int) (map[int]string, bool) {
	if len(post) < 32 {
		return nil, false
	}
	if be32(post[0:4]) != postVersion2 {
		return nil, false
	}
	// 32 字节头部之后: numberOfGlyphs(uint16) + glyphNameIndex[numGlyphs](uint16) + pascal 字符串
	if len(post) < 34 {
		return nil, false
	}
	n := be16(post[32:34])
	if n > numGlyphs {
		n = numGlyphs
	}
	idxStart := 34
	if idxStart+n*2 > len(post) {
		return nil, false
	}
	nameDataStart := idxStart + n*2
	// 先按顺序读出所有 pascal 字符串
	customNames := make([]string, 0, 8)
	p := nameDataStart
	for p < len(post) {
		l := int(post[p])
		p++
		if p+l > len(post) {
			break
		}
		customNames = append(customNames, string(post[p:p+l]))
		p += l
	}
	names := make(map[int]string, n)
	for i := 0; i < n; i++ {
		idx := be16(post[idxStart+i*2 : idxStart+i*2+2])
		if idx < postStandardNamesCount {
			// 标准 Mac 名，全部不以 "uni" 开头，忽略
			continue
		}
		k := int(idx) - postStandardNamesCount
		if k < 0 || k >= len(customNames) {
			continue
		}
		names[i] = customNames[k]
	}
	if len(names) == 0 {
		return nil, false
	}
	return names, true
}

// parseCmapNames 从 cmap 表推导 glyphID -> "uniXXXX" 名字（post 表不可用时的兜底）。
// 只处理 format 4（BMP）与 format 12（含增补平面）。
func parseCmapNames(cmap []byte, numGlyphs int) map[int]string {
	if len(cmap) < 4 {
		return nil
	}
	numSubtables := be16(cmap[2:4])
	best := -1
	bestScore := -1
	for i := 0; i < numSubtables; i++ {
		off := 4 + i*8
		if off+8 > len(cmap) {
			break
		}
		platformID := be16(cmap[off : off+2])
		encodingID := be16(cmap[off+2 : off+4])
		sub := int(be32(cmap[off+4 : off+8]))
		if sub+2 > len(cmap) {
			continue
		}
		format := be16(cmap[sub : sub+2])
		score := -1
		switch format {
		case 12:
			score = 30
		case 4:
			score = 20
		default:
			continue
		}
		// (3,10) / (3,1) / (0,x) 优先级更高
		if platformID == 3 && encodingID == 10 {
			score += 3
		} else if platformID == 3 && encodingID == 1 {
			score += 2
		} else if platformID == 0 {
			score++
		}
		if score > bestScore {
			bestScore = score
			best = sub
		}
	}
	if best < 0 {
		return nil
	}
	names := make(map[int]string, numGlyphs)
	add := func(gid, cp int) {
		if gid <= 0 || gid >= numGlyphs || cp <= 0 {
			return
		}
		if _, ok := names[gid]; ok {
			return // 保留第一个（码点最小）的映射
		}
		names[gid] = "uni" + strings.ToUpper(strconv.FormatInt(int64(cp), 16))
	}

	switch format := be16(cmap[best : best+2]); format {
	case 4:
		if best+14 > len(cmap) {
			return nil
		}
		segCountX2 := be16(cmap[best+6 : best+8])
		segCount := segCountX2 / 2
		if segCount == 0 {
			return nil
		}
		endBase := best + 14
		startBase := endBase + segCount*2 + 2
		deltaBase := startBase + segCount*2
		rangeBase := deltaBase + segCount*2
		if rangeBase+segCount*2 > len(cmap) {
			return nil
		}
		for s := 0; s < segCount; s++ {
			end := be16(cmap[endBase+s*2 : endBase+s*2+2])
			start := be16(cmap[startBase+s*2 : startBase+s*2+2])
			delta := be16(cmap[deltaBase+s*2 : deltaBase+s*2+2])
			ro := be16(cmap[rangeBase+s*2 : rangeBase+s*2+2])
			if start > end || end-start > 0xFFFF {
				continue
			}
			for cp := start; cp <= end; cp++ {
				if cp == 0xFFFF {
					continue
				}
				var gid int
				if ro == 0 {
					gid = (cp + delta) & 0xFFFF
				} else {
					gi := rangeBase + s*2 + ro + (cp-start)*2
					if gi+2 > len(cmap) {
						continue
					}
					g := be16(cmap[gi : gi+2])
					if g == 0 {
						continue
					}
					gid = (g + delta) & 0xFFFF
				}
				add(gid, cp)
			}
		}
	case 12:
		if best+16 > len(cmap) {
			return nil
		}
		nGroups := int(be32(cmap[best+12 : best+16]))
		base := best + 16
		for g := 0; g < nGroups; g++ {
			off := base + g*12
			if off+12 > len(cmap) {
				break
			}
			start := int(be32(cmap[off : off+4]))
			end := int(be32(cmap[off+4 : off+8]))
			startGID := int(be32(cmap[off+8 : off+12]))
			if end < start || end-start > 0x10FFFF {
				continue
			}
			for cp := start; cp <= end; cp++ {
				add(startGID+(cp-start), cp)
			}
		}
	default:
		return nil
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// glyphNames 返回 glyphID -> 字形名；优先 post 2.0，其次 cmap 兜底。
func glyphNames(f *sfntFont, numGlyphs int) map[int]string {
	if post, ok := f.tableData("post"); ok {
		if names, ok := parsePostNames(post, numGlyphs); ok {
			return names
		}
	}
	if cmap, ok := f.tableData("cmap"); ok {
		return parseCmapNames(cmap, numGlyphs)
	}
	return nil
}

// ParseTTFGlyphs 解析 TTF 的 head/maxp/loca/glyf（辅以 post/cmap 取字形名），
// 返回 {"uni4E00": hash, ...}。
// 只收集字形名以 "uni" 开头、且 numberOfContours > 0 的简单字形；复合字形跳过。
func ParseTTFGlyphs(ttf []byte) (map[string]string, error) {
	f, err := parseSFNT(ttf)
	if err != nil {
		return nil, err
	}
	head, ok := f.tableData("head")
	if !ok || len(head) < headIndexToLocFormatOff+2 {
		return nil, fmt.Errorf("缺少或损坏的 head 表")
	}
	indexToLocFormat := i16(head[headIndexToLocFormatOff : headIndexToLocFormatOff+2])

	maxp, ok := f.tableData("maxp")
	if !ok || len(maxp) < maxpNumGlyphsOff+2 {
		return nil, fmt.Errorf("缺少或损坏的 maxp 表")
	}
	numGlyphs := be16(maxp[maxpNumGlyphsOff : maxpNumGlyphsOff+2])
	if numGlyphs <= 0 {
		return nil, fmt.Errorf("maxp.numGlyphs 非法: %d", numGlyphs)
	}

	loca, ok := f.tableData("loca")
	if !ok {
		return nil, fmt.Errorf("缺少 loca 表")
	}
	offsets, err := parseLoca(loca, numGlyphs, indexToLocFormat)
	if err != nil {
		return nil, err
	}

	glyf, ok := f.tableData("glyf")
	if !ok {
		return nil, fmt.Errorf("缺少 glyf 表")
	}

	names := glyphNames(f, numGlyphs)

	result := make(map[string]string)
	for gid := 0; gid < numGlyphs; gid++ {
		name, ok := names[gid]
		if !ok || !strings.HasPrefix(name, "uni") {
			continue
		}
		start, end := offsets[gid], offsets[gid+1]
		if start > end || int(end) > len(glyf) {
			continue
		}
		g, err := parseGlyph(glyf[start:end])
		if err != nil {
			return nil, fmt.Errorf("解析字形 %s 失败: %w", name, err)
		}
		h := HashGlyph(g)
		if h == "" {
			continue
		}
		result[name] = h
	}
	return result, nil
}

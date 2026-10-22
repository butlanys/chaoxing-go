package font

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Decoder 供 htmlx 等调用方使用的接口；实现可为 nil（调用方需自行判空）。
type Decoder interface {
	Decode(string) string
}

// 与原版 FontDecoder 完全一致的正则与前缀常量
const (
	// fontBase64Pattern 对齐 Python FONT_BASE64_PATTERN = r"base64,([\w\W]+?)'"
	fontBase64Pattern = `base64,([\s\S]+?)'`
	// FontDataURLPrefix 对齐 Python FONT_DATA_URL_PREFIX（长度 47）
	FontDataURLPrefix = "data:application/font-ttf;charset=utf-8;base64,"
	// SecretStyleID 加密字体所在的 style 标签 id
	SecretStyleID = "cxSecretStyle"
)

var (
	fontBase64Re = regexp.MustCompile(fontBase64Pattern)
	styleTagRe   = regexp.MustCompile(`(?is)<style[^>]*id\s*=\s*["']?` + SecretStyleID + `["']?[^>]*>([\s\S]*?)</style>`)
)

// HTMLDecoder 超星加密字体解码器，对应 Python 版 api/font_decoder.py 的 FontDecoder。
type HTMLDecoder struct {
	// fontmap 目标字体字形名 -> hash（如 "uni4E00" -> "acba..."）
	fontmap map[string]string
	table   *Table
}

// NewDecoder 从含 <style id="cxSecretStyle"> 的 HTML 构造解码器。
// 失败返回 error（原版是记 warning 并把 font_map 置空，这里显式返回，便于调用方判断）。
func NewDecoder(htmlText string, table *Table) (*HTMLDecoder, error) {
	if table == nil {
		return nil, fmt.Errorf("字体映射表未加载，无法解码")
	}
	css := extractSecretStyle(htmlText)
	if strings.TrimSpace(css) == "" {
		return nil, fmt.Errorf("未找到加密字体样式标签")
	}
	m := fontBase64Re.FindStringSubmatch(css)
	if m == nil || len(m) < 2 {
		return nil, fmt.Errorf("无法从样式标签中提取字体数据")
	}
	fontData, err := decodeBase64Font(m[1])
	if err != nil {
		return nil, fmt.Errorf("无法解码Base64字体数据: %w", err)
	}
	fontmap, err := ParseTTFGlyphs(fontData)
	if err != nil {
		return nil, fmt.Errorf("无法解析字体文件: %w", err)
	}
	return &HTMLDecoder{fontmap: fontmap, table: table}, nil
}

// extractSecretStyle 取出 <style id="cxSecretStyle"> 标签内的文本。
// 优先用 goquery（对齐 BeautifulSoup 行为），解析不到时回退正则。
func extractSecretStyle(htmlText string) string {
	if strings.TrimSpace(htmlText) == "" {
		return ""
	}
	if doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText)); err == nil {
		if text := doc.Find("style#" + SecretStyleID).Text(); strings.TrimSpace(text) != "" {
			return text
		}
	}
	if m := styleTagRe.FindStringSubmatch(htmlText); m != nil && len(m) > 1 {
		return m[1]
	}
	return ""
}

// decodeBase64Font 解码字体 base64 数据。
// 注意：原版是给 base64 加上 47 字节的 data URL 前缀后再按 font_data[47:] 切片解码，
// 这里直接解码 base64 本体，结果完全等价。换行/空白会被忽略（Python b64decode 亦如此）。
func decodeBase64Font(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	// 容错：万一正则把前缀一起吃进来了
	if i := strings.Index(s, FontDataURLPrefix); i >= 0 {
		s = s[i+len(FontDataURLPrefix):]
	}
	clean := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
	if clean == "" {
		return nil, fmt.Errorf("字体 base64 数据为空")
	}
	if m := len(clean) % 4; m != 0 {
		clean += strings.Repeat("=", 4-m)
	}
	if b, err := base64.StdEncoding.DecodeString(clean); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(clean, "=")); err == nil {
		return b, nil
	}
	b, err := base64.URLEncoding.DecodeString(clean)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Ready 是否已成功初始化字体映射
func (d *HTMLDecoder) Ready() bool {
	return d != nil && len(d.fontmap) > 0 && d.table != nil
}

// FontMapSize 已解析出的字形数量（调试用）
func (d *HTMLDecoder) FontMapSize() int {
	if d == nil {
		return 0
	}
	return len(d.fontmap)
}

// Decode 解码加密字符串。
// 逐字符：char_code = "uni" + upper(hex(ord(c))) -> fontmap 得 hash -> table 反查得原始 uniXXXX -> 字符；
// 查不到保留原字符；最后统一做康熙部首替换（对齐 Python decrypt）。
func (d *HTMLDecoder) Decode(s string) string {
	if s == "" {
		return s
	}
	if !d.Ready() {
		// 字体映射未初始化，保持原文（原版会抛 FontDecodeError，这里不 panic 也不改动内容）
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, ch := range s {
		charCode := "uni" + strings.ToUpper(strconv.FormatInt(int64(ch), 16))
		if hash, ok := d.fontmap[charCode]; ok {
			if original, ok := d.table.FindChar(hash); ok {
				if original != "" && strings.HasPrefix(original, "uni") {
					if code, err := strconv.ParseInt(original[3:], 16, 64); err == nil && code >= 0 {
						b.WriteRune(rune(code))
						continue
					}
				}
			}
		}
		// 无法解密则保留原字符
		b.WriteRune(ch)
	}
	// 替换解密后的康熙部首
	return KXFix(b.String())
}

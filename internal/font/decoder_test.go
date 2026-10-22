package font

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeTable 用给定的 {字形名: hash} 构造 Table（char/hash 双向）。
func fakeTable(pairs map[string]string) *Table {
	cm := make(map[string]string, len(pairs))
	hm := make(map[string]string, len(pairs))
	for name, hash := range pairs {
		cm[name] = hash
		hm[hash] = name
	}
	return &Table{charMap: cm, hashMap: hm, path: "fake"}
}

// buildSecretHTML 把合成字体塞进 <style id="cxSecretStyle">，模拟超星页面结构。
func buildSecretHTML(ttf []byte, splitLine bool) string {
	b64 := base64.StdEncoding.EncodeToString(ttf)
	if splitLine {
		// 每 76 字符换行，验证解码时忽略空白（Python b64decode 同样忽略非字母字符）
		var lines []string
		for i := 0; i < len(b64); i += 76 {
			end := i + 76
			if end > len(b64) {
				end = len(b64)
			}
			lines = append(lines, b64[i:end])
		}
		b64 = strings.Join(lines, "\n")
	}
	return `<html><head><style id="cxSecretStyle">
@font-face{font-family:'cxSecret';src:url('` + FontDataURLPrefix + b64 + `') format('truetype');}
</style></head><body><span class="x">测试</span></body></html>`
}

// TestNewDecoderFromHTML 走完整的 HTML -> base64 -> TTF -> fontmap 流程。
func TestNewDecoderFromHTML(t *testing.T) {
	ttf := buildTestTTF(testFontOpts{withPost: true, withCmap: true})
	h00 := goldenGlyphHashes["uni4E00"]
	h8C := goldenGlyphHashes["uni4E8C"]
	// fakeTable 的参数是 {字形名: hash}，所以 FindChar(h00) == "uni4E8C"
	table := fakeTable(map[string]string{
		"uni4E8C": h00,
		"uni4E09": h8C,
	})
	for _, split := range []bool{false, true} {
		d, err := NewDecoder(buildSecretHTML(ttf, split), table)
		if err != nil {
			t.Fatalf("[split=%v] NewDecoder 失败: %v", split, err)
		}
		if !d.Ready() {
			t.Fatalf("[split=%v] 解码器未就绪", split)
		}
		if d.FontMapSize() != 4 {
			t.Errorf("[split=%v] fontmap 应有 4 个字形, 实际 %d", split, d.FontMapSize())
		}
		if got := d.Decode("一"); got != "二" {
			t.Errorf("[split=%v] Decode(一) = %q, 期望 二", split, got)
		}
	}
}

// TestNewDecoderErrors 各类异常输入都返回 error，不 panic。
func TestNewDecoderErrors(t *testing.T) {
	ttf := buildTestTTF(testFontOpts{withPost: true})
	table := fakeTable(map[string]string{goldenGlyphHashes["uni4E00"]: "uni4E8C"})
	cases := []struct {
		name string
		html string
		tbl  *Table
	}{
		{"table 为 nil", buildSecretHTML(ttf, false), nil},
		{"无 style 标签", `<html><body>hello</body></html>`, table},
		{"style 里没有 base64", `<html><style id="cxSecretStyle">.a{color:red}</style></html>`, table},
		{"base64 非法", `<html><style id="cxSecretStyle">src:url('` + FontDataURLPrefix + `!!!not-base64!!!')</style></html>`, table},
		{"不是合法 TTF", `<html><style id="cxSecretStyle">src:url('` + FontDataURLPrefix +
			base64.StdEncoding.EncodeToString([]byte("hello world, not a font")) + `')</style></html>`, table},
		{"空 HTML", "", table},
	}
	for _, tc := range cases {
		if _, err := NewDecoder(tc.html, tc.tbl); err == nil {
			t.Errorf("[%s] 期望返回 error", tc.name)
		}
	}
}

// TestDecodeBehaviour 字符还原、未知字符保留、康熙部首替换。
func TestDecodeBehaviour(t *testing.T) {
	h00 := goldenGlyphHashes["uni4E00"]
	h8C := goldenGlyphHashes["uni4E8C"]
	// charMap: 字形名 -> hash；hashMap: hash -> 字形名（fakeTable 的构造方向相反，这里手工构造）
	table := &Table{
		charMap: map[string]string{"uni4E00": h00, "uni4E8C": h8C},
		hashMap: map[string]string{h00: "uni4E8C", h8C: "uni4E00"},
		path:    "fake",
	}
	d := &HTMLDecoder{
		fontmap: map[string]string{
			"uni4E00": h00,
			"uni4E8C": h8C,
		},
		table: table,
	}

	cases := []struct{ in, want string }{
		{"一", "二"},
		{"二", "一"},
		{"一二", "二一"},
		{"你好", "你好"},     // 不在 fontmap，原样保留
		{"一你好二", "二你好一"}, // 混合
		{"", ""},
		{"⼀⼁⼂", "一丨丶"},         // U+2F00 起：康熙部首替换（本身不在 fontmap）
		{"⺠", "民"},             // U+2EA0 -> 民
		{"一⺠", "二民"},           // 解码 + 康熙替换叠加
		{"\n\t 一 ", "\n\t 二 "}, // 空白与标点保留
	}
	for _, tc := range cases {
		if got := d.Decode(tc.in); got != tc.want {
			t.Errorf("Decode(%q) = %q, 期望 %q", tc.in, got, tc.want)
		}
	}

	// hash 命中 fontmap 但 table 里查不到 -> 保留原字符
	d2 := &HTMLDecoder{
		fontmap: map[string]string{"uni4E00": "ffffffffffffffffffffffffffffffff"},
		table:   table,
	}
	if got := d2.Decode("一"); got != "一" {
		t.Errorf("table 未命中时应保留原字符, 实际 %q", got)
	}

	// table 里的字形名非法（非 uniXXXX）-> 保留原字符
	d3 := &HTMLDecoder{
		fontmap: map[string]string{"uni4E00": "deadbeef"},
		table:   fakeTable(map[string]string{"deadbeef": "A"}),
	}
	if got := d3.Decode("一"); got != "一" {
		t.Errorf("非法字形名应保留原字符, 实际 %q", got)
	}
}

// TestDecodeNilSafe 未初始化的解码器不 panic、不改动内容。
func TestDecodeNilSafe(t *testing.T) {
	var d *HTMLDecoder
	if got := d.Decode("一⼀"); got != "一⼀" {
		t.Errorf("nil 解码器应原样返回, 实际 %q", got)
	}
	if d.Ready() || d.FontMapSize() != 0 {
		t.Errorf("nil 解码器 Ready/FontMapSize 应分别为 false/0")
	}
	d2 := &HTMLDecoder{}
	if got := d2.Decode("abc"); got != "abc" {
		t.Errorf("未初始化解码器应原样返回, 实际 %q", got)
	}
}

// TestKXRadicalsTable 康熙部首表规模与关键映射。
func TestKXRadicalsTable(t *testing.T) {
	from := []rune(kxRadicalsFrom)
	to := []rune(kxRadicalsTo)
	if len(from) != 223 || len(to) != 223 {
		t.Fatalf("康熙部首表长度应为 223, 实际 from=%d to=%d", len(from), len(to))
	}
	if len(kxRadicalMap) != 223 {
		t.Errorf("映射表条目数应为 223, 实际 %d", len(kxRadicalMap))
	}
	pairs := map[rune]rune{'⼀': '一', '⼁': '丨', '⺠': '民', '⻬': '齐', '⻓': '长', '⿕': '龠'}
	for k, v := range pairs {
		if got := KXFix(string(k)); got != string(v) {
			t.Errorf("KXFix(%q) = %q, 期望 %q", string(k), got, string(v))
		}
		if c, ok := kxRadicalMap[k]; !ok || c != v {
			t.Errorf("kxRadicalMap[%q] = %q(%v), 期望 %q", string(k), string(c), ok, string(v))
		}
	}
	if got := KXRadicals.Replace("⼀⺠正常"); got != "一民正常" {
		t.Errorf("KXRadicals.Replace = %q, 期望 %q", got, "一民正常")
	}
}

// TestLoadTableRealResource 真实 resource/font_map_table.json（1.5MB）加载与双向查询。
func TestLoadTableRealResource(t *testing.T) {
	tbl, err := LoadTable("")
	if err != nil {
		t.Fatalf("LoadTable 失败: %v", err)
	}
	if tbl.Len() < 20000 {
		t.Fatalf("映射表条目数异常: %d", tbl.Len())
	}
	t.Logf("映射表路径=%s 条目数=%d", tbl.Path(), tbl.Len())
	// 与 resource/font_map_table.json 头部内容一致
	if h, ok := tbl.FindHash("A"); !ok || h != "6fca21acbe6393791a0e7096eaa49265" {
		t.Errorf("FindHash(A) = %q(%v)", h, ok)
	}
	// 该表中存在 39 个重复 hash；Python 的 dict 推导式是「后者覆盖前者」，
	// 期望 FindChar 得到文件里靠后的 "uni0410"（Go 侧用流式解析保证同样的确定性）。
	if c, ok := tbl.FindChar("6fca21acbe6393791a0e7096eaa49265"); !ok || c != "uni0410" {
		t.Errorf("FindChar(6fca...) = %q(%v), 期望 uni0410（与 Python 后者覆盖前者一致）", c, ok)
	}
	// 任取一个 uni 字形名做双向校验（重复 hash 时回查可能得到同 hash 的另一个名字）
	for name, hash := range tbl.charMap {
		if strings.HasPrefix(name, "uni") {
			back, ok := tbl.FindChar(hash)
			if !ok || back == "" {
				t.Errorf("FindChar(%s) 未命中", hash)
				break
			}
			if h2, ok := tbl.FindHash(back); !ok || h2 != hash {
				t.Errorf("双向查询不一致: %s -> %s -> %s", name, hash, back)
			}
			break
		}
	}
	if _, ok := tbl.FindHash("不存在的字形名"); ok {
		t.Errorf("不存在的字形名不应命中")
	}
	if _, ok := tbl.FindChar("不存在的hash"); ok {
		t.Errorf("不存在的 hash 不应命中")
	}
}

// TestLoadTableExplicitPath 显式路径优先。
func TestLoadTableExplicitPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "my_table.json")
	if err := os.WriteFile(p, []byte(`{"uni4E00":"aaaa","A":"bbbb"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := LoadTable(p)
	if err != nil {
		t.Fatalf("LoadTable 失败: %v", err)
	}
	if tbl.Len() != 2 || tbl.Path() != p {
		t.Fatalf("加载结果异常: len=%d path=%s", tbl.Len(), tbl.Path())
	}
	if h, _ := tbl.FindHash("uni4E00"); h != "aaaa" {
		t.Errorf("FindHash(uni4E00) = %q", h)
	}
	// 非法 JSON
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTable(bad); err == nil {
		t.Errorf("非法 JSON 期望返回 error")
	}
}

// TestLoadTableNotFound 所有候选路径都不存在时返回 error。
func TestLoadTableNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // 让 ./resource 与 ../../resource 都落到临时目录下
	if _, err := LoadTable(filepath.Join(dir, "missing.json")); err == nil {
		t.Errorf("期望返回 error")
	}
}

// TestDecoderImplementsInterface 编译期确认 Decoder 接口实现。
func TestDecoderImplementsInterface(t *testing.T) {
	var d Decoder = &HTMLDecoder{}
	if d.Decode("x") != "x" {
		t.Errorf("空解码器应原样返回")
	}
}

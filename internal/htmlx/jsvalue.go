package htmlx

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 超星返回的 mArg 是 JS 对象字面量而非严格 JSON（可能出现无引号 key、单引号字符串、
// 尾随逗号），Python 版直接用 json.loads，遇到非严格 JSON 会抛异常。
// 这里先尝试标准 JSON，失败后用下面的容错解析器兜底，保证不 panic。

// jsMaxDepth 限制嵌套深度，防止畸形输入把栈打爆
const jsMaxDepth = 64

// parseMArgJSON 把 mArg 抓到的片段解析成 map（调用方负责补齐外层花括号）。
func parseMArgJSON(full string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(full), &m); err == nil && m != nil {
		return m
	}

	// 回退：手工解析 JS 对象字面量
	m2, err := parseJSObjectLiteral(full)
	if err != nil {
		log().Warn("解析 mArg 卡片数据失败: %s", err.Error())
		return nil
	}
	return m2
}

type jsParser struct {
	s   string
	pos int
}

// parseJSObjectLiteral 解析一个 JS 对象字面量（必须以 '{' 开头）
func parseJSObjectLiteral(s string) (map[string]any, error) {
	p := &jsParser{s: s}
	v, err := p.parseValue(0)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mArg 不是对象字面量")
	}
	return m, nil
}

func (p *jsParser) skipSpace() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsParser) peek() (byte, bool) {
	if p.pos >= len(p.s) {
		return 0, false
	}
	return p.s[p.pos], true
}

func (p *jsParser) parseValue(depth int) (any, error) {
	if depth > jsMaxDepth {
		return nil, fmt.Errorf("嵌套层级过深")
	}
	p.skipSpace()
	c, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("意外的结尾")
	}
	switch {
	case c == '{':
		return p.parseObject(depth)
	case c == '[':
		return p.parseArray(depth)
	case c == '"' || c == '\'':
		return p.parseQuoted(c)
	default:
		return p.parseBare(depth)
	}
}

func (p *jsParser) parseObject(depth int) (any, error) {
	p.pos++ // 跳过 '{'
	out := map[string]any{}
	for {
		p.skipSpace()
		c, ok := p.peek()
		if !ok {
			return nil, fmt.Errorf("对象未闭合")
		}
		if c == '}' {
			p.pos++
			return out, nil
		}
		if c == ',' { // 容忍多余逗号
			p.pos++
			continue
		}
		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if c2, ok2 := p.peek(); ok2 && c2 == ':' {
			p.pos++
		} else {
			return nil, fmt.Errorf("对象缺少冒号")
		}
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
}

func (p *jsParser) parseArray(depth int) (any, error) {
	p.pos++ // 跳过 '['
	out := []any{}
	for {
		p.skipSpace()
		c, ok := p.peek()
		if !ok {
			return nil, fmt.Errorf("数组未闭合")
		}
		if c == ']' {
			p.pos++
			return out, nil
		}
		if c == ',' {
			p.pos++
			continue
		}
		val, err := p.parseValue(depth + 1)
		if err != nil {
			return nil, err
		}
		out = append(out, val)
	}
}

// parseKey 解析对象键：带引号字符串，或无引号的标识符/数字
func (p *jsParser) parseKey() (string, error) {
	p.skipSpace()
	c, ok := p.peek()
	if !ok {
		return "", fmt.Errorf("对象键缺失")
	}
	if c == '"' || c == '\'' {
		v, err := p.parseQuoted(c)
		if err != nil {
			return "", err
		}
		return anyString(v), nil
	}
	start := p.pos
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ':', ',', '}', ' ', '\t', '\n', '\r', '\v', '\f':
			if p.pos == start {
				return "", fmt.Errorf("对象键为空")
			}
			return p.s[start:p.pos], nil
		}
		p.pos++
	}
	if p.pos == start {
		return "", fmt.Errorf("对象键为空")
	}
	return p.s[start:p.pos], nil
}

func (p *jsParser) parseQuoted(quote byte) (any, error) {
	p.pos++ // 跳过引号
	var sb strings.Builder
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '\\' {
			p.pos++
			if p.pos >= len(p.s) {
				break
			}
			switch e := p.s[p.pos]; e {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'u':
				r, n := p.decodeUnicodeEscape()
				if n == 0 {
					sb.WriteString(`\u`)
				} else {
					sb.WriteRune(r)
					p.pos += n
					continue
				}
			default:
				sb.WriteByte(e)
			}
			p.pos++
			continue
		}
		if c == quote {
			p.pos++
			return sb.String(), nil
		}
		r, size := utf8.DecodeRuneInString(p.s[p.pos:])
		sb.WriteRune(r)
		p.pos += size
	}
	return nil, fmt.Errorf("字符串未闭合")
}

// decodeUnicodeEscape 解析 \uXXXX（当前 p.pos 指向 'u'），返回 rune 与消耗的字节数
func (p *jsParser) decodeUnicodeEscape() (rune, int) {
	// p.pos 指向 'u'
	start := p.pos + 1
	if start+4 > len(p.s) {
		return 0, 0
	}
	n, err := strconv.ParseUint(p.s[start:start+4], 16, 32)
	if err != nil {
		return 0, 0
	}
	return rune(n), 5 // 'u' + 4 位十六进制
}

// parseBare 解析 true/false/null/undefined/数字/裸标识符
func (p *jsParser) parseBare(depth int) (any, error) {
	start := p.pos
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ',', '}', ']', ':', ' ', '\t', '\n', '\r', '\v', '\f':
			goto done
		}
		p.pos++
	}
done:
	if p.pos == start {
		return nil, fmt.Errorf("无法识别的值")
	}
	tok := p.s[start:p.pos]
	switch tok {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "undefined", "NaN", "Infinity":
		return nil, nil
	}
	if f, err := strconv.ParseFloat(tok, 64); err == nil {
		return f, nil
	}
	// 裸字符串（例如 {type:video}）
	return tok, nil
}

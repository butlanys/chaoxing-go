package htmlx

import (
	"sort"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// 本文件对应 Python decode.py 的 _process_question / _get_question_type /
// _extract_title / _extract_choices。

// processQuestion 对应 Python 的 _process_question。
func processQuestion(divTag *goquery.Selection, fd FontDecoder) Question {
	questionID := attrOr(divTag, "data", "")

	// Python: div_tag.find("div", class_="TiMu").attrs.get("data","")
	tiMu := divTag.Find("div.TiMu").First()
	qTypeCode := attrOr(tiMu, "data", "")
	qType := questionTypeName(qTypeCode)

	titleDiv := divTag.Find("div.Zy_TItle").First()
	title := extractTitle(titleDiv, fd)

	options := []string{}
	if ul := divTag.Find("ul").First(); ul.Length() > 0 {
		ul.Find("li").Each(func(_ int, li *goquery.Selection) {
			options = append(options, extractChoices(li, fd))
		})
	}
	sort.Strings(options)

	return Question{
		ID:       questionID,
		Title:    title,
		Options:  strings.Join(options, "\n"),
		Type:     qType,
		TypeCode: qTypeCode,
	}
}

// questionTypeName 对应 Python 的 _get_question_type。
func questionTypeName(typeCode string) string {
	typeMap := map[string]string{
		"0": "single",      // 单选题
		"1": "multiple",    // 多选题
		"2": "completion",  // 填空题
		"3": "judgement",   // 判断题
		"4": "shortanswer", // 简答题
	}
	if t, ok := typeMap[typeCode]; ok {
		return t
	}
	log().Info("未知题型代码 -> %s", typeCode)
	return "unknown"
}

// extractTitle 对应 Python 的 _extract_title：按文档顺序收集所有文本节点，
// 并把 img 元素转成 <img src="URL">，去掉 \r\t\n 后按需解密。
func extractTitle(element *goquery.Selection, fd FontDecoder) string {
	if element == nil || element.Length() == 0 {
		return ""
	}

	var sb strings.Builder
	collectTitleContent(element.Nodes[0], &sb)

	cleaned := reLineBreaks.Replace(sb.String())
	if fd != nil {
		return fd.Decode(cleaned)
	}
	return cleaned
}

// collectTitleContent 深度优先遍历，复刻 BeautifulSoup 的 element.descendants 语义
// （NavigableString 取文本；img 取 <img src="...">；Comment 也是 NavigableString 的子类）。
func collectTitleContent(n *html.Node, sb *strings.Builder) {
	if n == nil {
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode, html.CommentNode:
			sb.WriteString(c.Data)
		case html.ElementNode:
			if c.Data == "img" {
				sb.WriteString(`<img src="` + htmlAttr(c, "src") + `">`)
			}
			collectTitleContent(c, sb)
		}
	}
}

// htmlAttr 读取 html.Node 的属性值
func htmlAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// extractChoices 对应 Python 的 _extract_choices：aria-label 优先，去掉 \r\t\n，
// 按需解密，trim 后若以"选择"结尾则裁掉。
func extractChoices(element *goquery.Selection, fd FontDecoder) string {
	if element == nil || element.Length() == 0 {
		return ""
	}

	// 优先取 aria-label 属性值（解决 #474）
	choice, ok := element.Attr("aria-label")
	if !ok || choice == "" {
		choice = element.Text()
	}
	if choice == "" {
		return ""
	}

	cleaned := reLineBreaks.Replace(choice)
	if fd != nil {
		cleaned = fd.Decode(cleaned)
	}

	cleaned = strings.TrimSpace(cleaned)
	if strings.HasSuffix(cleaned, "选择") {
		cleaned = strings.TrimRightFunc(cleaned[:len(cleaned)-len("选择")], unicode.IsSpace)
	}
	return cleaned
}

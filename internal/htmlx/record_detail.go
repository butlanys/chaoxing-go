package htmlx

import (
	"regexp"
	"strings"
)

// 本文件对应 Python api/base.py 的 _parse_work_record_detail（单次作答详情）。

var (
	// r'<div class="TiMu[^"]*singleQuesId" data="(\d+)"[^>]*>'
	reRecordQHeader = regexp.MustCompile(`<div class="TiMu[^"]*singleQuesId" data="(\d+)"[^>]*>`)

	// r'newZy_TItle">(.*?)</span>(.*?)</div>'  (re.S)
	reRecordQTitle = regexp.MustCompile(`(?s)newZy_TItle">(.*?)</span>(.*?)</div>`)

	// Python 的三种备选答案模式（{label} 运行时替换成 我的答案 / 正确答案）
	recordAnsPatternTpls = []string{
		`{label}[:：]</span>` + pySpaceClass + `*<div class="fl answerCon">` + pySpaceClass + `*(.*?)` + pySpaceClass + `*</div>`,
		`{label}[:：]` + pySpaceClass + `*</span>` + pySpaceClass + `*<div[^>]*class="[^"]*answerCon[^"]*"[^>]*>` + pySpaceClass + `*(.*?)` + pySpaceClass + `*</div>`,
		`{label}[:：]` + pySpaceClass + `*</span>` + pySpaceClass + `*<span[^>]*>` + pySpaceClass + `*(.*?)` + pySpaceClass + `*</span>`,
	}

	// 预编译两种标签下的全部备选模式
	recordAnsPatterns = buildRecordAnsPatterns()

	// Python 用前瞻 (?=<div class="TiMu|$) 界定题目块；Go RE2 不支持前瞻，
	// 用等价实现：题目块 = 头部匹配之后到下一个字面量 <div class="TiMu（或串尾）之间。
	recordQBoundary = `<div class="TiMu`
)

func buildRecordAnsPatterns() map[string][]*regexp.Regexp {
	labels := []string{"我的答案", "正确答案"}
	out := make(map[string][]*regexp.Regexp, len(labels))
	for _, label := range labels {
		compiled := make([]*regexp.Regexp, 0, len(recordAnsPatternTpls))
		for _, tpl := range recordAnsPatternTpls {
			pat := "(?s)" + strings.Replace(tpl, "{label}", label, 1)
			re, err := regexp.Compile(pat)
			if err != nil {
				log().Error("作答详情答案正则编译失败: %s", err.Error())
				continue
			}
			compiled = append(compiled, re)
		}
		out[label] = compiled
	}
	return out
}

// DecodeWorkRecordDetail 解析 /work/record-detail 页面，返回每题信息。
// 我的答案/正确答案任一未命中时 ParseOK=false。
func DecodeWorkRecordDetail(htmlText string) []RecordQuestion {
	questions := []RecordQuestion{}

	matches := reRecordQHeader.FindAllStringSubmatchIndex(htmlText, -1)
	for _, m := range matches {
		if len(m) < 4 {
			continue
		}
		qid := htmlText[m[2]:m[3]]

		// 题目块结束位置：头部匹配之后第一个 <div class="TiMu，否则到串尾
		bodyStart := m[1]
		bodyEnd := len(htmlText)
		if p := strings.Index(htmlText[bodyStart:], recordQBoundary); p >= 0 {
			bodyEnd = bodyStart + p
		}

		questions = append(questions, parseRecordQuestion(qid, htmlText[bodyStart:bodyEnd]))
	}

	return questions
}

// parseRecordQuestion 解析单个题目块
func parseRecordQuestion(qid, qb string) RecordQuestion {
	typeLabel := ""
	title := ""

	if tm := reRecordQTitle.FindStringSubmatch(qb); len(tm) > 2 {
		typeLabel = strings.TrimSpace(reTagStrip.ReplaceAllString(tm[1], ""))
		title = reTagStrip.ReplaceAllString(tm[2], "")
	}
	// Python: title = re.sub(r'\s+', ' ', title).strip()
	title = strings.TrimSpace(reWS.ReplaceAllString(title, " "))

	myRaw, myOK := extractRecordAnswer(qb, "我的答案")
	correctRaw, correctOK := extractRecordAnswer(qb, "正确答案")

	return RecordQuestion{
		ID:            qid,
		Title:         title,
		TypeLabel:     typeLabel,
		MyAnswer:      myRaw,
		CorrectAnswer: correctRaw,
		ParseOK:       myOK && correctOK,
	}
}

// extractRecordAnswer 依次尝试三种备选模式，命中即返回（去掉标签后 trim）。
// 第二个返回值表示是否命中（未命中时 Python 返回 None，ParseOK 为 false）。
func extractRecordAnswer(qb, label string) (string, bool) {
	for _, re := range recordAnsPatterns[label] {
		m := re.FindStringSubmatch(qb)
		if len(m) > 1 {
			return strings.TrimSpace(reTagStrip.ReplaceAllString(m[1], "")), true
		}
	}
	return "", false
}

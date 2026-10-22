package htmlx

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// 本文件对应 Python decode.py 的 decode_questions_info / _extract_form_data。
// 单题解析（_process_question / _extract_title / _extract_choices）见 question_item.go。

// DecodeQuestions 解析章节检测题目页面，对应 Python 的 decode_questions_info。
// fd 为字体解码器（可为 nil，nil 时不解密）。
func DecodeQuestions(htmlText string, fd FontDecoder) WorkForm {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		log().Error("题目页面 HTML 解析失败: %s", err.Error())
		return WorkForm{}
	}

	form := doc.Find("form").First()
	if form.Length() == 0 {
		// Python 版此处会抛 AttributeError（无 form 即非题目页），这里容错返回空表单
		log().Warn("题目页面未找到 form 表单")
		return WorkForm{}
	}

	fields := extractFormData(form)

	questions := []Question{}
	form.Find("div.singleQuesId").Each(func(_ int, divTag *goquery.Selection) {
		questions = append(questions, processQuestion(divTag, fd))
	})

	ids := make([]string, 0, len(questions))
	for _, q := range questions {
		ids = append(ids, q.ID)
	}

	return WorkForm{
		Fields:      fields,
		Questions:   questions,
		AnswerWQBID: strings.Join(ids, ",") + ",",
	}
}

// extractFormData 对应 Python 的 _extract_form_data：收集 form 内所有
// name 非空且不含 "answer" 的 input 的 name -> value。
func extractFormData(form *goquery.Selection) map[string]string {
	formData := map[string]string{}
	if form == nil || form.Length() == 0 {
		return formData
	}

	form.Find("input").Each(func(_ int, inputTag *goquery.Selection) {
		name, ok := inputTag.Attr("name")
		if !ok || name == "" || strings.Contains(name, "answer") {
			return
		}
		value, _ := inputTag.Attr("value")
		formData[name] = value
	})

	return formData
}

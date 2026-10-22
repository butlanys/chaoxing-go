package htmlx

import (
	"strings"
	"testing"
)

// 章节检测题目页面片段：含 5 种题型 + 未知题型、aria-label 优先、以"选择"结尾的选项
const questionsHTML = `<html><body>
<form id="form1" method="post" action="/work/addStudentWorkNew">
  <input type="hidden" name="courseId" value="1001">
  <input type="hidden" name="classId" value="2001">
  <input type="hidden" name="token" value="tk">
  <input type="hidden" name="answerwqbid" value="应被跳过">
  <input type="hidden" name="answertype1001" value="0">
  <input type="hidden" name="noValueAttr">
  <div class="singleQuesId" data="1001">
    <div class="TiMu" data="0">
      <div class="Zy_TItle"><span class="fl">1.</span>单选题：以下哪个正确？<img src="http://p.chaoxing.com/img/q1.png"></div>
      <ul class="Zy_ulTop">
        <li aria-label="B. 选项二">B. 选项二</li>
        <li aria-label="A. 选项一">A. 选项一</li>
      </ul>
    </div>
  </div>
  <div class="singleQuesId" data="1002">
    <div class="TiMu" data="1">
      <div class="Zy_TItle"><span class="fl">2.</span>多选题：以下哪些正确？</div>
      <ul class="Zy_ulTop">
        <li aria-label="C. 标签优先">D. 文本内容</li>
        <li>E. 纯文本选项</li>
      </ul>
    </div>
  </div>
  <div class="singleQuesId" data="1003">
    <div class="TiMu" data="2">
      <div class="Zy_TItle"><span class="fl">3.</span>
	填空题：____
</div>
      <ul>
        <li aria-label="答案二">答案二</li>
        <li aria-label="答案一">答案一</li>
      </ul>
    </div>
  </div>
  <div class="singleQuesId" data="1004">
    <div class="TiMu" data="3">
      <div class="Zy_TItle"><span class="fl">4.</span>判断题：这句话对吗？</div>
      <ul>
        <li aria-label="正确 选择">正确 选择</li>
        <li aria-label="错误">错误</li>
      </ul>
    </div>
  </div>
  <div class="singleQuesId" data="1005">
    <div class="TiMu" data="4">
      <div class="Zy_TItle"><span class="fl">5.</span>简答题：请简述。</div>
      <ul>
        <li aria-label="参考答案">参考答案</li>
      </ul>
    </div>
  </div>
  <div class="singleQuesId" data="1006">
    <div class="TiMu" data="9">
      <div class="Zy_TItle"><span class="fl">6.</span>未知题型。</div>
      <ul>
        <li aria-label="A. 未知题选项">A. 未知题选项</li>
      </ul>
    </div>
  </div>
</form>
</body></html>`

func TestDecodeQuestions(t *testing.T) {
	form := DecodeQuestions(questionsHTML, nil)

	// 表单字段：name 含 "answer" 的 input 必须被排除
	wantFields := map[string]string{
		"courseId":    "1001",
		"classId":     "2001",
		"token":       "tk",
		"noValueAttr": "",
	}
	if len(form.Fields) != len(wantFields) {
		t.Fatalf("表单字段 = %+v, 期望 %+v", form.Fields, wantFields)
	}
	for k, v := range wantFields {
		if form.Fields[k] != v {
			t.Errorf("字段 %s = %q, 期望 %q", k, form.Fields[k], v)
		}
	}

	wantQuestions := []Question{
		{
			ID: "1001", TypeCode: "0", Type: "single",
			Title:   `1.单选题：以下哪个正确？<img src="http://p.chaoxing.com/img/q1.png">`,
			Options: "A. 选项一\nB. 选项二",
		},
		{
			ID: "1002", TypeCode: "1", Type: "multiple",
			Title:   "2.多选题：以下哪些正确？",
			Options: "C. 标签优先\nE. 纯文本选项",
		},
		{
			ID: "1003", TypeCode: "2", Type: "completion",
			Title:   "3.填空题：____",
			Options: "答案一\n答案二",
		},
		{
			ID: "1004", TypeCode: "3", Type: "judgement",
			Title:   "4.判断题：这句话对吗？",
			Options: "正确\n错误",
		},
		{
			ID: "1005", TypeCode: "4", Type: "shortanswer",
			Title:   "5.简答题：请简述。",
			Options: "参考答案",
		},
		{
			ID: "1006", TypeCode: "9", Type: "unknown",
			Title:   "6.未知题型。",
			Options: "A. 未知题选项",
		},
	}

	if len(form.Questions) != len(wantQuestions) {
		t.Fatalf("题目数量 = %d, 期望 %d", len(form.Questions), len(wantQuestions))
	}
	for i, want := range wantQuestions {
		got := form.Questions[i]
		if got.ID != want.ID || got.Type != want.Type || got.TypeCode != want.TypeCode {
			t.Errorf("第 %d 题 id/type = %s/%s/%s, 期望 %s/%s/%s",
				i, got.ID, got.Type, got.TypeCode, want.ID, want.Type, want.TypeCode)
		}
		if got.Title != want.Title {
			t.Errorf("第 %d 题 title = %q, 期望 %q", i, got.Title, want.Title)
		}
		if got.Options != want.Options {
			t.Errorf("第 %d 题 options = %q, 期望 %q", i, got.Options, want.Options)
		}
	}

	// AnswerWQBID 拼接
	wantWQBID := "1001,1002,1003,1004,1005,1006,"
	if form.AnswerWQBID != wantWQBID {
		t.Errorf("AnswerWQBID = %q, 期望 %q", form.AnswerWQBID, wantWQBID)
	}
}

func TestDecodeQuestionsBranches(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string // 期望的 AnswerWQBID
	}{
		{name: "空字符串", html: "", want: ""},
		{name: "纯文本", html: "没有任何表单", want: ""},
		// 截断页面仍能解析出部分题目（容错，不 panic）
		{name: "截断 HTML", html: `<form><div class="singleQuesId" data="1"><div class="TiMu" data="0"`, want: "1,"},
		{name: "有 form 无题目", html: `<form><input name="a" value="1"></form>`, want: ","},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecodeQuestions(c.html, nil)
			if got.AnswerWQBID != c.want {
				t.Errorf("AnswerWQBID = %q, 期望 %q", got.AnswerWQBID, c.want)
			}
		})
	}
}

// fakeDecoder 记录调用次数并把"加密"替换成"明文"，用于验证 fd 非 nil 时确实被调用
type fakeDecoder struct {
	calls int
	seen  []string
}

func (f *fakeDecoder) Decode(s string) string {
	f.calls++
	f.seen = append(f.seen, s)
	return strings.ReplaceAll(s, "加密", "明文")
}

func TestDecodeQuestionsWithFontDecoder(t *testing.T) {
	html := `<form>
  <input name="courseId" value="1">
  <div class="singleQuesId" data="9001">
    <div class="TiMu" data="0">
      <div class="Zy_TItle">1.加密题目内容</div>
      <ul><li aria-label="加密选项 选择">加密选项 选择</li></ul>
    </div>
  </div>
</form>`

	// 无解码器：保留密文，"选择"结尾仍被裁剪
	plain := DecodeQuestions(html, nil)
	if plain.Questions[0].Title != "1.加密题目内容" {
		t.Errorf("无解码器时 title = %q", plain.Questions[0].Title)
	}
	if plain.Questions[0].Options != "加密选项" {
		t.Errorf("无解码器时 options = %q, 期望 %q", plain.Questions[0].Options, "加密选项")
	}

	// 有解码器：标题与选项都被解码，且"选择"裁剪发生在解码之后
	fd := &fakeDecoder{}
	decoded := DecodeQuestions(html, fd)
	if fd.calls == 0 {
		t.Fatalf("字体解码器未被调用")
	}
	if decoded.Questions[0].Title != "1.明文题目内容" {
		t.Errorf("解码后 title = %q, 期望 %q", decoded.Questions[0].Title, "1.明文题目内容")
	}
	if decoded.Questions[0].Options != "明文选项" {
		t.Errorf("解码后 options = %q, 期望 %q", decoded.Questions[0].Options, "明文选项")
	}
	if len(fd.seen) == 0 || fd.seen[0] != "1.加密题目内容" {
		t.Errorf("解码器入参错误: %v", fd.seen)
	}
}

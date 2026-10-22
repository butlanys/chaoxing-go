package htmlx

import "testing"

// /work/record-list 页面片段
const recordListHTML = `<html><body>
<div class="record-list">
  <div class="record-item"><span class="viewNum">第0次</span><span class="viewScore">80分</span></div>
  <div class="record-item"><span class="viewNum">第1次</span><span class="viewScore">100分</span></div>
  <div class="record-item"><span class="viewNum">第2次</span><span class="viewScore">99.5分</span></div>
</div>
</body></html>`

// 次数与成绩数量不匹配（缺一个成绩）
const recordListMismatchHTML = `<html><body>
<span class="viewNum">第0次</span><span class="viewScore">80分</span>
<span class="viewNum">第1次</span>
</body></html>`

func TestDecodeWorkRecordList(t *testing.T) {
	cases := []struct {
		name string
		html string
		want []RecordItem
	}{
		{
			name: "正常记录",
			html: recordListHTML,
			want: []RecordItem{{Times: 0, Score: 80}, {Times: 1, Score: 100}, {Times: 2, Score: 99.5}},
		},
		{
			name: "次数与成绩数量不匹配时成绩按0",
			html: recordListMismatchHTML,
			want: []RecordItem{{Times: 0, Score: 0}, {Times: 1, Score: 0}},
		},
		{name: "空字符串", html: "", want: []RecordItem{}},
		{name: "纯文本", html: "没有作答记录", want: []RecordItem{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecodeWorkRecordList(c.html)
			if len(got) != len(c.want) {
				t.Fatalf("记录数量 = %d, 期望 %d (%+v)", len(got), len(c.want), got)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("第 %d 条记录 = %+v, 期望 %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}

// /work/record-detail 页面片段：
// 第 1 题走第一种答案模式（我的答案 + 正确答案），第 2 题走第三种模式且缺正确答案。
const recordDetailHTML = `<html><body>
<div class="TiMu singleQuesId" data="1001">
  <div class="Zy_TItle newZy_TItle"><span class="fl">[单选题]</span>以下说法正确的是</div>
  <div class="Py_answer">
    <span class="fl">我的答案:</span>
    <div class="fl answerCon"><span class="cu">A</span></div>
    <span class="fl">正确答案:</span>
    <div class="fl answerCon">A</div>
  </div>
</div>
<div class="TiMu singleQuesId" data="1002">
  <div class="Zy_TItle newZy_TItle"><span class="fl">[判断题]</span>
    判断题内容
  </div>
  <div class="Py_answer">
    <span class="fl">我的答案:</span><span>正确</span>
  </div>
</div>
</body></html>`

func TestDecodeWorkRecordDetail(t *testing.T) {
	got := DecodeWorkRecordDetail(recordDetailHTML)

	want := []RecordQuestion{
		{
			ID: "1001", Title: "以下说法正确的是", TypeLabel: "[单选题]",
			MyAnswer: "A", CorrectAnswer: "A", ParseOK: true,
		},
		{
			// 第三种模式命中"我的答案"，但"正确答案"三种模式都未命中 -> ParseOK=false
			ID: "1002", Title: "判断题内容", TypeLabel: "[判断题]",
			MyAnswer: "正确", CorrectAnswer: "", ParseOK: false,
		},
	}

	if len(got) != len(want) {
		t.Fatalf("题目数量 = %d, 期望 %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 题 = %+v, 期望 %+v", i, got[i], want[i])
		}
	}

	// 空/纯文本/截断
	if qs := DecodeWorkRecordDetail(""); len(qs) != 0 {
		t.Errorf("空字符串应返回空列表, 实际 %+v", qs)
	}
	if qs := DecodeWorkRecordDetail("纯文本"); len(qs) != 0 {
		t.Errorf("纯文本应返回空列表, 实际 %+v", qs)
	}
	if qs := DecodeWorkRecordDetail(`<div class="TiMu singleQuesId" data="7">`); len(qs) != 1 || qs[0].ParseOK {
		t.Errorf("截断页面应解析出 1 题且 ParseOK=false, 实际 %+v", qs)
	}
}

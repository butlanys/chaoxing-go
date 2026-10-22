package htmlx

import (
	"strings"
	"testing"
)

// assertNoPanic 断言 fn 不 panic
func assertNoPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s 发生 panic: %v", name, r)
		}
	}()
	fn()
}

// TestDecodeNoPanic 对空串 / 纯文本 / 截断 HTML 调用全部 Decode* 函数，断言不 panic。
func TestDecodeNoPanic(t *testing.T) {
	inputs := []struct {
		name string
		html string
	}{
		{"空字符串", ""},
		{"纯文本", "这是一段纯文本，不是 HTML。"},
		{"截断 HTML", `<div class="course" id="1"><input class="clazzId" value="2"`},
		{"截断卡片脚本", `<script>var mArg={"defaults":{`},
		{"畸形 mArg", `<script>var mArg={defaults:{a:{b:{c:{d:{e:{f:}}}}}},attachments:[{type:}]};</script>`},
		{"未闭合引号", `<script>var mArg={"a":"b</script>`},
		{"未闭合题目页", `<form><div class="singleQuesId" data="1"><div class="TiMu" data="0"`},
		{"只有尖括号", `<<<>>><div><span></div>`},
		{"未闭合章节页", `<div class="chapter_unit"><ul><li><div id="cur`},
		{"未闭合记录页", `<div class="TiMu singleQuesId" data="1"><div class="Zy_TItle newZy_TItle">`},
	}

	fd := &fakeDecoder{}
	for _, in := range inputs {
		t.Run(in.name, func(t *testing.T) {
			assertNoPanic(t, "DecodeCourseList", func() { DecodeCourseList(in.html) })
			assertNoPanic(t, "DecodeCourseFolder", func() { DecodeCourseFolder(in.html) })
			assertNoPanic(t, "DecodeCoursePoint", func() { DecodeCoursePoint(in.html) })
			assertNoPanic(t, "DecodeCourseCard", func() { DecodeCourseCard(in.html) })
			assertNoPanic(t, "DecodeQuestions(nil)", func() { DecodeQuestions(in.html, nil) })
			assertNoPanic(t, "DecodeQuestions(fd)", func() { DecodeQuestions(in.html, fd) })
			assertNoPanic(t, "DecodeWorkRecordList", func() { DecodeWorkRecordList(in.html) })
			assertNoPanic(t, "DecodeWorkRecordDetail", func() { DecodeWorkRecordDetail(in.html) })
		})
	}
}

// TestDecodeCardDeepNesting 深度嵌套的 JS 字面量不能把栈打爆
func TestDecodeCardDeepNesting(t *testing.T) {
	deep := strings.Repeat("{a:", 300) + "1" + strings.Repeat("}", 300)
	html := `<script>var mArg=` + deep + `};</script>`

	assertNoPanic(t, "DecodeCourseCard", func() {
		jobs, info := DecodeCourseCard(html)
		if len(jobs) != 0 {
			t.Errorf("深层嵌套应解析失败并返回空任务列表, 实际 %+v", jobs)
		}
		if info.NotOpen {
			t.Errorf("不应标记为未开放: %+v", info)
		}
	})
}

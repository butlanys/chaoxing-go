package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/butlanys/chaoxing-go/internal/htmlx"
	"github.com/butlanys/chaoxing-go/internal/tiku"
)

// ---- 题库假实现（只做答案映射，不 mock 解析逻辑） ----

type fakeTiku struct {
	answers    map[string]string // 题目标题 -> 答案（缺失表示未命中）
	judgement  map[string]bool
	submitMode string
	coverRate  float64
	manual     bool
	disabled   bool
	feedback   []string
}

func newFakeTiku(answers map[string]string) *fakeTiku {
	return &fakeTiku{answers: answers, submitMode: "", coverRate: 0.9}
}

func (f *fakeTiku) Name() string { return "假题库" }

func (f *fakeTiku) Query(q tiku.Question) (string, bool) {
	a, ok := f.answers[q.Title]
	return a, ok
}

func (f *fakeTiku) QueryAll(qs []tiku.Question, _ float64) []string {
	out := make([]string, len(qs))
	for i, q := range qs {
		out[i] = f.answers[q.Title]
	}
	return out
}

func (f *fakeTiku) SubmitMode() string { return f.submitMode }
func (f *fakeTiku) CoverRate() float64 { return f.coverRate }

func (f *fakeTiku) JudgementSelect(ans string) bool {
	if v, ok := f.judgement[ans]; ok {
		return v
	}
	switch strings.ToLower(strings.TrimSpace(ans)) {
	case "true", "对", "正确", "√", "是":
		return true
	}
	return false
}

func (f *fakeTiku) SetWorkFeedback(fb []string) { f.feedback = fb }
func (f *fakeTiku) Disabled() bool              { return f.disabled }
func (f *fakeTiku) IsManual() bool              { return f.manual }
func (f *fakeTiku) TrueList() []string          { return []string{"正确", "对", "√", "是"} }
func (f *fakeTiku) FalseList() []string         { return []string{"错误", "错", "×", "否"} }

// ---- 章节检测页面夹具 ----

type questionFixture struct {
	id       string
	typeCode string
	title    string
	options  []string
}

// fiveQuestions 覆盖 5 种题型（single/multiple/judgement/completion/shortanswer）
func fiveQuestions() []questionFixture {
	return []questionFixture{
		{id: "5001", typeCode: "0", title: "1. 中国的首都是哪里？", options: []string{"A. 北京", "B. 上海"}},
		{id: "5002", typeCode: "1", title: "2. 以下属于直辖市的有？", options: []string{"A. 甲", "B. 乙", "C. 丙", "D. 丁"}},
		{id: "5003", typeCode: "3", title: "3. 地球是圆的。", options: []string{"A. 正确", "B. 错误"}},
		{id: "5004", typeCode: "2", title: "4. 填空：中国的首都是____。", options: nil},
		{id: "5005", typeCode: "4", title: "5. 请简述你的看法。", options: nil},
	}
}

// fiveAnswers 与 fiveQuestions 对应的题库答案（全部能匹配到选项，覆盖率 100%）
func fiveAnswers() map[string]string {
	return map[string]string{
		"1. 中国的首都是哪里？":      "北京",
		"2. 以下属于直辖市的有？":     "甲、丙",
		"3. 地球是圆的。":         "正确",
		"4. 填空：中国的首都是____。": "北京",
		"5. 请简述你的看法。":       "我的简答",
	}
}

// questionPageHTML 构造章节检测题目页（form + 5 种题型）
func questionPageHTML(qs []questionFixture) string {
	var sb strings.Builder
	sb.WriteString(`<form method="post" action="/mooc-ans/work/addStudentWorkNew">`)
	sb.WriteString(`<input type="hidden" name="workId" value="9001">`)
	sb.WriteString(`<input type="hidden" name="workAnswerId" value="77001">`)
	sb.WriteString(`<input type="hidden" name="cpi" value="3003">`)
	// 该字段名含 answer，原版 _extract_form_data 会忽略它，提交时应使用解析得到的 answerwqbid
	sb.WriteString(`<input type="hidden" name="answerwqbid" value="should-be-ignored">`)
	for _, q := range qs {
		sb.WriteString(`<div class="singleQuesId" data="` + q.id + `">`)
		sb.WriteString(`<div class="TiMu" data="` + q.typeCode + `"></div>`)
		sb.WriteString(`<div class="Zy_TItle">` + q.title + `</div>`)
		if len(q.options) > 0 {
			sb.WriteString(`<ul>`)
			for _, o := range q.options {
				sb.WriteString(`<li aria-label="` + o + `">` + o + `</li>`)
			}
			sb.WriteString(`</ul>`)
		}
		sb.WriteString(`</div>`)
	}
	sb.WriteString(`</form>`)
	return sb.String()
}

// recordListHTML 构造作答记录列表页
func recordListHTML(times int, score string) string {
	return `<div class="viewNum">第` + itoa(times) + `次</div><div class="viewScore">` + score + `分</div>`
}

// detailFromAnswers 依据"我的答案/正确答案"构造作答详情页（元素为 {题号, 题目, 我的答案, 正确答案}）
func detailFromAnswers(pairs [][4]string) string {
	var sb strings.Builder
	for _, p := range pairs {
		sb.WriteString(`<div class="TiMu singleQuesId" data="` + p[0] + `">`)
		sb.WriteString(`<div class="Zy_TItle"><span class="newZy_TItle">[题型]</span>` + p[1] + `</div>`)
		sb.WriteString(`<span>我的答案:</span><div class="fl answerCon">` + p[2] + `</div>`)
		sb.WriteString(`<span>正确答案:</span><div class="fl answerCon">` + p[3] + `</div>`)
		sb.WriteString(`</div>`)
	}
	return sb.String()
}

// correctDetail 全部正确的作答详情
func correctDetail() string {
	return detailFromAnswers([][4]string{
		{"5001", "1. 中国的首都是哪里？", "A", "A"},
		{"5002", "2. 以下属于直辖市的有？", "AC", "AC"},
		{"5003", "3. 地球是圆的。", "true", "true"},
		{"5004", "4. 填空：中国的首都是____。", "北京", "北京"},
		{"5005", "5. 请简述你的看法。", "我的简答", "我的简答"},
	})
}

// wrongDetail 有错题的作答详情（第 3 题判断题答错）
func wrongDetail() string {
	return detailFromAnswers([][4]string{
		{"5001", "1. 中国的首都是哪里？", "A", "A"},
		{"5002", "2. 以下属于直辖市的有？", "AC", "AC"},
		{"5003", "3. 地球是圆的。", "true", "错误"},
		{"5004", "4. 填空：中国的首都是____。", "北京", "北京"},
		{"5005", "5. 请简述你的看法。", "我的简答", "我的简答"},
	})
}

func workJob() htmlx.Job {
	return htmlx.Job{Type: "workid", JobID: "work-9001", Enc: "enc-1"}
}

func workInfo() htmlx.JobInfo {
	return htmlx.JobInfo{KToken: "kt1", CPI: "3003", KnowledgeID: "5005"}
}

// ---- 测试 ----

func TestStudyWorkSubmitsFullForm(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("mooc-ans/api/work", htmlResponse(http.StatusOK, questionPageHTML(fiveQuestions()))),
		staticRoute("mooc-ans/work/addStudentWorkNew", jsonResponse(t, http.StatusOK, map[string]any{"status": true, "msg": "提交成功"})),
		staticRoute("mooc-ans/work/record-list", htmlResponse(http.StatusOK, recordListHTML(0, "100.0"))),
		staticRoute("mooc-ans/work/record-detail", htmlResponse(http.StatusOK, correctDetail())),
	)

	c := newTestClient(t, tr, Options{})
	c.tiku = newFakeTiku(fiveAnswers())

	if got := c.StudyWork(testCourse(), workJob(), workInfo()); got != ResultSuccess {
		t.Fatalf("StudyWork = %v, 期望 success", got)
	}

	// 题目页请求参数
	fetch := tr.findFirst(t, "mooc-ans/api/work")
	wantFetch := map[string]string{
		"api":          "1",
		"workId":       "9001",
		"jobid":        "work-9001",
		"originJobId":  "work-9001",
		"needRedirect": "true",
		"skipHeader":   "true",
		"knowledgeid":  "5005",
		"ktoken":       "kt1",
		"cpi":          "3003",
		"ut":           "s",
		"clazzId":      "2002",
		"type":         "",
		"enc":          "enc-1",
		"mooc2":        "1",
		"courseid":     "1001",
	}
	for k, v := range wantFetch {
		if got := fetch.query.Get(k); got != v {
			t.Errorf("题目页参数 %s = %q, 期望 %q", k, got, v)
		}
	}

	// 提交表单
	submits := tr.find("mooc-ans/work/addStudentWorkNew")
	if len(submits) != 1 {
		t.Fatalf("提交请求数 = %d, 期望 1 (%v)", len(submits), tr.urls())
	}
	submit := submits[0]
	if submit.method != http.MethodPost {
		t.Errorf("提交方法 = %s, 期望 POST", submit.method)
	}
	if got := submit.header.Get("Content-Type"); got != "application/x-www-form-urlencoded; charset=UTF-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := submit.header.Get("X-Requested-With"); got != "XMLHttpRequest" {
		t.Errorf("X-Requested-With = %q", got)
	}
	if submit.host != "mooc1.chaoxing.com" {
		t.Errorf("Host = %q, 期望 mooc1.chaoxing.com", submit.host)
	}
	if got := submit.header.Get("Origin"); got != "https://mooc1.chaoxing.com" {
		t.Errorf("Origin = %q", got)
	}

	form, err := url.ParseQuery(submit.body)
	if err != nil {
		t.Fatalf("解析提交表单失败: %v", err)
	}
	wantForm := map[string]string{
		"workId":       "9001",
		"workAnswerId": "77001",
		"cpi":          "3003",
		"pyFlag":       "",
		"answerwqbid":  "5001,5002,5003,5004,5005,",
		// single：答案 "北京" 命中选项 A. 北京 -> A
		"answer5001":     "A",
		"answertype5001": "0",
		// multiple：答案 "甲、丙" -> A、C 排序后 AC
		"answer5002":     "AC",
		"answertype5002": "1",
		// judgement："正确" -> true
		"answer5003":     "true",
		"answertype5003": "3",
		// completion
		"answer5004":     "北京",
		"answertype5004": "2",
		// shortanswer
		"answer5005":     "我的简答",
		"answertype5005": "4",
	}
	for k, v := range wantForm {
		if got := form.Get(k); got != v {
			t.Errorf("提交字段 %s = %q, 期望 %q", k, got, v)
		}
	}
	if len(form) != len(wantForm) {
		t.Errorf("提交字段数 = %d, 期望 %d (实际 %v)", len(form), len(wantForm), form)
	}

	// 成绩校验请求
	listReq := tr.findFirst(t, "mooc-ans/work/record-list")
	wantList := map[string]string{
		"courseId": "1001", "classId": "2002", "workId": "9001", "workAnswerId": "77001",
		"cpi": "3003", "api": "1", "mooc2": "1", "ut": "s",
	}
	for k, v := range wantList {
		if got := listReq.query.Get(k); got != v {
			t.Errorf("record-list 参数 %s = %q, 期望 %q", k, got, v)
		}
	}

	detailReq := tr.findFirst(t, "mooc-ans/work/record-detail")
	wantDetail := map[string]string{
		"courseId": "1001", "classId": "2002", "workId": "9001", "workAnswerId": "77001",
		"times": "0", "cpi": "3003", "ut": "s", "isdisplaytable": "0", "firstHeader": "2",
		"isWork": "false", "workSystem": "0", "api": "1", "archive": "false", "mooc2": "1",
	}
	for k, v := range wantDetail {
		if got := detailReq.query.Get(k); got != v {
			t.Errorf("record-detail 参数 %s = %q, 期望 %q", k, got, v)
		}
	}
}

func TestStudyWorkLowCoverageSavesOnly(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("mooc-ans/api/work", htmlResponse(http.StatusOK, questionPageHTML(fiveQuestions()))),
		staticRoute("mooc-ans/work/addStudentWorkNew", jsonResponse(t, http.StatusOK, map[string]any{"status": true, "msg": "保存成功"})),
	)

	c := newTestClient(t, tr, Options{})
	// 只答对 1 题 -> 覆盖率 20% < 90% -> pyFlag="1" 只保存
	c.tiku = newFakeTiku(map[string]string{"1. 中国的首都是哪里？": "北京"})

	if got := c.StudyWork(testCourse(), workJob(), workInfo()); got != ResultSuccess {
		t.Fatalf("低覆盖率应返回 success(仅保存), 实际 %v", got)
	}

	submit := tr.findFirst(t, "mooc-ans/work/addStudentWorkNew")
	form, err := url.ParseQuery(submit.body)
	if err != nil {
		t.Fatalf("解析提交表单失败: %v", err)
	}
	if form.Get("pyFlag") != "1" {
		t.Errorf("pyFlag = %q, 期望 1", form.Get("pyFlag"))
	}
	if got := form.Get("answer5001"); got != "A" {
		t.Errorf("已覆盖题目应带上答案, answer5001 = %q", got)
	}
	// 未覆盖的题目在仅保存模式下答案留空
	if got := form.Get("answer5002"); got != "" {
		t.Errorf("未覆盖题目在保存模式下应为空, answer5002 = %q", got)
	}
	if got := form.Get("answertype5002"); got != "1" {
		t.Errorf("answertype5002 = %q, 期望 1", got)
	}
	// 仅保存时无法判分，不应请求作答记录
	if len(tr.find("mooc-ans/work/record-list")) != 0 {
		t.Errorf("仅保存模式不应请求 record-list: %v", tr.urls())
	}
}

func TestStudyWorkNotCreatedSkips(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("mooc-ans/api/work", htmlResponse(http.StatusOK,
		`<html><body>教师未创建完成该测验</body></html>`)))

	c := newTestClient(t, tr, Options{})
	c.tiku = newFakeTiku(fiveAnswers())

	if got := c.StudyWork(testCourse(), workJob(), workInfo()); got != ResultSuccess {
		t.Fatalf("未创建测验应视为跳过并返回 success, 实际 %v", got)
	}
	if n := len(tr.find("mooc-ans/api/work")); n != 1 {
		t.Errorf("未创建测验不应重试, 请求数 = %d (%v)", n, tr.urls())
	}
	if len(tr.find("mooc-ans/work/addStudentWorkNew")) != 0 {
		t.Errorf("未创建测验不应提交: %v", tr.urls())
	}
}

func TestStudyWorkScoreFallbackFullMarks(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("mooc-ans/api/work", htmlResponse(http.StatusOK, questionPageHTML(fiveQuestions()))),
		staticRoute("mooc-ans/work/addStudentWorkNew", jsonResponse(t, http.StatusOK, map[string]any{"status": true, "msg": "提交成功"})),
		staticRoute("mooc-ans/work/record-list", htmlResponse(http.StatusOK, recordListHTML(1, "100.0"))),
		// 逐题比对不一致（正确答案写成"错误"），但成绩满分 -> 以分数为准通过
		staticRoute("mooc-ans/work/record-detail", htmlResponse(http.StatusOK, wrongDetail())),
	)

	c := newTestClient(t, tr, Options{})
	c.tiku = newFakeTiku(fiveAnswers())

	if got := c.StudyWork(testCourse(), workJob(), workInfo()); got != ResultSuccess {
		t.Fatalf("满分兜底应返回 success, 实际 %v", got)
	}
	// 单轮模式（work_redo_enabled=false）只提交一次
	if n := len(tr.find("mooc-ans/work/addStudentWorkNew")); n != 1 {
		t.Errorf("提交次数 = %d, 期望 1", n)
	}
	detailReq := tr.findFirst(t, "mooc-ans/work/record-detail")
	if got := detailReq.query.Get("times"); got != "1" {
		t.Errorf("record-detail times = %q, 期望 1", got)
	}
}

func TestStudyWorkRedoWithFeedback(t *testing.T) {
	tr := &stubTransport{}
	detailCalls := 0
	tr.handler = routes(t,
		staticRoute("mooc-ans/api/work", htmlResponse(http.StatusOK, questionPageHTML(fiveQuestions()))),
		staticRoute("mooc-ans/work/addStudentWorkNew", jsonResponse(t, http.StatusOK, map[string]any{"status": true, "msg": "提交成功"})),
		staticRoute("mooc-ans/work/record-list", htmlResponse(http.StatusOK, recordListHTML(0, "80.0"))),
		route{match: "mooc-ans/work/record-detail", handler: func(*http.Request, string) (*http.Response, error) {
			detailCalls++
			if detailCalls == 1 {
				return htmlResponse(http.StatusOK, wrongDetail()), nil
			}
			return htmlResponse(http.StatusOK, correctDetail()), nil
		}},
	)

	c := newTestClient(t, tr, Options{WorkRedoEnabled: true, WorkMaxRetries: 1})
	fk := newFakeTiku(fiveAnswers())
	c.tiku = fk

	if got := c.StudyWork(testCourse(), workJob(), workInfo()); got != ResultSuccess {
		t.Fatalf("重做后应返回 success, 实际 %v", got)
	}
	if n := len(tr.find("mooc-ans/work/addStudentWorkNew")); n != 2 {
		t.Errorf("提交次数 = %d, 期望 2（首轮 + 重做轮）", n)
	}
	if len(fk.feedback) != 1 {
		t.Fatalf("重做轮应把上一轮错题反馈传给题库, 实际 %d 条: %v", len(fk.feedback), fk.feedback)
	}
	fb := fk.feedback[0]
	for _, want := range []string{"- 题目：3. 地球是圆的。", "  你的上次答案：true", "  正确答案：错误"} {
		if !strings.Contains(fb, want) {
			t.Errorf("错误反馈缺少 %q, 实际:\n%s", want, fb)
		}
	}
	if got := c.rollbackTimes(); got != 1 {
		t.Errorf("rollback_times = %d, 期望 1", got)
	}
}

func TestStudyWorkFallbackEditablePage(t *testing.T) {
	tr := &stubTransport{}
	workCalls := 0
	tr.handler = routes(t,
		route{match: "mooc-ans/api/work", handler: func(*http.Request, string) (*http.Response, error) {
			workCalls++
			if workCalls == 1 {
				return htmlResponse(http.StatusOK, questionPageHTML(fiveQuestions())), nil
			}
			// 兜底：题目页仍可编辑（含 answerwqbid）-> 推断未全部正确
			return htmlResponse(http.StatusOK, `<html><input name="answerwqbid" value="5001,"></html>`), nil
		}},
		staticRoute("mooc-ans/work/addStudentWorkNew", jsonResponse(t, http.StatusOK, map[string]any{"status": true, "msg": "提交成功"})),
		// 作答记录始终为空 -> 走兜底分支
		staticRoute("mooc-ans/work/record-list", htmlResponse(http.StatusOK, "<html>暂无记录</html>")),
	)

	c := newTestClient(t, tr, Options{})
	c.tiku = newFakeTiku(fiveAnswers())

	if got := c.StudyWork(testCourse(), workJob(), workInfo()); got != ResultSuccess {
		t.Fatalf("兜底分支在单轮模式下应保留成绩并返回 success, 实际 %v", got)
	}
	if n := len(tr.find("mooc-ans/work/record-list")); n != 5 {
		t.Errorf("record-list 重试次数 = %d, 期望 5", n)
	}
	if n := len(tr.find("mooc-ans/api/work")); n != 2 {
		t.Errorf("api/work 请求数 = %d, 期望 2（取题 + 兜底）", n)
	}
}

func TestStudyWorkDisabledTiku(t *testing.T) {
	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{})
	c.tiku = &fakeTiku{disabled: true, coverRate: 0.9}

	if got := c.StudyWork(testCourse(), workJob(), workInfo()); got != ResultSuccess {
		t.Fatalf("题库停用应直接返回 success, 实际 %v", got)
	}
	if tr.count() != 0 {
		t.Errorf("题库停用不应发起请求, 实际 %d 次", tr.count())
	}

	// tiku 为 nil 时同样跳过
	c2 := newTestClient(t, tr, Options{})
	if got := c2.StudyWork(testCourse(), workJob(), workInfo()); got != ResultSuccess {
		t.Fatalf("题库为 nil 应直接返回 success, 实际 %v", got)
	}
}

func TestStudyWorkSubmitFailure(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("mooc-ans/api/work", htmlResponse(http.StatusOK, questionPageHTML(fiveQuestions()))),
		staticRoute("mooc-ans/work/addStudentWorkNew", jsonResponse(t, http.StatusOK, map[string]any{"status": false, "msg": "提交失败"})),
	)

	c := newTestClient(t, tr, Options{})
	c.tiku = newFakeTiku(fiveAnswers())

	if got := c.StudyWork(testCourse(), workJob(), workInfo()); got != ResultError {
		t.Fatalf("提交失败应返回 error, 实际 %v", got)
	}
}

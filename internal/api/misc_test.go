package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/butlanys/chaoxing-go/internal/consts"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
)

func TestStudyDocument(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("ananas/job/document", newResponse(http.StatusOK, "text/plain", "ok")))

	c := newTestClient(t, tr, Options{})
	job := htmlx.Job{Type: "document", JobID: "job-9", OtherInfo: "nodeId_5005-1", JToken: "jt-9"}

	if got := c.StudyDocument(testCourse(), job); got != ResultSuccess {
		t.Fatalf("StudyDocument = %v, 期望 success", got)
	}

	req := tr.findFirst(t, "ananas/job/document")
	wantParams := map[string]string{
		"jobid":       "job-9",
		"knowledgeid": "5005",
		"courseid":    "1001",
		"clazzid":     "2002",
		"jtoken":      "jt-9",
	}
	for k, v := range wantParams {
		if got := req.query.Get(k); got != v {
			t.Errorf("document 参数 %s = %q, 期望 %q", k, got, v)
		}
	}
	if req.query.Get("_dc") == "" {
		t.Errorf("document 请求缺少 _dc 时间戳: %s", req.rawURL)
	}
	// 参数顺序与 Python f-string 一致
	wantPrefix := "https://mooc1.chaoxing.com/ananas/job/document?jobid=job-9&knowledgeid=5005&courseid=1001&clazzid=2002&jtoken=jt-9&_dc="
	if !strings.HasPrefix(req.rawURL, wantPrefix) {
		t.Errorf("document URL = %q, 期望前缀 %q", req.rawURL, wantPrefix)
	}

	tr2 := &stubTransport{}
	tr2.handler = routes(t, staticRoute("ananas/job/document", newResponse(http.StatusInternalServerError, "text/plain", "boom")))
	c2 := newTestClient(t, tr2, Options{})
	if got := c2.StudyDocument(testCourse(), job); got != ResultError {
		t.Errorf("非 200 时 StudyDocument = %v, 期望 error", got)
	}
}

func TestStudyRead(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("ananas/job/readv2", jsonResponse(t, http.StatusOK, map[string]any{"msg": "阅读任务完成"})))

	c := newTestClient(t, tr, Options{})
	job := htmlx.Job{Type: "read", JobID: "job-7", JToken: "jt-7"}
	info := htmlx.JobInfo{KnowledgeID: "5005"}

	if got := c.StudyRead(testCourse(), job, info); got != ResultSuccess {
		t.Fatalf("StudyRead = %v, 期望 success", got)
	}
	req := tr.findFirst(t, "ananas/job/readv2")
	wantParams := map[string]string{
		"jobid":       "job-7",
		"knowledgeid": "5005",
		"jtoken":      "jt-7",
		"courseid":    "1001",
		"clazzid":     "2002",
	}
	for k, v := range wantParams {
		if got := req.query.Get(k); got != v {
			t.Errorf("readv2 参数 %s = %q, 期望 %q", k, got, v)
		}
	}

	tr2 := &stubTransport{}
	tr2.handler = routes(t, staticRoute("ananas/job/readv2", newResponse(http.StatusInternalServerError, "text/plain", "boom")))
	c2 := newTestClient(t, tr2, Options{})
	if got := c2.StudyRead(testCourse(), job, info); got != ResultError {
		t.Errorf("非 200 时 StudyRead = %v, 期望 error", got)
	}
}

func TestAccessChapterForCount(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("studentstudyAjax", htmlResponse(http.StatusOK, "<html>章节页</html>")))

	c := newTestClient(t, tr, Options{})
	body, ok := c.accessChapterForCount(testCourse(), testPoint())
	if !ok || !strings.Contains(body, "章节页") {
		t.Fatalf("accessChapterForCount = (%q, %v), 期望成功", body, ok)
	}
	req := tr.findFirst(t, "studentstudyAjax")
	want := map[string]string{
		"courseId": "1001", "clazzid": "2002", "chapterId": "4004", "cpi": "3003",
		"verificationcode": "", "mooc2": "1", "microTopicId": "0", "editorPreview": "0",
	}
	for k, v := range want {
		if got := req.query.Get(k); got != v {
			t.Errorf("studentstudyAjax 参数 %s = %q, 期望 %q", k, got, v)
		}
	}
}

func TestExtractAndSendSetlog(t *testing.T) {
	setlogURL := "https://fystat-ans.chaoxing.com/log/setlog?encode=abc&t=1"
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("fystat-ans.chaoxing.com/log/setlog", newResponse(http.StatusOK, "text/plain", "ok")))

	c := newTestClient(t, tr, Options{})
	c.extractAndSendSetlog(`<html><script src="` + setlogURL + `"></script></html>`)

	reqs := tr.find("fystat-ans.chaoxing.com/log/setlog")
	if len(reqs) != 1 {
		t.Fatalf("setlog 请求数 = %d, 期望 1 (%v)", len(reqs), tr.urls())
	}
	if reqs[0].rawURL != setlogURL {
		t.Errorf("setlog URL = %q, 期望 %q", reqs[0].rawURL, setlogURL)
	}

	// 未找到 setlog 时不应发请求
	tr2 := &stubTransport{}
	c2 := newTestClient(t, tr2, Options{})
	c2.extractAndSendSetlog("<html>没有脚本</html>")
	if tr2.count() != 0 {
		t.Errorf("未找到 setlog 时不应发请求, 实际 %d 次", tr2.count())
	}
}

func TestSendMonitorHeartbeat(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("detect.chaoxing.com/api/monitor", newResponse(http.StatusOK, "text/plain", "jsonp1()")))

	c := newTestClient(t, tr, Options{})
	c.sendMonitorHeartbeat(testCourse(), testPoint())

	req := tr.findFirst(t, "detect.chaoxing.com/api/monitor")
	want := map[string]string{
		"refer": "http://i.mooc.chaoxing.com",
		"from":  "",
		"fid":   "1234",
	}
	for k, v := range want {
		if got := req.query.Get(k); got != v {
			t.Errorf("monitor 参数 %s = %q, 期望 %q", k, got, v)
		}
	}
	if req.query.Get("version") == "" || req.query.Get("t") == "" {
		t.Errorf("monitor 缺少时间戳参数: %s", req.rawURL)
	}
	cb := req.query.Get("jsoncallback")
	if !strings.HasPrefix(cb, "jsonp") || len(cb) != len("jsonp")+21 {
		t.Errorf("jsoncallback = %q, 期望 jsonp + 21 位数字", cb)
	}
	if cb[len("jsonp")] == '0' {
		t.Errorf("jsoncallback 首位不应为 0: %q", cb)
	}
	wantReferer := "https://mooc1.chaoxing.com/mycourse/studentstudy?chapterId=4004&courseId=1001&clazzid=2002&cpi=3003&mooc2=1"
	if got := req.header.Get("Referer"); got != wantReferer {
		t.Errorf("monitor Referer = %q, 期望 %q", got, wantReferer)
	}
}

func TestIncreaseChapterLearningCount(t *testing.T) {
	fastChapter(t)

	setlogURL := "https://fystat-ans.chaoxing.com/log/setlog?encode=abc"
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("studentstudyAjax", htmlResponse(http.StatusOK, `<html><script src="`+setlogURL+`"></script></html>`)),
		staticRoute("fystat-ans.chaoxing.com/log/setlog", newResponse(http.StatusOK, "text/plain", "ok")),
		staticRoute("detect.chaoxing.com/api/monitor", newResponse(http.StatusOK, "text/plain", "ok")),
	)

	c := newTestClient(t, tr, Options{})
	var progress [][2]int
	got := c.IncreaseChapterLearningCount(testCourse(), []htmlx.Point{testPoint()}, 2, func(n, target int) {
		progress = append(progress, [2]int{n, target})
	})
	if got != ResultSuccess {
		t.Fatalf("IncreaseChapterLearningCount = %v, 期望 success", got)
	}
	if len(progress) != 2 || progress[0] != [2]int{1, 2} || progress[1] != [2]int{2, 2} {
		t.Errorf("进度回调 = %v, 期望 [[1 2] [2 2]]", progress)
	}
	if n := len(tr.find("studentstudyAjax")); n != 2 {
		t.Errorf("章节访问次数 = %d, 期望 2", n)
	}
	if n := len(tr.find("fystat-ans.chaoxing.com/log/setlog")); n != 2 {
		t.Errorf("setlog 次数 = %d, 期望 2", n)
	}
	// 每次访问发两次心跳
	if n := len(tr.find("detect.chaoxing.com/api/monitor")); n != 4 {
		t.Errorf("monitor 心跳次数 = %d, 期望 4", n)
	}
}

func TestIncreaseChapterLearningCountStopsAfterFailures(t *testing.T) {
	fastChapter(t)

	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("studentstudyAjax", htmlResponse(http.StatusInternalServerError, "boom")))

	c := newTestClient(t, tr, Options{})
	got := c.IncreaseChapterLearningCount(testCourse(), []htmlx.Point{testPoint()}, 5, nil)
	if got != ResultError {
		t.Fatalf("连续失败应返回 error, 实际 %v", got)
	}
	if n := len(tr.find("studentstudyAjax")); n != 10 {
		t.Errorf("连续失败上限为 10, 实际请求 %d 次", n)
	}
}

func TestIncreaseChapterLearningCountEmptyPoints(t *testing.T) {
	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{})
	if got := c.IncreaseChapterLearningCount(testCourse(), nil, 5, nil); got != ResultSuccess {
		t.Fatalf("空章节应返回 success, 实际 %v", got)
	}
	if tr.count() != 0 {
		t.Errorf("空章节不应发请求, 实际 %d 次", tr.count())
	}
}

func TestActivityList(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("active/student/activelist", jsonResponse(t, http.StatusOK, map[string]any{
		"result": 1,
		"data": map[string]any{
			"activeList": []any{
				map[string]any{"id": "A1", "name": "签到", "status": 1, "type": 2, "otherId": "O1", "startTime": "2026-01-01"},
			},
		},
	})))

	c := newTestClient(t, tr, Options{})
	list, err := c.ActivityList(testCourse())
	if err != nil {
		t.Fatalf("ActivityList 返回错误: %v", err)
	}
	if len(list) != 1 || list[0].ID != "A1" || list[0].Name != "签到" || list[0].Type != 2 || list[0].Status != 1 {
		t.Fatalf("活动解析异常: %+v", list)
	}

	req := tr.findFirst(t, "active/student/activelist")
	want := map[string]string{
		"fid": "1234", "courseId": "1001", "classId": "2002", "showNotStartedActive": "0",
	}
	for k, v := range want {
		if got := req.query.Get(k); got != v {
			t.Errorf("activelist 参数 %s = %q, 期望 %q", k, got, v)
		}
	}
	if req.query.Get("_") == "" {
		t.Errorf("activelist 缺少 _ 时间戳参数")
	}
}

func TestActivityListResultNotOne(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("active/student/activelist", jsonResponse(t, http.StatusOK, map[string]any{
		"result":   0,
		"errorMsg": "未登录",
	})))

	c := newTestClient(t, tr, Options{})
	list, err := c.ActivityList(testCourse())
	if err != nil {
		t.Fatalf("result!=1 不应返回错误: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("result!=1 应返回空列表, 实际 %+v", list)
	}
}

// 原版使用 allow_redirects=False，Go 版必须不跟随重定向（只发一次请求）
func TestActivityListNoRedirect(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, route{match: "active/student/activelist", handler: func(*http.Request, string) (*http.Response, error) {
		resp := newResponse(http.StatusFound, "text/html", "")
		resp.Header.Set("Location", "https://passport2.chaoxing.com/login")
		return resp, nil
	}})

	c := newTestClient(t, tr, Options{})
	list, err := c.ActivityList(testCourse())
	if err != nil {
		t.Fatalf("ActivityList 返回错误: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("302 应返回空列表, 实际 %+v", list)
	}
	if n := tr.count(); n != 1 {
		t.Errorf("不应跟随重定向, 请求数 = %d (%v)", n, tr.urls())
	}
}

func TestSignIn(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("newsign/preSign", htmlResponse(http.StatusOK, "<html>签到页</html>")),
		staticRoute("pptSign/stuSignajax", newResponse(http.StatusOK, "text/plain", "success")),
	)

	c := newTestClient(t, tr, Options{})
	text, err := c.SignIn(testCourse(), "ACT-1")
	if err != nil {
		t.Fatalf("SignIn 返回错误: %v", err)
	}
	if text != "success" {
		t.Errorf("签到响应 = %q, 期望 success", text)
	}

	pre := tr.findFirst(t, "newsign/preSign")
	wantPre := map[string]string{
		"general": "1", "sys": "1", "ls": "1", "appType": "15", "tid": "", "ut": "s",
		"uid": "12345678", "activePrimaryId": "ACT-1", "courseId": "1001", "classId": "2002",
	}
	for k, v := range wantPre {
		if got := pre.query.Get(k); got != v {
			t.Errorf("preSign 参数 %s = %q, 期望 %q", k, got, v)
		}
	}

	sign := tr.findFirst(t, "pptSign/stuSignajax")
	wantSign := map[string]string{
		"activeId": "ACT-1", "uid": "12345678", "fid": "1234", "courseId": "1001", "classId": "2002",
		"clientip": "", "objectId": "aaa", "name": "", "useragent": "", "latitude": "-1",
		"longitude": "-1", "appType": "15",
	}
	for k, v := range wantSign {
		if got := sign.query.Get(k); got != v {
			t.Errorf("stuSignajax 参数 %s = %q, 期望 %q", k, got, v)
		}
	}
}

func liveJob() htmlx.Job {
	return htmlx.Job{
		Type: "live",
		Name: "测试直播",
		Property: map[string]any{
			"liveId":     "L1",
			"streamName": "SN-1",
			"vdoid":      "VD-1",
			"_jobid":     "J-1",
		},
	}
}

func TestLiveStatus(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("ananas/live/liveinfo", jsonResponse(t, http.StatusOK, map[string]any{
		"temp": map[string]any{"data": map[string]any{"duration": 600}},
	})))

	c := newTestClient(t, tr, Options{})
	status, err := c.LiveStatus(testCourse(), liveJob(), "5005")
	if err != nil {
		t.Fatalf("LiveStatus 返回错误: %v", err)
	}
	temp, _ := status["temp"].(map[string]any)
	data, _ := temp["data"].(map[string]any)
	if jsonInt(data["duration"]) != 600 {
		t.Fatalf("直播时长解析异常: %v", status)
	}

	req := tr.findFirst(t, "ananas/live/liveinfo")
	want := map[string]string{
		"liveid": "L1", "userid": "12345678", "clazzid": "2002",
		"knowledgeid": "5005", "courseid": "1001", "jobid": "J-1", "ut": "s",
	}
	for k, v := range want {
		if got := req.query.Get(k); got != v {
			t.Errorf("liveinfo 参数 %s = %q, 期望 %q", k, got, v)
		}
	}
	if got := req.header.Get("Referer"); got != consts.LiveHeaders["Referer"] {
		t.Errorf("liveinfo Referer = %q, 期望 %q", got, consts.LiveHeaders["Referer"])
	}
}

func TestLiveStatusMissingParams(t *testing.T) {
	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{})
	job := htmlx.Job{Type: "live", Property: map[string]any{}}
	if _, err := c.LiveStatus(testCourse(), job, "5005"); err == nil {
		t.Fatal("缺少直播参数应返回错误")
	}
	if tr.count() != 0 {
		t.Errorf("缺少参数不应发请求, 实际 %d 次", tr.count())
	}
}

func TestRunLive(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("ananas/live/liveinfo", jsonResponse(t, http.StatusOK, map[string]any{
			"temp": map[string]any{"data": map[string]any{"duration": 600}},
		})),
		staticRoute("zhibo.chaoxing.com/saveTimePc", newResponse(http.StatusOK, "text/plain", "@success")),
	)

	c := newTestClient(t, tr, Options{})
	// speed=100、duration=600 -> 调整后 6s -> 向上取整为 1 分钟
	got := c.RunLive(testCourse(), liveJob(), "5005", 100)
	if got != ResultSuccess {
		t.Fatalf("RunLive = %v, 期望 success", got)
	}

	saves := tr.find("zhibo.chaoxing.com/saveTimePc")
	if len(saves) != 1 {
		t.Fatalf("时长提交次数 = %d, 期望 1 (%v)", len(saves), tr.urls())
	}
	wantPrefix := "https://zhibo.chaoxing.com/saveTimePc?streamName=SN-1&vdoid=VD-1&userId=12345678&isStart=0&t="
	if !strings.HasPrefix(saves[0].rawURL, wantPrefix) {
		t.Errorf("saveTimePc URL = %q, 期望前缀 %q", saves[0].rawURL, wantPrefix)
	}
	if !strings.HasSuffix(saves[0].rawURL, "&courseId=1001") {
		t.Errorf("saveTimePc URL 缺少 courseId: %q", saves[0].rawURL)
	}
	if got := saves[0].header.Get("Referer"); got != consts.LiveHeaders["Referer"] {
		t.Errorf("saveTimePc Referer = %q", got)
	}
}

func TestRunLiveMissingParams(t *testing.T) {
	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{})
	job := htmlx.Job{Type: "live", Property: map[string]any{"liveId": "L1"}}
	if got := c.RunLive(testCourse(), job, "5005", 1); got != ResultError {
		t.Fatalf("缺少 streamName/vdoid 应返回 error, 实际 %v", got)
	}
	if tr.count() != 0 {
		t.Errorf("缺少参数不应发请求, 实际 %d 次", tr.count())
	}
}

func TestRunLiveDefaultDuration(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		// 无 duration -> 按 30 分钟处理
		staticRoute("ananas/live/liveinfo", jsonResponse(t, http.StatusOK, map[string]any{"temp": map[string]any{"data": map[string]any{}}})),
		staticRoute("zhibo.chaoxing.com/saveTimePc", newResponse(http.StatusOK, "text/plain", "@success")),
	)

	c := newTestClient(t, tr, Options{})
	// speed=1000：1800s/1000 = 1.8 -> int=1 -> 向上取整为 1 分钟
	if got := c.RunLive(testCourse(), liveJob(), "5005", 1000); got != ResultSuccess {
		t.Fatalf("RunLive = %v, 期望 success", got)
	}
	if n := len(tr.find("zhibo.chaoxing.com/saveTimePc")); n != 1 {
		t.Errorf("默认 30 分钟 + speed=1000 应提交 1 次, 实际 %d", n)
	}
}

// 直播时长提交失败时应重试一次（间隔 5s，这里只校验请求次数为 2）
func TestRunLiveRetriesOnFailure(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("ananas/live/liveinfo", jsonResponse(t, http.StatusOK, map[string]any{
			"temp": map[string]any{"data": map[string]any{"duration": 600}},
		})),
		staticRoute("zhibo.chaoxing.com/saveTimePc", newResponse(http.StatusOK, "text/plain", "@fail")),
	)

	c := newTestClient(t, tr, Options{})
	if got := c.RunLive(testCourse(), liveJob(), "5005", 100); got != ResultSuccess {
		t.Fatalf("RunLive = %v, 期望 success", got)
	}
	// 首次失败 -> sleep 5s -> 重试一次，共 2 次
	if n := len(tr.find("zhibo.chaoxing.com/saveTimePc")); n != 2 {
		t.Errorf("失败应重试一次, 实际请求 %d 次", n)
	}
}

func TestBuildURL(t *testing.T) {
	got := buildURL("https://example.com/x", [2]string{"a", "1"}, [2]string{"b", "中 文"}, [2]string{"c", ""})
	want := "https://example.com/x?a=1&b=%E4%B8%AD+%E6%96%87&c="
	if got != want {
		t.Errorf("buildURL = %q, 期望 %q", got, want)
	}
}

func TestJobProperty(t *testing.T) {
	job := htmlx.Job{Property: map[string]any{"liveId": "L1", "vdoid": 12345, "nilVal": nil}}
	if got := jobProperty(job, "liveId"); got != "L1" {
		t.Errorf("jobProperty(liveId) = %q", got)
	}
	if got := jobProperty(job, "vdoid"); got != "12345" {
		t.Errorf("jobProperty(vdoid) = %q, 期望 12345", got)
	}
	if got := jobProperty(job, "nilVal"); got != "" {
		t.Errorf("jobProperty(nilVal) = %q, 期望空串", got)
	}
	if got := jobProperty(job, "missing"); got != "" {
		t.Errorf("jobProperty(missing) = %q, 期望空串", got)
	}
	if got := jobProperty(htmlx.Job{}, "liveId"); got != "" {
		t.Errorf("nil property 应返回空串, 实际 %q", got)
	}
}

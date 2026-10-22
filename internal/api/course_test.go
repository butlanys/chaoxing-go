package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/butlanys/chaoxing-go/internal/htmlx"
)

// courseListHTML 一份最小可解析的课程列表页（字段名与 decode_course_list 对齐）
func courseListHTML(courseID, clazzID, cpi, title string) string {
	return `<div class="course" id="course_` + courseID + `" info="1" roleid="3">
	<a href="/mooc-ans/mycourse/studentcourse?courseid=` + courseID + `&clazzid=` + clazzID + `&cpi=` + cpi + `&ut=s">进入</a>
	<input class="clazzId" value="` + clazzID + `">
	<input class="courseId" value="` + courseID + `">
	<span class="course-name" title="` + title + `"></span>
	<p class="margint10" title="课程简介"></p>
	<p class="color3" title="张老师"></p>
</div>`
}

// courseFolderHTML 一份最小可解析的二级文件夹页
func courseFolderHTML(folderID, rename string) string {
	return `<ul class="file-list"><li fileid="` + folderID + `"><input class="rename-input" value="` + rename + `"></li></ul>`
}

// coursePointHTML 一份最小可解析的章节列表页
func coursePointHTML(pointID, title, jobCount string) string {
	return `<div class="chapter_unit"><ul><li><div id="cur` + pointID + `">
	<a class="clicktitle">` + title + `</a>
	<input class="knowledgeJobCount" value="` + jobCount + `">
	<span class="bntHoverTips">已完成</span>
	</div></li></ul></div>`
}

// cardHTML 一份最小可解析的任务点卡片页（mArg 为严格 JSON，空格会被原版正则移除）
// 注意：原版 _process_attachment_cards 只在 card["job"] 非空时才识别 video/document/workid，
// 因此夹具里必须带 job 字段（真实超星返回也如此）。
func cardHTML(jobID, objectID, otherInfo, name string, playTime int) string {
	return `<html><script type="text/javascript">mArg={"attachments":[{"jobid":"` + jobID +
		`","type":"video","job":{"id":"` + jobID + `"},"objectId":"` + objectID + `","otherInfo":"` + otherInfo +
		`","property":{"name":"` + name + `"},"mid":"mid-1","playTime":` + itoa(playTime) +
		`,"jtoken":"jt1","enc":"enc1"}],"defaults":{"ktoken":"kt1","cpi":"3003","knowledgeid":"5005","reportTimeInterval":60}};</script></html>`
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf []byte
	for v > 0 {
		buf = append([]byte{byte('0' + v%10)}, buf...)
		v /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}

func TestCourseList(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		route{match: "mooc2-ans/visit/interaction", handler: func(*http.Request, string) (*http.Response, error) {
			return htmlResponse(http.StatusOK, courseFolderHTML("77", "我的文件夹")), nil
		}},
		route{match: "mooc2-ans/visit/courselistdata", handler: func(req *http.Request, body string) (*http.Response, error) {
			form, _ := url.ParseQuery(body)
			if form.Get("courseFolderId") == "77" {
				return htmlResponse(http.StatusOK, courseListHTML("2001", "2002", "3003", "文件夹课程")), nil
			}
			return htmlResponse(http.StatusOK, courseListHTML("1001", "1002", "1003", "一级课程")), nil
		}},
	)

	c := newTestClient(t, tr, Options{})
	courses, err := c.CourseList()
	if err != nil {
		t.Fatalf("CourseList 返回错误: %v", err)
	}
	if len(courses) != 2 {
		t.Fatalf("课程数 = %d, 期望 2 (%+v)", len(courses), courses)
	}
	if courses[0].CourseID != "1001" || courses[0].ClazzID != "1002" || courses[0].CPI != "1003" || courses[0].Title != "一级课程" {
		t.Errorf("一级课程解析异常: %+v", courses[0])
	}
	if courses[1].CourseID != "2001" || courses[1].Title != "文件夹课程" {
		t.Errorf("文件夹课程解析异常: %+v", courses[1])
	}

	listReqs := tr.find("visit/courselistdata")
	if len(listReqs) != 2 {
		t.Fatalf("courselistdata 请求数 = %d, 期望 2 (%v)", len(listReqs), tr.urls())
	}
	first, _ := url.ParseQuery(listReqs[0].body)
	if first.Get("courseType") != "1" || first.Get("courseFolderId") != "0" || first.Get("query") != "" || first.Get("superstarClass") != "0" {
		t.Errorf("一级列表表单异常: %v", first)
	}
	wantReferer := "https://mooc2-ans.chaoxing.com/mooc2-ans/visit/interaction?moocDomain=https://mooc1-1.chaoxing.com/mooc-ans"
	if got := listReqs[0].header.Get("Referer"); got != wantReferer {
		t.Errorf("Referer = %q, 期望 %q", got, wantReferer)
	}
	second, _ := url.ParseQuery(listReqs[1].body)
	if second.Get("courseFolderId") != "77" {
		t.Errorf("二级列表 courseFolderId = %q, 期望 77", second.Get("courseFolderId"))
	}

	if got := tr.findFirst(t, "visit/interaction"); got.method != http.MethodGet {
		t.Errorf("interaction 方法 = %s, 期望 GET", got.method)
	}
}

func TestCoursePoints(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("mycourse/studentcourse", htmlResponse(http.StatusOK,
		coursePointHTML("4004", "第一章", "2")+coursePointHTML("4005", "第二章", "1"))))

	c := newTestClient(t, tr, Options{})
	pl, err := c.CoursePoints("1001", "2002", "3003")
	if err != nil {
		t.Fatalf("CoursePoints 返回错误: %v", err)
	}
	if len(pl.Points) != 2 {
		t.Fatalf("章节数 = %d, 期望 2 (%+v)", len(pl.Points), pl)
	}
	if pl.Points[0].ID != "4004" || pl.Points[0].Title != "第一章" || pl.Points[0].JobCount != "2" || !pl.Points[0].HasFinished {
		t.Errorf("章节解析异常: %+v", pl.Points[0])
	}

	req := tr.findFirst(t, "mycourse/studentcourse")
	wantURL := "https://mooc2-ans.chaoxing.com/mooc2-ans/mycourse/studentcourse?courseid=1001&clazzid=2002&cpi=3003&ut=s"
	if req.rawURL != wantURL {
		t.Errorf("URL = %q, 期望 %q", req.rawURL, wantURL)
	}
}

func TestJobListRequestsAllSevenNums(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, route{match: "mooc-ans/knowledge/cards", handler: func(req *http.Request, _ string) (*http.Response, error) {
		if req.URL.Query().Get("num") == "0" {
			return htmlResponse(http.StatusOK, cardHTML("job-1", "obj-1", "nodeId_5005-1", "视频1", 1000)), nil
		}
		return htmlResponse(http.StatusOK, "<html></html>"), nil
	}})

	c := newTestClient(t, tr, Options{})
	jobs, info, err := c.JobList(testCourse(), testPoint())
	if err != nil {
		t.Fatalf("JobList 返回错误: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("任务点数 = %d, 期望 1 (%+v)", len(jobs), jobs)
	}
	j := jobs[0]
	if j.JobID != "job-1" || j.ObjectID != "obj-1" || j.Name != "视频1" || j.PlayTime != 1000 || j.Type != "video" {
		t.Errorf("任务点解析异常: %+v", j)
	}
	if info.KToken != "kt1" || info.KnowledgeID != "5005" || info.CPI != "3003" || info.ReportTimeInterval != 60 {
		t.Errorf("jobInfo 合并异常: %+v", info)
	}
	if info.NotOpen {
		t.Errorf("jobInfo.NotOpen 应为 false")
	}

	reqs := tr.find("mooc-ans/knowledge/cards")
	if len(reqs) != 7 {
		t.Fatalf("cards 请求数 = %d, 期望 7 (%v)", len(reqs), tr.urls())
	}
	nums := make([]string, 0, 7)
	for _, r := range reqs {
		q := r.query
		nums = append(nums, q.Get("num"))
		wantParams := map[string]string{
			"clazzid":     "2002",
			"courseid":    "1001",
			"knowledgeid": "4004",
			"ut":          "s",
			"cpi":         "3003",
			"v":           "2025-0424-1038-3",
			"mooc2":       "1",
		}
		for k, v := range wantParams {
			if got := q.Get(k); got != v {
				t.Errorf("cards 参数 %s = %q, 期望 %q", k, got, v)
			}
		}
	}
	if strings.Join(nums, "") != "0123456" {
		t.Errorf("num 序列 = %v, 期望 0..6", nums)
	}

	// job 列表非空时不应触发空页面请求
	if len(tr.find("studentstudyAjax")) != 0 {
		t.Errorf("任务点非空时不应请求 studentstudyAjax: %v", tr.urls())
	}
}

func TestJobListNotOpenReturnsEarly(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("mooc-ans/knowledge/cards", htmlResponse(http.StatusOK,
		`<html><div>章节未开放</div></html>`)))

	c := newTestClient(t, tr, Options{})
	jobs, info, err := c.JobList(testCourse(), testPoint())
	if err != nil {
		t.Fatalf("JobList 返回错误: %v", err)
	}
	if !info.NotOpen {
		t.Fatal("应识别为章节未开放")
	}
	if len(jobs) != 0 {
		t.Errorf("未开放章节不应返回任务点: %+v", jobs)
	}
	if got := len(tr.find("mooc-ans/knowledge/cards")); got != 1 {
		t.Errorf("未开放时应提前返回, 请求数 = %d, 期望 1", got)
	}
}

func TestJobListEmptyTriggersEmptyPage(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("mooc-ans/knowledge/cards", htmlResponse(http.StatusOK, "<html></html>")),
		staticRoute("studentstudyAjax", htmlResponse(http.StatusOK, "<html>ok</html>")),
	)

	c := newTestClient(t, tr, Options{})
	jobs, _, err := c.JobList(testCourse(), testPoint())
	if err != nil {
		t.Fatalf("JobList 返回错误: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("期望无任务点, 实际 %+v", jobs)
	}
	if got := len(tr.find("mooc-ans/knowledge/cards")); got != 7 {
		t.Errorf("cards 请求数 = %d, 期望 7", got)
	}
	empty := tr.find("studentstudyAjax")
	if len(empty) != 1 {
		t.Fatalf("应触发一次空页面请求, 实际 %v", tr.urls())
	}
	want := map[string]string{
		"courseId":         "1001",
		"clazzid":          "2002",
		"chapterId":        "4004",
		"cpi":              "3003",
		"verificationcode": "",
		"mooc2":            "1",
		"microTopicId":     "0",
		"editorPreview":    "0",
	}
	for k, v := range want {
		if got := empty[0].query.Get(k); got != v {
			t.Errorf("空页面参数 %s = %q, 期望 %q", k, got, v)
		}
	}
}

func TestJobListNon200ReturnsError(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("mooc-ans/knowledge/cards", htmlResponse(http.StatusBadGateway, "boom")))

	c := newTestClient(t, tr, Options{})
	jobs, info, err := c.JobList(testCourse(), testPoint())
	if err == nil {
		t.Fatal("非 200 应返回错误")
	}
	if jobs != nil {
		t.Errorf("出错时任务点应为 nil, 实际 %+v", jobs)
	}
	if info != (htmlx.JobInfo{}) {
		t.Errorf("出错时 jobInfo 应为零值, 实际 %+v", info)
	}
}

func TestStudyEmptyPage(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("studentstudyAjax", htmlResponse(http.StatusOK, "<html>ok</html>")))

	c := newTestClient(t, tr, Options{})
	if got := c.StudyEmptyPage(testCourse(), testPoint()); got != ResultSuccess {
		t.Errorf("StudyEmptyPage = %v, 期望 success", got)
	}

	tr2 := &stubTransport{}
	tr2.handler = routes(t, staticRoute("studentstudyAjax", htmlResponse(http.StatusInternalServerError, "boom")))
	c2 := newTestClient(t, tr2, Options{})
	if got := c2.StudyEmptyPage(testCourse(), testPoint()); got != ResultError {
		t.Errorf("非 200 时 StudyEmptyPage = %v, 期望 error", got)
	}
}

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/butlanys/chaoxing-go/internal/consts"
	"github.com/butlanys/chaoxing-go/internal/crypto"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
)

// videoStatusJSON 构造 /ananas/status 的成功响应
func videoStatusJSON(t *testing.T, dtoken string, duration int) *http.Response {
	t.Helper()
	return jsonResponse(t, http.StatusOK, map[string]any{
		"status":   "success",
		"dtoken":   dtoken,
		"crc":      "crc-1",
		"key":      "key-1",
		"duration": duration,
		"playTime": 0,
	})
}

func TestVideoProgressLogParams(t *testing.T) {
	fastVideo(t)

	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("mooc-ans/multimedia/log/a", jsonResponse(t, http.StatusOK,
		map[string]any{"isPassed": true})))

	c := newTestClient(t, tr, Options{})
	job := testJob()
	job.Rt = "0.9"
	job.AttDuration = "500"
	job.AttDurationEnc = "enc-att"
	job.VideoFaceCaptureEnc = "enc-face"

	passed, state := c.videoProgressLog(testCourse(), job, htmlx.JobInfo{}, "dtoken-1", 600, 120, "Video", 3, consts.VideoHeaders)
	if !passed || state != http.StatusOK {
		t.Fatalf("videoProgressLog = (%v, %d), 期望 (true, 200)", passed, state)
	}

	req := tr.findFirst(t, "mooc-ans/multimedia/log/a")
	wantURL := "https://mooc1.chaoxing.com/mooc-ans/multimedia/log/a/3003/dtoken-1"
	if !strings.HasPrefix(req.rawURL, wantURL) {
		t.Errorf("上报 URL = %q, 期望前缀 %q", req.rawURL, wantURL)
	}
	if req.method != http.MethodGet {
		t.Errorf("上报方法 = %s, 期望 GET", req.method)
	}
	if got := req.header.Get("Referer"); got != consts.VideoHeaders["Referer"] {
		t.Errorf("Referer = %q, 期望 %q", got, consts.VideoHeaders["Referer"])
	}

	wantParams := map[string]string{
		"clazzId":             "2002",
		"playingTime":         "120",
		"duration":            "600",
		"clipTime":            "0_600",
		"objectId":            "obj-1",
		"otherInfo":           "nodeId_5005-1",
		"courseId":            "1001",
		"jobid":               "job-1",
		"userid":              "12345678",
		"isdrag":              "3",
		"view":                "pc",
		"enc":                 crypto.VideoEnc("2002", "12345678", "job-1", "obj-1", 120, 600),
		"dtype":               "Video",
		"rt":                  "0.9",
		"videoFaceCaptureEnc": "enc-face",
		"attDuration":         "500",
		"attDurationEnc":      "enc-att",
	}
	for k, v := range wantParams {
		if got := req.query.Get(k); got != v {
			t.Errorf("上报参数 %s = %q, 期望 %q", k, got, v)
		}
	}
	if req.query.Get("_t") == "" {
		t.Errorf("上报参数缺少 _t 时间戳")
	}
	// 只应发一次请求（rt 已给定）
	if tr.count() != 1 {
		t.Errorf("请求数 = %d, 期望 1 (%v)", tr.count(), tr.urls())
	}
}

func TestVideoProgressLogRtFromOtherInfo(t *testing.T) {
	fastVideo(t)

	cases := []struct {
		name      string
		otherInfo string
		wantRt    string
	}{
		{"otherinfo 中 rt_1", "nodeId_5005-1-rt_1-x", "1"},
		{"otherinfo 中 rt_d", "nodeId_5005-1-rt_d-x", "0.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &stubTransport{}
			tr.handler = routes(t, staticRoute("mooc-ans/multimedia/log/a", jsonResponse(t, http.StatusOK,
				map[string]any{"isPassed": false})))

			c := newTestClient(t, tr, Options{})
			job := testJob()
			job.OtherInfo = tc.otherInfo
			job.Rt = ""

			passed, state := c.videoProgressLog(testCourse(), job, htmlx.JobInfo{}, "tok", 100, 10, "Video", 3, consts.VideoHeaders)
			if passed || state != http.StatusOK {
				t.Fatalf("结果 = (%v, %d), 期望 (false, 200)", passed, state)
			}
			if tr.count() != 1 {
				t.Fatalf("推导出 rt 后应只发一次请求, 实际 %d (%v)", tr.count(), tr.urls())
			}
			req := tr.findFirst(t, "mooc-ans/multimedia/log/a")
			if got := req.query.Get("rt"); got != tc.wantRt {
				t.Errorf("rt = %q, 期望 %q", got, tc.wantRt)
			}
		})
	}
}

func TestVideoProgressLogNoRtTriesBothValues(t *testing.T) {
	fastVideo(t)

	// 第一轮 rt=0.9 返回 403，第二轮 rt=1 返回 200
	tr := &stubTransport{}
	seen := 0
	tr.handler = routes(t, route{match: "mooc-ans/multimedia/log/a", handler: func(req *http.Request, _ string) (*http.Response, error) {
		seen++
		if req.URL.Query().Get("rt") == "0.9" {
			return jsonResponse(t, http.StatusForbidden, map[string]any{"msg": "403"}), nil
		}
		return jsonResponse(t, http.StatusOK, map[string]any{"isPassed": true}), nil
	}}, captchaStub())

	c := newTestClient(t, tr, Options{})
	job := testJob()
	job.OtherInfo = "nodeId_5005-1" // 不含 -rt_ 标记
	job.Rt = ""

	passed, state := c.videoProgressLog(testCourse(), job, htmlx.JobInfo{}, "tok", 100, 10, "Video", 3, consts.VideoHeaders)
	if !passed || state != http.StatusOK {
		t.Fatalf("结果 = (%v, %d), 期望 (true, 200)", passed, state)
	}
	if seen != 2 {
		t.Fatalf("应依次尝试 rt=0.9 与 rt=1, 实际请求 %d 次", seen)
	}
	rts := []string{}
	for _, r := range tr.find("mooc-ans/multimedia/log/a") {
		rts = append(rts, r.query.Get("rt"))
	}
	if strings.Join(rts, ",") != "0.9,1" {
		t.Errorf("rt 尝试顺序 = %v, 期望 [0.9 1]", rts)
	}
}

func TestVideoProgressLog403ReturnsForbidden(t *testing.T) {
	fastVideo(t)

	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("mooc-ans/multimedia/log/a", jsonResponse(t, http.StatusForbidden,
		map[string]any{"msg": "403"})), captchaStub())

	c := newTestClient(t, tr, Options{})
	job := testJob()
	job.Rt = "1"

	passed, state := c.videoProgressLog(testCourse(), job, htmlx.JobInfo{}, "tok", 100, 10, "Video", 3, consts.VideoHeaders)
	if passed {
		t.Fatal("403 时 passed 应为 false")
	}
	if state != http.StatusForbidden {
		t.Fatalf("403 时应返回状态码 403, 实际 %d", state)
	}
}

func TestVideoProgressLogAudioHeaders(t *testing.T) {
	fastVideo(t)

	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("mooc-ans/multimedia/log/a", jsonResponse(t, http.StatusOK,
		map[string]any{"isPassed": true})))

	c := newTestClient(t, tr, Options{})
	job := testJob()
	job.Rt = "1"

	_, _ = c.videoProgressLog(testCourse(), job, htmlx.JobInfo{}, "tok", 100, 10, "Audio", 3, consts.AudioHeaders)
	req := tr.findFirst(t, "mooc-ans/multimedia/log/a")
	if got := req.header.Get("Referer"); got != consts.AudioHeaders["Referer"] {
		t.Errorf("音频任务 Referer = %q, 期望 %q", got, consts.AudioHeaders["Referer"])
	}
	if got := req.query.Get("dtype"); got != "Audio" {
		t.Errorf("dtype = %q, 期望 Audio", got)
	}
}

func TestStudyVideoInstantComplete(t *testing.T) {
	fastVideo(t)

	tr := &stubTransport{}
	tr.handler = routes(t,
		route{match: "ananas/status", handler: func(*http.Request, string) (*http.Response, error) {
			return videoStatusJSON(t, "dtoken-1", 600), nil
		}},
		staticRoute("mooc-ans/multimedia/log/a", jsonResponse(t, http.StatusOK, map[string]any{"isPassed": true})),
	)

	c := newTestClient(t, tr, Options{Speed: 1})
	got := c.StudyVideo(testCourse(), testJob(), htmlx.JobInfo{}, "Video")
	if got != ResultSuccess {
		t.Fatalf("瞬间完成应返回 success, 实际 %v", got)
	}

	statusReq := tr.findFirst(t, "ananas/status")
	wantURL := "https://mooc1.chaoxing.com/ananas/status/obj-1?k=1234&flag=normal"
	if statusReq.rawURL != wantURL {
		t.Errorf("状态 URL = %q, 期望 %q", statusReq.rawURL, wantURL)
	}
	if got := statusReq.header.Get("Referer"); got != consts.VideoHeaders["Referer"] {
		t.Errorf("状态请求 Referer = %q", got)
	}

	logReq := tr.findFirst(t, "mooc-ans/multimedia/log/a")
	if got := logReq.query.Get("isdrag"); got != "4" {
		t.Errorf("首次探测的 isdrag = %q, 期望 4", got)
	}
	if got := logReq.query.Get("playingTime"); got != "600" {
		t.Errorf("首次探测的 playingTime = %q, 期望 600", got)
	}
	if got := logReq.query.Get("duration"); got != "600" {
		t.Errorf("首次探测的 duration = %q, 期望 600", got)
	}
}

func TestStudyVideoUnknownStatus(t *testing.T) {
	fastVideo(t)

	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("ananas/status", jsonResponse(t, http.StatusOK,
		map[string]any{"status": "fail"})))

	c := newTestClient(t, tr, Options{})
	if got := c.StudyVideo(testCourse(), testJob(), htmlx.JobInfo{}, "Video"); got != ResultError {
		t.Fatalf("status 非 success 应返回 error, 实际 %v", got)
	}
}

func TestStudyVideo403Forbidden(t *testing.T) {
	fastVideo(t)

	tr := &stubTransport{}
	tr.handler = routes(t,
		route{match: "ananas/status", handler: func(*http.Request, string) (*http.Response, error) {
			// duration=0 使循环每次都能命中 playTime == duration，快速走到 403 分支
			return videoStatusJSON(t, "dtoken-1", 0), nil
		}},
		staticRoute("mooc-ans/multimedia/log/a", jsonResponse(t, http.StatusForbidden, map[string]any{"msg": "403"})),
		captchaStub(),
	)

	c := newTestClient(t, tr, Options{})
	job := testJob()
	job.Rt = "1" // 给定 rt 后每次上报只发一次请求

	got := c.StudyVideo(testCourse(), job, htmlx.JobInfo{}, "Video")
	if got != ResultForbidden {
		t.Fatalf("连续 403 应返回 forbidden, 实际 %v", got)
	}

	// 403 恢复会刷新状态，恢复成功则继续重试，直到超过上限
	if n := len(tr.find("ananas/status")); n < 2 {
		t.Errorf("403 恢复应刷新视频状态, 状态请求数 = %d", n)
	}
	if n := len(tr.find("mooc-ans/multimedia/log/a")); n != 4 {
		t.Errorf("上报次数 = %d, 期望 4 (1 次探测 + 3 次循环)\n请求:\n%s", n, strings.Join(tr.urls(), "\n"))
	}
	// 403 会触发验证码自动通关尝试（无 OCR 时快速失败）
	if n := len(tr.find("processVerifyPng.ac")); n == 0 {
		t.Errorf("403 应触发验证码自动通关尝试")
	}
}

func TestStudyVideoRecoverFailsReturnsError(t *testing.T) {
	fastVideo(t)

	tr := &stubTransport{}
	tr.handler = routes(t,
		route{match: "ananas/status", handler: func(req *http.Request, _ string) (*http.Response, error) {
			// 首次取状态成功；恢复时的刷新一律失败
			if strings.Contains(req.URL.String(), "k=1234") && len(tr.find("ananas/status")) == 1 {
				return videoStatusJSON(t, "dtoken-1", 0), nil
			}
			return jsonResponse(t, http.StatusInternalServerError, map[string]any{"status": "fail"}), nil
		}},
		staticRoute("mooc-ans/multimedia/log/a", jsonResponse(t, http.StatusForbidden, map[string]any{"msg": "403"})),
		staticRoute("visit/courselistdata", htmlResponse(http.StatusOK, "please login")),
		staticRoute("passport2.chaoxing.com/fanyalogin", jsonResponse(t, http.StatusOK, map[string]any{"status": false, "msg2": "登录失败"})),
		captchaStub(),
	)

	c := newTestClient(t, tr, Options{})
	job := testJob()
	job.Rt = "1"

	if got := c.StudyVideo(testCourse(), job, htmlx.JobInfo{}, "Video"); got != ResultError {
		t.Fatalf("会话恢复失败应返回 error, 实际 %v", got)
	}
}

func TestRefreshVideoStatus(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("ananas/status", videoStatusJSON(t, "tok-2", 300)))

	c := newTestClient(t, tr, Options{})
	data, err := c.refreshVideoStatus(testJob(), "Video")
	if err != nil {
		t.Fatalf("refreshVideoStatus 返回错误: %v", err)
	}
	if anyString(data["dtoken"]) != "tok-2" || jsonInt(data["duration"]) != 300 {
		t.Fatalf("刷新结果异常: %v", data)
	}

	tr2 := &stubTransport{}
	tr2.handler = routes(t, staticRoute("ananas/status", jsonResponse(t, http.StatusOK, map[string]any{"status": "fail"})))
	c2 := newTestClient(t, tr2, Options{})
	if _, err := c2.refreshVideoStatus(testJob(), "Video"); err == nil {
		t.Fatal("status 非 success 时应返回错误")
	}
}

// 断言音频任务的 status 请求使用 AudioHeaders
func TestStudyVideoAudioStatusHeaders(t *testing.T) {
	fastVideo(t)

	tr := &stubTransport{}
	tr.handler = routes(t,
		route{match: "ananas/status", handler: func(*http.Request, string) (*http.Response, error) {
			return videoStatusJSON(t, "t", 10), nil
		}},
		staticRoute("mooc-ans/multimedia/log/a", jsonResponse(t, http.StatusOK, map[string]any{"isPassed": true})),
	)

	c := newTestClient(t, tr, Options{})
	if got := c.StudyVideo(testCourse(), testJob(), htmlx.JobInfo{}, "Audio"); got != ResultSuccess {
		t.Fatalf("音频瞬间完成应返回 success, 实际 %v", got)
	}
	req := tr.findFirst(t, "ananas/status")
	if got := req.header.Get("Referer"); got != consts.AudioHeaders["Referer"] {
		t.Errorf("音频状态请求 Referer = %q, 期望 %q", got, consts.AudioHeaders["Referer"])
	}
	if !strings.Contains(req.rawURL, "k=1234") {
		t.Errorf("状态 URL 应带 fid 参数: %q", req.rawURL)
	}
}

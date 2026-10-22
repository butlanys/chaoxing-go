package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/butlanys/chaoxing-go/internal/htmlx"
	"github.com/butlanys/chaoxing-go/internal/logx"
)

// 本文件提供 internal/api 单元测试的公共脚手架：
// 用自定义 RoundTripper 拦截全部出网请求（真实生产 URL 保持不变），
// 既可断言请求构造（URL / 方法 / 参数 / 请求头 / 表单），也可脚本化响应。

// recordedRequest 一次被拦截请求的明细
type recordedRequest struct {
	method string
	rawURL string
	host   string
	header http.Header
	body   string
	query  url.Values
}

// stubTransport 拦截全部出网请求，记录明细并按 handler 返回响应
type stubTransport struct {
	mu      sync.Mutex
	reqs    []recordedRequest
	handler func(req *http.Request, body string) (*http.Response, error)
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	if req.Body != nil {
		if b, err := io.ReadAll(req.Body); err == nil {
			body = string(b)
		}
		_ = req.Body.Close()
	}

	s.mu.Lock()
	s.reqs = append(s.reqs, recordedRequest{
		method: req.Method,
		rawURL: req.URL.String(),
		host:   req.Host,
		header: req.Header.Clone(),
		body:   body,
		query:  req.URL.Query(),
	})
	s.mu.Unlock()

	if s.handler != nil {
		return s.handler(req, body)
	}
	return newResponse(http.StatusOK, "application/json", `{}`), nil
}

func (s *stubTransport) all() []recordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedRequest, len(s.reqs))
	copy(out, s.reqs)
	return out
}

func (s *stubTransport) count() int { return len(s.all()) }

// urls 返回全部请求的 URL（断言失败时便于排查）
func (s *stubTransport) urls() []string {
	out := make([]string, 0, s.count())
	for _, r := range s.all() {
		out = append(out, r.method+" "+r.rawURL)
	}
	return out
}

// find 返回所有 URL 中包含 sub 的请求
func (s *stubTransport) find(sub string) []recordedRequest {
	out := []recordedRequest{}
	for _, r := range s.all() {
		if strings.Contains(r.rawURL, sub) {
			out = append(out, r)
		}
	}
	return out
}

// findFirst 返回第一个 URL 中包含 sub 的请求，找不到直接失败
func (s *stubTransport) findFirst(t *testing.T, sub string) recordedRequest {
	t.Helper()
	got := s.find(sub)
	if len(got) == 0 {
		t.Fatalf("未捕获到包含 %q 的请求\n实际请求:\n%s", sub, strings.Join(s.urls(), "\n"))
	}
	return got[0]
}

// newResponse 构造一个内存响应
func newResponse(status int, contentType, body string) *http.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}
}

// jsonResponse 返回 JSON 响应
func jsonResponse(t *testing.T, status int, v any) *http.Response {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("构造 JSON 响应失败: %v", err)
	}
	return newResponse(status, "application/json; charset=utf-8", string(b))
}

// htmlResponse 返回 HTML 响应
func htmlResponse(status int, body string) *http.Response {
	return newResponse(status, "text/html; charset=utf-8", body)
}

// route 一条 URL 子串匹配的路由
type route struct {
	match   string
	handler func(req *http.Request, body string) (*http.Response, error)
}

// routes 把若干路由组合成 stubTransport 的 handler；未匹配到的请求记为测试失败
func routes(t *testing.T, rs ...route) func(*http.Request, string) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request, body string) (*http.Response, error) {
		for _, r := range rs {
			if strings.Contains(req.URL.String(), r.match) {
				return r.handler(req, body)
			}
		}
		t.Errorf("未匹配到路由: %s", req.URL.String())
		return newResponse(http.StatusNotFound, "text/plain", "no route"), nil
	}
}

// staticRoute 始终返回同一内容的响应（每次请求重建 body，避免同一 Body 被重复读取）
func staticRoute(match string, resp *http.Response) route {
	status := resp.StatusCode
	contentType := resp.Header.Get("Content-Type")
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return route{match: match, handler: func(*http.Request, string) (*http.Response, error) {
		return newResponse(status, contentType, string(body)), nil
	}}
}

// captchaStub 返回非 PNG 内容，使验证码自动识别立即失败（403 场景下快速短路，避免真实 OCR 依赖）
func captchaStub() route {
	return route{match: "processVerifyPng.ac", handler: func(*http.Request, string) (*http.Response, error) {
		return newResponse(http.StatusOK, "text/html", "<html>not an image</html>"), nil
	}}
}

// newTestClient 构造一个把出网请求交给 stubTransport 的测试客户端，并预置登录态 cookie。
func newTestClient(t *testing.T, tr *stubTransport, opt Options) *Client {
	t.Helper()
	if opt.Timeout <= 0 {
		opt.Timeout = 3 * time.Second
	}
	c := NewClient(Account{Username: "13800138000", Password: "secret"}, nil, logx.New("测试"), opt)
	c.http.Transport = tr
	// 去掉限速，避免测试被无谓的等待拖慢
	c.rl = NewRateLimiter(0)
	c.videoRL = NewRateLimiter(0)
	// 预置登录态 cookie
	if err := c.LoadCookies("_uid=12345678; fid=1234; UID=12345678; vc3=abc"); err != nil {
		t.Fatalf("导入测试 cookie 失败: %v", err)
	}
	return c
}

// fastVideo 关闭视频进度上报、403 退避与验证码重试的等待（仅测试使用，退出时还原）
func fastVideo(t *testing.T) {
	t.Helper()
	oldLimit, oldBackoff, oldCaptcha := videoLogLimit, forbiddenRetryBackoff, captchaRetryInterval
	videoLogLimit = func(*Client) {}
	forbiddenRetryBackoff = func() time.Duration { return 0 }
	captchaRetryInterval = 0
	t.Cleanup(func() {
		videoLogLimit = oldLimit
		forbiddenRetryBackoff = oldBackoff
		captchaRetryInterval = oldCaptcha
	})
}

// fastChapter 关闭章节停留等待（仅测试使用，退出时还原）
func fastChapter(t *testing.T) {
	t.Helper()
	old := chapterDwellTime
	chapterDwellTime = 0
	t.Cleanup(func() { chapterDwellTime = old })
}

// testCourse 构造一个课程
func testCourse() htmlx.Course {
	return htmlx.Course{
		CourseID: "1001",
		ClazzID:  "2002",
		CPI:      "3003",
		Title:    "测试课程",
	}
}

// testPoint 构造一个章节
func testPoint() htmlx.Point {
	return htmlx.Point{ID: "4004", Title: "第一章", JobCount: "1"}
}

// testJob 构造一个视频任务点
func testJob() htmlx.Job {
	return htmlx.Job{
		Type:      "video",
		JobID:     "job-1",
		Name:      "视频任务",
		OtherInfo: "nodeId_5005-1",
		ObjectID:  "obj-1",
		Mid:       "mid-1",
		PlayTime:  0,
	}
}

// resetCookies 清空会话 cookie（测试用）
func resetCookies(c *Client) {
	if fj, ok := c.jar.(*flatJar); ok {
		fj.mu.Lock()
		fj.data = map[string]string{}
		fj.mu.Unlock()
	}
}

// cookieMap 返回会话 cookie 快照（测试用）
func cookieMap(c *Client) map[string]string {
	if fj, ok := c.jar.(*flatJar); ok {
		return fj.all()
	}
	return nil
}

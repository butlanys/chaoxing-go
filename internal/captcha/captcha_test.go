package captcha

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/butlanys/chaoxing-go/internal/logx"
)

// captureLogger 记录日志，供断言使用
type captureLogger struct {
	infos  []string
	warns  []string
	errs   []string
	debugs []string
}

func (c *captureLogger) Info(format string, a ...any) {
	c.infos = append(c.infos, fmt.Sprintf(format, a...))
}
func (c *captureLogger) Warn(format string, a ...any) {
	c.warns = append(c.warns, fmt.Sprintf(format, a...))
}
func (c *captureLogger) Error(format string, a ...any) {
	c.errs = append(c.errs, fmt.Sprintf(format, a...))
}
func (c *captureLogger) Debug(format string, a ...any) {
	c.debugs = append(c.debugs, fmt.Sprintf(format, a...))
}

// fakeSolver 固定返回的识别器
type fakeSolver struct {
	ans string
	err error
}

func (f fakeSolver) Solve([]byte) (string, error) { return f.ans, f.err }

// pngBytes 生成一张 1x1 的 PNG
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// cxServer 模拟超星的取图/提交两个端点
type cxServer struct {
	imgStatus    int
	imgCT        string
	submitStatus int
	submitLoc    string

	gotGetQuery string
	gotSubmit   string
	gotUA       string
	gotCookie   string
	gotAccept   string
	submitHits  int
}

func newCxServer(t *testing.T, cfg cxServer) (*httptest.Server, *cxServer) {
	t.Helper()
	state := &cfg
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case cxGetPath:
			state.gotGetQuery = r.URL.RawQuery
			state.gotUA = r.Header.Get("User-Agent")
			state.gotCookie = r.Header.Get("Cookie")
			state.gotAccept = r.Header.Get("Accept")
			ct := state.imgCT
			if ct == "" {
				ct = "image/png"
			}
			w.Header().Set("Content-Type", ct)
			status := state.imgStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			if strings.HasPrefix(ct, "image/png") && status == http.StatusOK {
				_, _ = w.Write(pngBytes(t))
			} else {
				_, _ = w.Write([]byte("<html>403</html>"))
			}
		case cxSubmitPath:
			state.submitHits++
			state.gotSubmit = r.URL.RawQuery
			if state.submitStatus == http.StatusFound {
				w.Header().Set("Location", state.submitLoc)
			}
			status := state.submitStatus
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
		default:
			// 重定向目标：若客户端跟随重定向会走到这里
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, state
}

// newTestCaptcha 构造指向测试服务器的 CxCaptcha
func newTestCaptcha(t *testing.T, srv *httptest.Server, solver Solver, log logger) *CxCaptcha {
	t.Helper()
	c := NewCxCaptcha(srv.Client(), "test-ua", "UID=abc; _uid=1", solver, log)
	c.host = srv.URL
	return c
}

// TestNewWithoutOcrURL 未配置 ocr_url 时返回 DisabledSolver
func TestNewWithoutOcrURL(t *testing.T) {
	log := &captureLogger{}
	s := New(map[string]string{"provider": "TikuYanxi"}, log)
	if _, ok := s.(DisabledSolver); !ok {
		t.Fatalf("期望 DisabledSolver, 实际 %T", s)
	}
	if _, err := s.Solve([]byte("img")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Solve 应返回 ErrUnavailable, 实际 %v", err)
	}
	if len(log.warns) == 0 {
		t.Error("应打 Warn 日志")
	}
}

// TestNewWithOcrURL 配置 ocr_url 后使用 HTTP OCR 服务
func TestNewWithOcrURL(t *testing.T) {
	var gotCT, gotField string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		f, _, err := r.FormFile("image")
		if err != nil {
			t.Errorf("缺少 image 字段: %v", err)
		} else {
			defer f.Close()
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(f)
			gotBody = buf.Bytes()
			gotField = "image"
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("abcd\n"))
	}))
	defer srv.Close()

	log := &captureLogger{}
	s := New(map[string]string{"ocr_url": srv.URL}, log)
	ocr, ok := s.(*HTTPOcrSolver)
	if !ok {
		t.Fatalf("期望 *HTTPOcrSolver, 实际 %T", s)
	}
	ans, err := ocr.Solve([]byte("PNGDATA"))
	if err != nil {
		t.Fatalf("Solve 失败: %v", err)
	}
	if ans != "abcd" {
		t.Errorf("answer = %q, 期望 abcd", ans)
	}
	if gotField != "image" {
		t.Errorf("未收到 image 字段, Content-Type = %s", gotCT)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data") {
		t.Errorf("Content-Type = %s, 期望 multipart/form-data", gotCT)
	}
	if string(gotBody) != "PNGDATA" {
		t.Errorf("图片内容 = %q", gotBody)
	}
}

// TestHTTPOcrSolverJSON 兼容 JSON 返回
func TestHTTPOcrSolverJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"result":"WXYZ"}`))
	}))
	defer srv.Close()

	log := &captureLogger{}
	ocr := &HTTPOcrSolver{url: srv.URL, client: srv.Client(), log: log}
	ans, err := ocr.Solve([]byte("img"))
	if err != nil {
		t.Fatalf("Solve 失败: %v", err)
	}
	if ans != "WXYZ" {
		t.Errorf("answer = %q", ans)
	}
}

// TestHTTPOcrSolverErrors 空图片 / 非 2xx / 空返回
func TestHTTPOcrSolverErrors(t *testing.T) {
	log := &captureLogger{}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer empty.Close()

	s := &HTTPOcrSolver{url: bad.URL, client: bad.Client(), log: log}
	if _, err := s.Solve([]byte("img")); err == nil {
		t.Error("非 2xx 应返回 error")
	}
	s2 := &HTTPOcrSolver{url: empty.URL, client: empty.Client(), log: log}
	if _, err := s2.Solve([]byte("img")); err == nil {
		t.Error("空返回应返回 error")
	}
	if _, err := s2.Solve(nil); err == nil {
		t.Error("空图片应返回 error")
	}
}

// TestGetCaptcha 取图成功，且 t 参数与请求头正确
func TestGetCaptcha(t *testing.T) {
	srv, state := newCxServer(t, cxServer{})
	log := &captureLogger{}
	c := newTestCaptcha(t, srv, fakeSolver{ans: "abcd"}, log)

	img, err := c.GetCaptcha()
	if err != nil {
		t.Fatalf("GetCaptcha 失败: %v", err)
	}
	if len(img) == 0 {
		t.Fatal("图片为空")
	}
	if !strings.HasPrefix(state.gotGetQuery, "t=") {
		t.Errorf("query = %q, 期望 t=<随机数>", state.gotGetQuery)
	}
	if state.gotUA != "test-ua" {
		t.Errorf("User-Agent = %q", state.gotUA)
	}
	if state.gotCookie != "UID=abc; _uid=1" {
		t.Errorf("Cookie = %q", state.gotCookie)
	}
	if state.gotAccept != cxAccept {
		t.Errorf("Accept = %q", state.gotAccept)
	}
}

// TestGetCaptchaContentTypeMismatch Content-Type 不匹配时返回 error
func TestGetCaptchaContentTypeMismatch(t *testing.T) {
	srv, _ := newCxServer(t, cxServer{imgCT: "text/html; charset=utf-8"})
	log := &captureLogger{}
	c := newTestCaptcha(t, srv, fakeSolver{ans: "abcd"}, log)

	if _, err := c.GetCaptcha(); err == nil {
		t.Fatal("Content-Type 不匹配应返回 error")
	} else if !strings.Contains(err.Error(), "image/png") {
		t.Errorf("错误信息应提示 image/png: %v", err)
	}
}

// TestGetCaptchaNon200 非 200 返回 error
func TestGetCaptchaNon200(t *testing.T) {
	srv, _ := newCxServer(t, cxServer{imgStatus: http.StatusForbidden, imgCT: "image/png"})
	log := &captureLogger{}
	c := newTestCaptcha(t, srv, fakeSolver{ans: "abcd"}, log)
	if _, err := c.GetCaptcha(); err == nil {
		t.Fatal("403 应返回 error")
	}
}

// TestSubmitCaptcha302 302 视为成功，且不跟随重定向
func TestSubmitCaptcha302(t *testing.T) {
	srv, state := newCxServer(t, cxServer{submitStatus: http.StatusFound, submitLoc: "/html/processVerify.ac?done=1"})
	log := &captureLogger{}
	c := newTestCaptcha(t, srv, fakeSolver{ans: "abcd"}, log)

	ok, err := c.SubmitCaptcha("abcd")
	if err != nil {
		t.Fatalf("SubmitCaptcha 失败: %v", err)
	}
	if !ok {
		t.Fatal("302 应返回 true")
	}
	if state.gotSubmit != "app=0&ucode=abcd" {
		t.Errorf("query = %q, 期望 app=0&ucode=abcd", state.gotSubmit)
	}
}

// TestSubmitCaptcha200 200 视为失败
func TestSubmitCaptcha200(t *testing.T) {
	srv, _ := newCxServer(t, cxServer{submitStatus: http.StatusOK})
	log := &captureLogger{}
	c := newTestCaptcha(t, srv, fakeSolver{ans: "abcd"}, log)
	ok, err := c.SubmitCaptcha("abcd")
	if err != nil {
		t.Fatalf("SubmitCaptcha 失败: %v", err)
	}
	if ok {
		t.Fatal("200 应返回 false")
	}
}

// TestTryPassSuccess 取图 -> 识别 -> 提交 全流程成功
func TestTryPassSuccess(t *testing.T) {
	srv, state := newCxServer(t, cxServer{submitStatus: http.StatusFound})
	log := &captureLogger{}
	c := newTestCaptcha(t, srv, fakeSolver{ans: "  good  "}, log)

	if !c.TryPass() {
		t.Fatal("TryPass 应为 true")
	}
	if state.gotSubmit != "app=0&ucode=good" {
		t.Errorf("提交内容 = %q", state.gotSubmit)
	}
	if len(log.errs) != 0 {
		t.Errorf("不应有错误日志: %v", log.errs)
	}
}

// TestTryPassFailures TryPass 的各失败分支都返回 false 且不 panic
func TestTryPassFailures(t *testing.T) {
	t.Run("取图失败(Content-Type 不匹配)", func(t *testing.T) {
		srv, _ := newCxServer(t, cxServer{imgCT: "text/html"})
		log := &captureLogger{}
		c := newTestCaptcha(t, srv, fakeSolver{ans: "abcd"}, log)
		if c.TryPass() {
			t.Fatal("应为 false")
		}
		if len(log.errs) == 0 {
			t.Error("应有错误日志")
		}
	})

	t.Run("识别失败", func(t *testing.T) {
		srv, _ := newCxServer(t, cxServer{submitStatus: http.StatusFound})
		log := &captureLogger{}
		c := newTestCaptcha(t, srv, fakeSolver{err: errors.New("OCR 挂了")}, log)
		if c.TryPass() {
			t.Fatal("应为 false")
		}
	})

	t.Run("识别结果为空", func(t *testing.T) {
		srv, _ := newCxServer(t, cxServer{submitStatus: http.StatusFound})
		log := &captureLogger{}
		c := newTestCaptcha(t, srv, fakeSolver{ans: "   "}, log)
		if c.TryPass() {
			t.Fatal("应为 false")
		}
	})

	t.Run("提交未通过(200)", func(t *testing.T) {
		srv, _ := newCxServer(t, cxServer{submitStatus: http.StatusOK})
		log := &captureLogger{}
		c := newTestCaptcha(t, srv, fakeSolver{ans: "abcd"}, log)
		if c.TryPass() {
			t.Fatal("应为 false")
		}
		if len(log.warns) == 0 {
			t.Error("提交未通过时应打 Warn 日志")
		}
	})

	t.Run("DisabledSolver", func(t *testing.T) {
		srv, _ := newCxServer(t, cxServer{submitStatus: http.StatusFound})
		log := &captureLogger{}
		c := newTestCaptcha(t, srv, DisabledSolver{}, log)
		if c.TryPass() {
			t.Fatal("应为 false")
		}
	})

	t.Run("nil solver 不 panic", func(t *testing.T) {
		srv, _ := newCxServer(t, cxServer{submitStatus: http.StatusFound})
		log := &captureLogger{}
		c := newTestCaptcha(t, srv, nil, log)
		if c.TryPass() {
			t.Fatal("应为 false")
		}
	})
}

// TestRecognitionEmptySolver Recognition 在无 solver 时返回 ErrUnavailable
func TestRecognitionEmptySolver(t *testing.T) {
	log := &captureLogger{}
	c := NewCxCaptcha(nil, "", "", nil, log)
	if _, err := c.Recognition([]byte("img")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("应返回 ErrUnavailable, 实际 %v", err)
	}
}

// TestNewCxCaptchaNilClient 默认客户端超时 10s
func TestNewCxCaptchaNilClient(t *testing.T) {
	c := NewCxCaptcha(nil, "ua", "c", DisabledSolver{}, &captureLogger{})
	if c.client == nil || c.client.Timeout == 0 {
		t.Fatal("默认客户端应带超时")
	}
	if c.host != cxHost {
		t.Errorf("host = %q, 期望 %q", c.host, cxHost)
	}
}

// 编译期断言：*logx.Logger 满足本包 logger 接口（CONTRACT 要求可直接传入）
var _ logger = (*logx.Logger)(nil)

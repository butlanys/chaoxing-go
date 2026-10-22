package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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

// recorded 一次被记录到的请求
type recorded struct {
	method string
	path   string
	query  url.Values
	form   url.Values
	body   string
	ctype  string
}

func newRecorder(t *testing.T, status int, body string) (*httptest.Server, *[]recorded) {
	t.Helper()
	got := &[]recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		*got = append(*got, recorded{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query(),
			form:   r.PostForm,
			body:   string(raw),
			ctype:  r.Header.Get("Content-Type"),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

// TestNoopWhenUnconfigured 空 provider / 空 url / 未知 provider 都返回 noop
func TestNoopWhenUnconfigured(t *testing.T) {
	cases := []struct {
		name string
		conf map[string]string
	}{
		{"空配置", map[string]string{}},
		{"provider 为空", map[string]string{"provider": "", "url": "https://example.com"}},
		{"url 为空", map[string]string{"provider": "ServerChan", "url": ""}},
		{"url 只有空白", map[string]string{"provider": "ServerChan", "url": "   "}},
		{"未知 provider", map[string]string{"provider": "NotExist", "url": "https://example.com"}},
		{"Telegram 缺 tg_chat_id", map[string]string{"provider": "Telegram", "url": "https://api.telegram.org/bot1:a/sendMessage"}},
		{"nil 配置", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &captureLogger{}
			n := New(tc.conf, log)
			if n.Name() != "Noop" {
				t.Fatalf("Name() = %q, 期望 Noop", n.Name())
			}
			if err := n.Send("测试消息"); err != nil {
				t.Fatalf("noop Send 应返回 nil, 实际 %v", err)
			}
			if len(log.debugs) == 0 {
				t.Error("noop 发送时应打 Debug 日志")
			}
		})
	}
}

// TestServerChan 断言 URL/方法/参数
func TestServerChan(t *testing.T) {
	srv, got := newRecorder(t, http.StatusOK, `{"code":0,"message":"ok"}`)
	log := &captureLogger{}
	n := New(map[string]string{"provider": "ServerChan", "url": srv.URL + "/SCT123.send"}, log)
	if n.Name() != "ServerChan" {
		t.Fatalf("Name() = %q", n.Name())
	}
	if err := n.Send("学习完成"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("请求数 = %d, 期望 1", len(*got))
	}
	r := (*got)[0]
	if r.method != http.MethodPost {
		t.Errorf("method = %s, 期望 POST", r.method)
	}
	if r.path != "/SCT123.send" {
		t.Errorf("path = %s", r.path)
	}
	if r.ctype != "application/json;charset=utf-8" {
		t.Errorf("Content-Type = %q, 期望 application/json;charset=utf-8", r.ctype)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(r.body), &payload); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v (%s)", err, r.body)
	}
	if payload["text"] != "学习完成" || payload["desp"] != "学习完成" {
		t.Errorf("JSON body = %v, 期望 text/desp 均为消息内容", payload)
	}

	// 5s 超时
	if sc, ok := n.(*serverChanNotifier); !ok || sc.client.Timeout != 5*time.Second {
		t.Errorf("HTTP 客户端超时应为 5s")
	}
}

// TestQmsg 断言 GET 与 msg 参数
func TestQmsg(t *testing.T) {
	srv, got := newRecorder(t, http.StatusOK, `{"success":true}`)
	log := &captureLogger{}
	n := New(map[string]string{"provider": "Qmsg", "url": srv.URL + "/send/KEY"}, log)
	if n.Name() != "Qmsg" {
		t.Fatalf("Name() = %q", n.Name())
	}
	if err := n.Send("消息内容"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	r := (*got)[0]
	if r.method != http.MethodPost {
		t.Errorf("method = %s, 期望 POST", r.method)
	}
	if r.path != "/send/KEY" {
		t.Errorf("path = %s", r.path)
	}
	if r.query.Get("msg") != "消息内容" {
		t.Errorf("query = %v, 期望 msg=消息内容", r.query)
	}
}

// TestBark 断言 POST <url>?body=<text>
func TestBark(t *testing.T) {
	srv, got := newRecorder(t, http.StatusOK, `{"code":200}`)
	log := &captureLogger{}
	n := New(map[string]string{"provider": "Bark", "url": srv.URL + "/barkkey"}, log)
	if n.Name() != "Bark" {
		t.Fatalf("Name() = %q", n.Name())
	}
	if err := n.Send("任务完成"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	r := (*got)[0]
	if r.method != http.MethodPost {
		t.Errorf("method = %s, 期望 POST", r.method)
	}
	if r.path != "/barkkey" {
		t.Errorf("path = %s, 期望 /barkkey", r.path)
	}
	if r.query.Get("body") != "任务完成" {
		t.Errorf("query = %v, 期望 body=任务完成", r.query)
	}
}

// TestTelegram 断言 POST 与 chat_id/text 参数、token 提取
func TestTelegram(t *testing.T) {
	srv, got := newRecorder(t, http.StatusOK, `{"ok":true,"result":{}}`)
	log := &captureLogger{}
	raw := srv.URL + "/bot123456:AAHhJx/sendMessage"
	n := New(map[string]string{"provider": "Telegram", "url": raw, "tg_chat_id": "999888"}, log)
	if n.Name() != "Telegram" {
		t.Fatalf("Name() = %q", n.Name())
	}
	if err := n.Send("hello"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	r := (*got)[0]
	if r.method != http.MethodPost {
		t.Errorf("method = %s, 期望 POST", r.method)
	}
	if r.path != "/bot123456:AAHhJx/sendMessage" {
		t.Errorf("path = %s", r.path)
	}
	if r.form.Get("chat_id") != "999888" || r.form.Get("text") != "hello" || r.form.Get("parse_mode") != "HTML" {
		t.Errorf("form = %v, 期望 chat_id/text/parse_mode=HTML", r.form)
	}

	// token 提取
	if tok := extractBotToken(raw); tok != "123456:AAHhJx" {
		t.Errorf("extractBotToken = %q", tok)
	}
	// url 中缺少 sendMessage 段时用 token 补全
	if target := telegramTarget("https://api.telegram.org/bot123456:AAHhJx/", "123456:AAHhJx"); target != "https://api.telegram.org/bot123456:AAHhJx/sendMessage" {
		t.Errorf("telegramTarget = %q", target)
	}
}

// TestTelegramNotOK Telegram 返回 ok=false 时应返回 error
func TestTelegramNotOK(t *testing.T) {
	srv, _ := newRecorder(t, http.StatusOK, `{"ok":false,"description":"chat not found"}`)
	log := &captureLogger{}
	n := New(map[string]string{"provider": "Telegram", "url": srv.URL + "/bot1:a/sendMessage", "tg_chat_id": "1"}, log)
	if err := n.Send("hello"); err == nil {
		t.Fatal("ok=false 时应返回 error")
	}
	if len(log.errs) == 0 {
		t.Error("失败时应打 Error 日志")
	}
}

// TestNon2xxReturnsError 非 2xx 返回 error 且不 panic
func TestNon2xxReturnsError(t *testing.T) {
	for _, provider := range []string{"ServerChan", "Qmsg", "Bark", "Telegram"} {
		t.Run(provider, func(t *testing.T) {
			srv, _ := newRecorder(t, http.StatusInternalServerError, `boom`)
			log := &captureLogger{}
			conf := map[string]string{"provider": provider, "url": srv.URL + "/hook"}
			if provider == "Telegram" {
				conf["url"] = srv.URL + "/bot1:a/sendMessage"
				conf["tg_chat_id"] = "1"
			}
			n := New(conf, log)
			if err := n.Send("x"); err == nil {
				t.Fatalf("%s 非 2xx 应返回 error", provider)
			}
			if len(log.errs) == 0 {
				t.Errorf("%s 失败时应打 Error 日志", provider)
			}
		})
	}
}

// TestNetworkErrorReturnsError 网络不可达时返回 error
func TestNetworkErrorReturnsError(t *testing.T) {
	log := &captureLogger{}
	// 指向一个已关闭的端口
	srv, _ := newRecorder(t, http.StatusOK, `{}`)
	dead := srv.URL + "/x"
	srv.Close()
	n := New(map[string]string{"provider": "ServerChan", "url": dead}, log)
	if err := n.Send("x"); err == nil {
		t.Fatal("网络错误应返回 error")
	}
}

// TestGetCaseInsensitive 配置键大小写不敏感
func TestGetCaseInsensitive(t *testing.T) {
	if got := get(map[string]string{"Provider": "Bark"}, "provider"); got != "Bark" {
		t.Errorf("get = %q", got)
	}
	if got := get(map[string]string{"TG_CHAT_ID": "123"}, "tg_chat_id"); got != "123" {
		t.Errorf("get = %q", got)
	}
}

// 编译期断言：*logx.Logger 满足本包 logger 接口（CONTRACT 要求可直接传入）
var _ logger = (*logx.Logger)(nil)

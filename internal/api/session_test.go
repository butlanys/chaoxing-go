package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/butlanys/chaoxing-go/internal/crypto"
)

// 期望的表单字段与 base.py login 的 _data 逐字一致。
func TestLoginWithPassword(t *testing.T) {
	tr := &stubTransport{}
	cookieFile := filepath.Join(t.TempDir(), "cookies_13800138000.txt")

	tr.handler = routes(t,
		route{match: "passport2.chaoxing.com/fanyalogin", handler: func(*http.Request, string) (*http.Response, error) {
			resp := jsonResponse(t, http.StatusOK, map[string]any{"status": true})
			resp.Header.Add("Set-Cookie", "_uid=88888888; Path=/")
			resp.Header.Add("Set-Cookie", "fid=4321; Path=/")
			return resp, nil
		}},
		route{match: "passport2.chaoxing.com/mooc/accountManage", handler: func(*http.Request, string) (*http.Response, error) {
			return htmlResponse(http.StatusOK, `<input id="messageName" value="张三">`), nil
		}},
	)

	c := newTestClient(t, tr, Options{CookieFile: cookieFile})
	c.account = Account{Username: "13800138000", Password: "secret-pass"}

	res, err := c.Login(false)
	if err != nil {
		t.Fatalf("Login(false) 返回错误: %v", err)
	}
	if !res.Status {
		t.Fatalf("Login(false) 未成功: %+v", res)
	}
	if res.Msg != "登录成功" {
		t.Errorf("Msg = %q, 期望 登录成功", res.Msg)
	}
	if res.RealName != "张三" {
		t.Errorf("RealName = %q, 期望 张三", res.RealName)
	}

	req := tr.findFirst(t, "passport2.chaoxing.com/fanyalogin")
	if req.method != http.MethodPost {
		t.Errorf("登录方法 = %s, 期望 POST", req.method)
	}
	if ct := req.header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", ct)
	}
	if ua := req.header.Get("User-Agent"); !strings.Contains(ua, "Chrome/118") {
		t.Errorf("未带上通用 User-Agent, 实际 %q", ua)
	}

	form, err := url.ParseQuery(req.body)
	if err != nil {
		t.Fatalf("解析登录表单失败: %v", err)
	}
	wantFields := map[string]string{
		"fid":      "-1",
		"uname":    crypto.Encrypt("13800138000"),
		"password": crypto.Encrypt("secret-pass"),
		"refer":    "https%3A%2F%2Fi.chaoxing.com",
		// base.py 里是 Python 布尔 True, requests 用 str() 序列化 -> 线上取值为 "True"
		"t":                 "True",
		"forbidotherlogin":  "0",
		"validate":          "",
		"doubleFactorLogin": "0",
		"independentId":     "0",
	}
	for k, want := range wantFields {
		if got := form.Get(k); got != want {
			t.Errorf("登录表单 %s = %q, 期望 %q", k, got, want)
		}
	}
	if len(form) != len(wantFields) {
		t.Errorf("登录表单字段数 = %d, 期望 %d (%v)", len(form), len(wantFields), form)
	}
	// AES 密文必须原样出现在请求体里（不做二次编码）
	if !strings.Contains(req.body, "uname="+url.QueryEscape(crypto.Encrypt("13800138000"))) {
		t.Errorf("请求体缺少加密后的 uname: %s", req.body)
	}
	if !strings.Contains(req.body, "refer=https%253A%252F%252Fi.chaoxing.com") {
		t.Errorf("refer 字段编码与原版不一致: %s", req.body)
	}

	// 登录成功后应把响应 cookie 落盘
	data, err := os.ReadFile(cookieFile)
	if err != nil {
		t.Fatalf("cookie 文件未写入: %v", err)
	}
	content := string(data)
	if content == "" {
		t.Fatal("cookie 文件内容为空")
	}
	if strings.Contains(content, " ") || strings.HasSuffix(content, ";") {
		t.Errorf("cookie 落盘格式不符合 k=v;k=v: %q", content)
	}
	if !strings.Contains(content, "_uid=88888888") || !strings.Contains(content, "fid=4321") {
		t.Errorf("cookie 落盘内容缺少登录响应 cookie: %q", content)
	}
	info, err := os.Stat(cookieFile)
	if err != nil {
		t.Fatalf("stat cookie 文件失败: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cookie 文件权限 = %o, 期望 600", perm)
	}
}

func TestLoginWithPasswordFailure(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("fanyalogin", jsonResponse(t, http.StatusOK, map[string]any{
		"status": false,
		"msg2":   "用户名或密码错误",
	})))

	c := newTestClient(t, tr, Options{})
	c.account = Account{Username: "13800138000", Password: "bad"}

	res, err := c.Login(false)
	if err != nil {
		t.Fatalf("Login 返回错误: %v", err)
	}
	if res.Status {
		t.Fatal("登录失败时 Status 应为 false")
	}
	if res.Msg != "用户名或密码错误" {
		t.Errorf("Msg = %q, 期望 用户名或密码错误", res.Msg)
	}
}

func TestLoginWithCookiesValid(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("visit/courselistdata", htmlResponse(http.StatusOK, `<div class="course">正常课程页</div>`)),
		staticRoute("mooc/accountManage", htmlResponse(http.StatusOK, `<input id="messageName" value="李四">`)),
	)

	c := newTestClient(t, tr, Options{})
	res, err := c.Login(true)
	if err != nil {
		t.Fatalf("Login(true) 返回错误: %v", err)
	}
	if !res.Status || res.RealName != "李四" {
		t.Fatalf("cookie 登录结果异常: %+v", res)
	}

	req := tr.findFirst(t, "visit/courselistdata")
	if req.method != http.MethodPost {
		t.Errorf("校验会话方法 = %s, 期望 POST", req.method)
	}
	form, err := url.ParseQuery(req.body)
	if err != nil {
		t.Fatalf("解析表单失败: %v", err)
	}
	want := map[string]string{"courseType": "1", "courseFolderId": "0", "query": "", "superstarClass": "0"}
	for k, v := range want {
		if got := form.Get(k); got != v {
			t.Errorf("courselistdata %s = %q, 期望 %q", k, got, v)
		}
	}
}

func TestLoginWithCookiesInvalidNoPassword(t *testing.T) {
	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{})
	resetCookies(c)
	c.account = Account{Username: "", Password: ""}

	res, err := c.Login(true)
	if err != nil {
		t.Fatalf("Login(true) 返回错误: %v", err)
	}
	if res.Status {
		t.Fatal("无 _uid 时应视为失效")
	}
	if res.Msg != "cookies 已失效，请更新 cookies 或提供账号密码" {
		t.Errorf("Msg = %q", res.Msg)
	}
	if tr.count() != 0 {
		t.Errorf("未提供账号密码时不应发起任何请求, 实际 %d 次", tr.count())
	}
}

func TestLoginWithCookiesFallbackToPassword(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t,
		staticRoute("passport2.chaoxing.com/fanyalogin", jsonResponse(t, http.StatusOK, map[string]any{"status": true})),
		staticRoute("mooc/accountManage", htmlResponse(http.StatusOK, `<input id="messageName" value="王五">`)),
	)

	cookieFile := filepath.Join(t.TempDir(), "cookies.txt")
	if err := os.WriteFile(cookieFile, []byte("vc3=stale-cookie"), 0o600); err != nil {
		t.Fatalf("写入过期 cookie 文件失败: %v", err)
	}

	c := newTestClient(t, tr, Options{CookieFile: cookieFile})
	resetCookies(c)
	c.account = Account{Username: "13800138000", Password: "secret"}

	res, err := c.Login(true)
	if err != nil {
		t.Fatalf("Login(true) 返回错误: %v", err)
	}
	if !res.Status {
		t.Fatalf("应回退到密码登录成功: %+v", res)
	}
	if len(tr.find("fanyalogin")) != 1 {
		t.Errorf("应发起一次密码登录, 实际请求: %v", tr.urls())
	}
	if res.RealName != "王五" {
		t.Errorf("RealName = %q, 期望 王五", res.RealName)
	}
}

func TestValidateCookieSessionBranches(t *testing.T) {
	cases := []struct {
		name     string
		cookies  string
		status   int
		body     string
		want     bool
		wantReqs int
	}{
		{"无 _uid 直接失败", "fid=1234", http.StatusOK, "任意内容", false, 0},
		{"响应含 passport2", "_uid=1", http.StatusOK, "跳转 https://passport2.chaoxing.com/login", false, 1},
		{"响应含 login(大小写不敏感)", "_uid=1", http.StatusOK, "<html>Please LOGIN first</html>", false, 1},
		{"状态码非 200", "_uid=1", http.StatusInternalServerError, "ok", false, 1},
		{"正常页面", "_uid=1", http.StatusOK, "<div>课程列表</div>", true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := &stubTransport{}
			tr.handler = routes(t, staticRoute("visit/courselistdata", htmlResponse(tc.status, tc.body)))

			c := newTestClient(t, tr, Options{})
			resetCookies(c)
			if err := c.LoadCookies(tc.cookies); err != nil {
				t.Fatalf("导入 cookie 失败: %v", err)
			}

			if got := c.ValidateCookieSession(); got != tc.want {
				t.Errorf("ValidateCookieSession() = %v, 期望 %v", got, tc.want)
			}
			if tr.count() != tc.wantReqs {
				t.Errorf("请求次数 = %d, 期望 %d (%v)", tr.count(), tc.wantReqs, tr.urls())
			}
		})
	}
}

func TestUIDAndFID(t *testing.T) {
	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{})

	if got := c.UID(); got != "12345678" {
		t.Errorf("UID() = %q, 期望 12345678", got)
	}
	if got := c.FID(); got != "1234" {
		t.Errorf("FID() = %q, 期望 1234", got)
	}

	// 只有 UID cookie 时也能取到
	resetCookies(c)
	c.uid = ""
	c.fid = ""
	if err := c.LoadCookies("UID=abcdefg"); err != nil {
		t.Fatalf("导入 cookie 失败: %v", err)
	}
	if got := c.UID(); got != "abcdefg" {
		t.Errorf("UID() 应回退到 UID cookie, 实际 %q", got)
	}
	// 无 fid 时按原版默认 1024
	if got := c.FID(); got != "1024" {
		t.Errorf("FID() 默认值 = %q, 期望 1024", got)
	}

	// 两者都没有时 UID 为空
	resetCookies(c)
	c.uid = ""
	if got := c.UID(); got != "" {
		t.Errorf("无 cookie 时 UID() = %q, 期望空串", got)
	}
}

func TestNameParsing(t *testing.T) {
	tr := &stubTransport{}
	tr.handler = routes(t, staticRoute("mooc/accountManage", htmlResponse(http.StatusOK,
		`<html><body><input type="text" id="messageName"  value="赵六" /></body></html>`)))

	c := newTestClient(t, tr, Options{})
	if got := c.Name(); got != "赵六" {
		t.Errorf("Name() = %q, 期望 赵六", got)
	}

	tr2 := &stubTransport{}
	tr2.handler = routes(t, staticRoute("mooc/accountManage", htmlResponse(http.StatusOK, "<html>无姓名</html>")))
	c2 := newTestClient(t, tr2, Options{})
	if got := c2.Name(); got != "" {
		t.Errorf("无 messageName 时 Name() = %q, 期望空串", got)
	}
}

func TestCookieFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.txt")

	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{CookieFile: path})
	if err := c.saveCookiesToFile(); err != nil {
		t.Fatalf("saveCookiesToFile 失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 cookie 文件失败: %v", err)
	}
	content := string(data)
	if strings.Contains(content, " ") || strings.HasSuffix(content, ";") {
		t.Fatalf("落盘格式应为 k=v;k=v（无空格无尾分号）: %q", content)
	}

	// 读回：兼容带空格与不带空格
	c2 := newTestClient(t, tr, Options{CookieFile: path})
	resetCookies(c2)
	if err := c2.LoadCookies(content + "; extra=1"); err != nil {
		t.Fatalf("LoadCookies 失败: %v", err)
	}
	got := cookieMap(c2)
	if got["_uid"] != "12345678" || got["fid"] != "1234" || got["extra"] != "1" {
		t.Fatalf("cookie 往返不一致: %v", got)
	}
}

func TestCookieStringFormat(t *testing.T) {
	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{})
	s := c.CookieString()
	if !strings.Contains(s, "_uid=12345678") || !strings.Contains(s, "; ") {
		t.Fatalf("CookieString 格式异常: %q", s)
	}
}

// 未登录时 UID 取不到，仅记录错误并返回空串（不 panic）。
func TestUIDMissing(t *testing.T) {
	tr := &stubTransport{}
	c := newTestClient(t, tr, Options{})
	resetCookies(c)
	c.uid = ""
	if got := c.UID(); got != "" {
		t.Errorf("UID() = %q, 期望空串", got)
	}
	// 保持接口可用
	_ = time.Now()
}

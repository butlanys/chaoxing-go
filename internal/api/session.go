package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/butlanys/chaoxing-go/internal/consts"
	"github.com/butlanys/chaoxing-go/internal/crypto"
)

// 本文件对应 Python 原版 api/base.py 的请求底座与登录/会话部分：
// SessionManager + Chaoxing.login / get_name / _validate_cookie_session / get_uid / get_fid，
// 以及 api/cookies.py 的 save_cookies / use_cookies。

// 超星各接口地址（与 base.py 中的字面量逐字一致）
const (
	urlFanyaLogin       = "https://passport2.chaoxing.com/fanyalogin"
	urlAccountManage    = "https://passport2.chaoxing.com/mooc/accountManage"
	urlCourseListData   = "https://mooc2-ans.chaoxing.com/mooc2-ans/visit/courselistdata"
	urlInteraction      = "https://mooc2-ans.chaoxing.com/mooc2-ans/visit/interaction"
	urlStudentCourse    = "https://mooc2-ans.chaoxing.com/mooc2-ans/mycourse/studentcourse"
	urlKnowledgeCards   = "https://mooc1.chaoxing.com/mooc-ans/knowledge/cards"
	urlStudentStudyAjax = "https://mooc1.chaoxing.com/mooc-ans/mycourse/studentstudyAjax"
	urlVideoStatus      = "https://mooc1.chaoxing.com/ananas/status"
	urlMultimediaLog    = "https://mooc1.chaoxing.com/mooc-ans/multimedia/log/a"
	urlWorkAPI          = "https://mooc1.chaoxing.com/mooc-ans/api/work"
	urlWorkSubmit       = "https://mooc1.chaoxing.com/mooc-ans/work/addStudentWorkNew"
	urlWorkRecordList   = "https://mooc1.chaoxing.com/mooc-ans/work/record-list"
	urlWorkRecordDetail = "https://mooc1.chaoxing.com/mooc-ans/work/record-detail"
	urlJobDocument      = "https://mooc1.chaoxing.com/ananas/job/document"
	urlJobReadV2        = "https://mooc1.chaoxing.com/ananas/job/readv2"
	urlLiveInfo         = "https://mooc1.chaoxing.com/ananas/live/liveinfo"
	urlActiveList       = "https://mobilelearn.chaoxing.com/v2/apis/active/student/activelist"
	urlPreSign          = "https://mobilelearn.chaoxing.com/newsign/preSign"
	urlStuSignAjax      = "https://mobilelearn.chaoxing.com/pptSign/stuSignajax"
	urlSaveTimePc       = "https://zhibo.chaoxing.com/saveTimePc"
	urlMonitor          = "https://detect.chaoxing.com/api/monitor"
)

// maxBodyBytes 单次响应体读取上限，避免异常大响应撑爆内存。
const maxBodyBytes = 32 << 20

// loginLock 全局登录锁，对齐 Python SessionManager._login_lock，
// 避免多账号/多协程同时重登把账号打爆。
var loginLock sync.Mutex

// reMessageName 对齐 base.py get_name 的正则: r'id="messageName"\s+value="([^"]*)"'
var reMessageName = regexp.MustCompile(`id="messageName"\s+value="([^"]*)"`)

// httpResult 一次性读完的响应快照（避免同一响应体被重复读取）。
type httpResult struct {
	statusCode int
	body       string
	header     http.Header
}

// requestOpts 一次 HTTP 请求的全部可变项。
type requestOpts struct {
	method     string
	rawURL     string
	params     url.Values // GET 查询参数（与 URL 上已有查询串合并）
	form       url.Values // POST 表单（application/x-www-form-urlencoded）
	headers    map[string]string
	timeout    time.Duration // 0 表示使用 Client 默认超时
	noRedirect bool          // 不跟随重定向（对齐 requests 的 allow_redirects=False）
}

// mergeHeaders 合并通用请求头与本次请求的额外头（额外头覆盖通用头）。
func mergeHeaders(extra map[string]string) map[string]string {
	out := make(map[string]string, len(consts.Headers)+len(extra))
	for k, v := range consts.Headers {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// clientFor 返回指定超时/重定向策略的客户端副本（共享 Transport 与 cookie jar）。
func (c *Client) clientFor(timeout time.Duration, noRedirect bool) *http.Client {
	if timeout <= 0 {
		timeout = c.http.Timeout
	}
	if !noRedirect && timeout == c.http.Timeout {
		return c.http
	}
	cp := *c.http
	cp.Timeout = timeout
	if noRedirect {
		cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return &cp
}

// do 执行一次 HTTP 请求：合并通用头、追加查询参数、编码表单、套用超时。
func (c *Client) do(o requestOpts) (*http.Response, error) {
	u, err := url.Parse(o.rawURL)
	if err != nil {
		return nil, fmt.Errorf("请求地址非法: %w", err)
	}
	if len(o.params) > 0 {
		enc := o.params.Encode()
		if u.RawQuery != "" {
			u.RawQuery = u.RawQuery + "&" + enc
		} else {
			u.RawQuery = enc
		}
	}

	var body io.Reader
	if o.form != nil {
		body = strings.NewReader(o.form.Encode())
	}

	req, err := http.NewRequest(o.method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("构造请求失败: %w", err)
	}
	for k, v := range mergeHeaders(o.headers) {
		req.Header.Set(k, v)
	}
	// Go 的 Transport 不会采用 Header 里的 Host，需显式设置到 req.Host
	if host := o.headers["Host"]; host != "" {
		req.Host = host
	}
	if o.form != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.clientFor(o.timeout, o.noRedirect).Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// doText 执行请求并一次性读出响应体（调用方无需再关心 body 关闭）。
func (c *Client) doText(o requestOpts) (*httpResult, error) {
	resp, err := c.do(o)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return &httpResult{statusCode: resp.StatusCode, body: readBody(resp), header: resp.Header}, nil
}

// get 发起 GET 请求（默认超时）。
func (c *Client) get(rawURL string, params url.Values, headers map[string]string) (*http.Response, error) {
	return c.do(requestOpts{method: http.MethodGet, rawURL: rawURL, params: params, headers: headers})
}

// getTimeout 发起 GET 请求并指定超时。
func (c *Client) getTimeout(rawURL string, params url.Values, headers map[string]string, timeout time.Duration) (*http.Response, error) {
	return c.do(requestOpts{method: http.MethodGet, rawURL: rawURL, params: params, headers: headers, timeout: timeout})
}

// postForm 发起表单 POST 请求。
func (c *Client) postForm(rawURL string, form url.Values, headers map[string]string, timeout time.Duration) (*http.Response, error) {
	return c.do(requestOpts{method: http.MethodPost, rawURL: rawURL, form: form, headers: headers, timeout: timeout})
}

// readBody 读取响应体（限长，避免异常响应撑爆内存）。
func readBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil && len(b) == 0 {
		return ""
	}
	return string(b)
}

// cookieValue 读取当前会话中的单个 cookie。
func (c *Client) cookieValue(name string) (string, bool) {
	fj, ok := c.jar.(*flatJar)
	if !ok {
		return "", false
	}
	return fj.get(name)
}

// UID 返回当前登录用户 ID（cookie _uid，其次 UID），缓存到 c.uid。
// 对齐 base.py get_uid（原版取不到时抛 ValueError，这里返回 "" 并记 Error）。
func (c *Client) UID() string {
	c.mu.RLock()
	cached := c.uid
	c.mu.RUnlock()
	if cached != "" {
		return cached
	}

	if v, ok := c.cookieValue("_uid"); ok && v != "" {
		c.mu.Lock()
		c.uid = v
		c.mu.Unlock()
		return v
	}
	if v, ok := c.cookieValue("UID"); ok && v != "" {
		c.mu.Lock()
		c.uid = v
		c.mu.Unlock()
		return v
	}
	c.log.Error("Cannot get uid !")
	return ""
}

// FID 返回机构 ID（cookie fid，缺失时按原版默认值 1024），缓存到 c.fid。
func (c *Client) FID() string {
	c.mu.RLock()
	cached := c.fid
	c.mu.RUnlock()
	if cached != "" {
		return cached
	}

	v := "1024"
	if got, ok := c.cookieValue("fid"); ok && got != "" {
		v = got
	}
	c.mu.Lock()
	c.fid = v
	c.mu.Unlock()
	return v
}

// Name 获取当前登录用户真实姓名（对齐 base.py get_name），失败返回 ""。
func (c *Client) Name() string {
	resp, err := c.getTimeout(urlAccountManage, nil, nil, 10*time.Second)
	if err != nil {
		c.log.Debug("获取用户名失败: %v", err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body := readBody(resp)
	if m := reMessageName.FindStringSubmatch(body); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// ValidateCookieSession 校验当前 cookie 会话是否仍然有效（对齐 base.py _validate_cookie_session）。
func (c *Client) ValidateCookieSession() bool {
	if v, ok := c.cookieValue("_uid"); !ok || v == "" {
		return false
	}

	form := url.Values{}
	form.Set("courseType", "1")
	form.Set("courseFolderId", "0")
	form.Set("query", "")
	form.Set("superstarClass", "0")

	resp, err := c.postForm(urlCourseListData, form, nil, 8*time.Second)
	if err != nil {
		c.log.Debug("Cookie 校验请求失败: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}
	body := readBody(resp)
	if strings.Contains(body, "passport2.chaoxing.com") {
		return false
	}
	if strings.Contains(strings.ToLower(body), "login") {
		return false
	}
	return true
}

// reloadCookiesFromFile 从 cookie 文件重新导入会话（对齐 SessionManager.update_cookies）。
func (c *Client) reloadCookiesFromFile() {
	if c.opt.CookieFile == "" {
		return
	}
	data, err := os.ReadFile(c.opt.CookieFile)
	if err != nil {
		c.log.Trace("读取 cookies 文件失败: %v", err)
		return
	}
	if err := c.LoadCookies(string(data)); err != nil {
		c.log.Debug("导入 cookies 失败: %v", err)
	}
}

// saveCookiesToFile 把当前会话 cookie 落盘为 k=v;k=v（无空格、无尾分号，权限 0600）。
// 对齐 api/cookies.py 的 save_cookies。
func (c *Client) saveCookiesToFile() error {
	if c.opt.CookieFile == "" {
		return nil
	}
	fj, ok := c.jar.(*flatJar)
	if !ok {
		return nil
	}
	all := fj.all()
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	// 原版按 cookie 字典插入序输出，这里排序以保证落盘结果稳定可 diff
	sortStrings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(";")
		}
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(all[k])
	}
	return os.WriteFile(c.opt.CookieFile, []byte(sb.String()), 0o600)
}

// reloginIfNeeded 对齐 Python SessionManager.relogin_if_needed：
// 会话仍有效直接返回 true；否则用账号密码重新登录。
func (c *Client) reloginIfNeeded() bool {
	loginLock.Lock()
	defer loginLock.Unlock()

	if c.ValidateCookieSession() {
		return true
	}
	c.log.Info("Cookie session invalid, attempting thread-safe relogin...")
	if c.account.Username != "" && c.account.Password != "" {
		res, err := c.Login(false)
		if err == nil && res.Status {
			c.log.Info("Thread-safe relogin succeeded")
			return true
		}
		c.log.Warn("Thread-safe relogin failed: %s", res.Msg)
	}
	return false
}

// Login 登录超星（对齐 base.py login）。
//
// useCookies 为 true 时优先使用 cookie（先读 CookieFile，再校验会话），
// 校验失败且提供了账号密码则退回密码登录；为 false 时直接走密码登录。
func (c *Client) Login(useCookies bool) (LoginResult, error) {
	if useCookies {
		c.log.Info("Logging in with cookies")
		c.reloadCookiesFromFile()
		c.log.Debug("Logged in with cookies: %s", c.CookieString())

		if !c.ValidateCookieSession() {
			c.log.Warn("Cookie 登录校验失败，尝试使用账号密码重新登录")
			if c.account.Username != "" && c.account.Password != "" {
				return c.Login(false)
			}
			return LoginResult{Status: false, Msg: "cookies 已失效，请更新 cookies 或提供账号密码"}, nil
		}
		c.log.Info("登录成功...")
		realName := c.Name()
		if realName != "" {
			c.log.Info("当前登录用户: %s", realName)
		}
		if err := c.saveCookiesToFile(); err != nil {
			c.log.Debug("保存 cookies 失败: %v", err)
		}
		return LoginResult{Status: true, Msg: "登录成功", RealName: realName}, nil
	}

	// 密码登录
	if c.account.Username == "" || c.account.Password == "" {
		return LoginResult{Status: false, Msg: "未提供账号或密码"}, nil
	}

	form := url.Values{}
	form.Set("fid", "-1")
	form.Set("uname", crypto.Encrypt(c.account.Username))
	form.Set("password", crypto.Encrypt(c.account.Password))
	form.Set("refer", "https%3A%2F%2Fi.chaoxing.com")
	form.Set("t", "true")
	form.Set("forbidotherlogin", "0")
	form.Set("validate", "")
	form.Set("doubleFactorLogin", "0")
	form.Set("independentId", "0")

	c.log.Trace("正在尝试登录...")
	resp, err := c.postForm(urlFanyaLogin, form, nil, 0)
	if err != nil {
		return LoginResult{}, fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()
	body := readBody(resp)

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return LoginResult{}, fmt.Errorf("登录响应解析失败: %w", err)
	}

	if jsonBool(payload["status"]) {
		if err := c.saveCookiesToFile(); err != nil {
			c.log.Debug("保存 cookies 失败: %v", err)
		}
		c.log.Info("登录成功...")
		realName := c.Name()
		if realName != "" {
			c.log.Info("当前登录用户: %s", realName)
		}
		return LoginResult{Status: true, Msg: "登录成功", RealName: realName}, nil
	}

	msg := anyString(payload["msg2"])
	if msg == "" {
		msg = "登录失败"
	}
	return LoginResult{Status: false, Msg: msg}, nil
}

// errNotLoggedIn 未登录/会话失效
var errNotLoggedIn = errors.New("会话未登录或已失效")

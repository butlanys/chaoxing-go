package api

import (
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/butlanys/chaoxing-go/internal/consts"
)

// randFloat 返回 [min, max) 区间内的随机浮点数
func randFloat(min, max float64) float64 {
	if max <= min {
		return min
	}
	return min + rand.Float64()*(max-min)
}

// flatJar 是超星专用 cookie 容器。
//
// Python 版用 requests.Session 的 cookie 机制，会话 cookie 会随任意子域请求发送；
// 超星在 passport2 / mooc1 / mooc2-ans / mobilelearn / zhibo / fanyalogin 之间共享同一批
// cookie（_uid、fid、UID、vc3 等），因此这里用「扁平存储 + 全超星域发送」的方式复刻该行为，
// 避免标准 cookiejar 因 Domain 属性缺失而丢 cookie。
type flatJar struct {
	mu   sync.RWMutex
	data map[string]string
}

func newFlatJar() *flatJar {
	return &flatJar{data: map[string]string{}}
}

// hostAllowed 判断是否应当发送 cookie：仅超星相关域名
func hostAllowed(host string) bool {
	host = strings.ToLower(host)
	return strings.HasSuffix(host, "chaoxing.com") || strings.HasSuffix(host, "chaoxing.com.cn")
}

func (j *flatJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if u == nil || !hostAllowed(u.Hostname()) {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		if c.MaxAge < 0 {
			delete(j.data, c.Name)
			continue
		}
		j.data[c.Name] = c.Value
	}
}

func (j *flatJar) Cookies(u *url.URL) []*http.Cookie {
	if u == nil || !hostAllowed(u.Hostname()) {
		return nil
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]*http.Cookie, 0, len(j.data))
	for k, v := range j.data {
		out = append(out, &http.Cookie{Name: k, Value: v, Path: "/"})
	}
	return out
}

// get 读取单个 cookie 值
func (j *flatJar) get(name string) (string, bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	v, ok := j.data[name]
	return v, ok
}

// set 写入单个 cookie
func (j *flatJar) set(name, value string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.data[name] = value
}

// all 返回全部 cookie 的快照
func (j *flatJar) all() map[string]string {
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make(map[string]string, len(j.data))
	for k, v := range j.data {
		out[k] = v
	}
	return out
}

// newHTTPClient 构造带 cookie jar 与超时的 HTTP 客户端
func newHTTPClient(jar http.CookieJar, opt Options) *http.Client {
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = consts.DefaultTimeout
	}
	// 调用方指定了传输层时直接使用（代理、拦截器、测试注入）
	if opt.Transport != nil {
		return &http.Client{Transport: opt.Transport, Jar: jar, Timeout: timeout}
	}
	tr := &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
		Proxy:               http.ProxyFromEnvironment,
	}
	if opt.Proxy != "" {
		if pu, err := url.Parse(opt.Proxy); err == nil {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{Transport: tr, Jar: jar, Timeout: timeout}
}

// SetTransport 替换底层 RoundTripper。
//
// 用于上层注入自定义传输层（例如集成测试里把请求导向本地假超星服务端，
// 或接入自定义代理/拦截器）。传 nil 表示不做变更。
func (c *Client) SetTransport(rt http.RoundTripper) {
	if rt == nil {
		return
	}
	c.http.Transport = rt
}

// CookieString 导出当前会话 cookie 为 "k=v; k=v" 形式
func (c *Client) CookieString() string {
	fj, ok := c.jar.(*flatJar)
	if !ok {
		return ""
	}
	all := fj.all()
	if len(all) == 0 {
		return ""
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	// 稳定输出，便于落盘 diff
	sortStrings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(all[k])
	}
	return sb.String()
}

// LoadCookies 从 "k=v; k=v" 字符串导入 cookie
func (c *Client) LoadCookies(cookieStr string) error {
	fj, ok := c.jar.(*flatJar)
	if !ok {
		return nil
	}
	for _, item := range strings.Split(cookieStr, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		fj.set(name, strings.TrimSpace(parts[1]))
	}
	return nil
}

// sortStrings 简单插入排序，避免为一个排序引入额外依赖（切片通常很小）
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

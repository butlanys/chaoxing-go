// Package captcha 复刻 Python 原版 api/captcha.py 的验证码获取/识别/提交流程。
//
// 与原版差异：原版使用 ddddocr 本地库识别，Go 版通过 HTTP OCR 服务（配置键 ocr_url）
// 识别；未配置 ocr_url 时返回 DisabledSolver（Solve 返回 ErrUnavailable），
// 对应原版未安装 ddddocr 依赖时的行为。
package captcha

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 超星验证码相关常量，与 api/captcha.py 逐字一致
const (
	cxHost       = "https://mooc1.chaoxing.com"
	cxGetPath    = "/processVerifyPng.ac"
	cxSubmitPath = "/html/processVerify.ac"
	// 与原版 requests 会话一致的 Accept 头
	cxAccept = "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8"
)

// ErrUnavailable 未配置可用的 OCR 服务
var ErrUnavailable = errors.New("未配置可用的验证码识别(OCR)服务")

// Solver 验证码识别器
type Solver interface {
	// Solve 识别验证码图片，返回纯文本答案
	Solve(img []byte) (string, error)
}

// DisabledSolver 未配置 OCR 时的空实现
type DisabledSolver struct{}

// Solve 始终返回 ErrUnavailable
func (DisabledSolver) Solve([]byte) (string, error) { return "", ErrUnavailable }

// logger 最小日志接口，*logx.Logger 天然满足
type logger interface {
	Debug(format string, a ...any)
	Info(format string, a ...any)
	Warn(format string, a ...any)
	Error(format string, a ...any)
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// get 读取配置（键大小写不敏感）
func get(conf map[string]string, key string) string {
	if conf == nil {
		return ""
	}
	if v, ok := conf[strings.ToLower(key)]; ok {
		return strings.TrimSpace(v)
	}
	for k, v := range conf {
		if strings.EqualFold(k, key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// New 依据配置构造 Solver：conf["ocr_url"] 非空时使用 HTTP OCR 服务，否则返回 DisabledSolver
func New(conf map[string]string, log logger) Solver {
	if log == nil {
		log = nopLogger{}
	}
	ocrURL := get(conf, "ocr_url")
	if ocrURL == "" {
		log.Warn("未配置 OCR 服务(ocr_url)，自动验证码识别将不可用。如遇403限制请在浏览器端手动完成验证。")
		return DisabledSolver{}
	}
	log.Info("已初始化 HTTP OCR 验证码识别服务: %s", ocrURL)
	return &HTTPOcrSolver{
		url:    ocrURL,
		client: &http.Client{Timeout: 10 * time.Second},
		log:    log,
	}
}

// HTTPOcrSolver 通过 HTTP OCR 服务识别验证码：
// 以 multipart/form-data（字段名 image）把图片 POST 到 ocr_url，返回纯文本答案。
type HTTPOcrSolver struct {
	url    string
	client *http.Client
	log    logger
}

// Solve 识别验证码图片
func (s *HTTPOcrSolver) Solve(img []byte) (string, error) {
	if len(img) == 0 {
		return "", errors.New("验证码图片为空，无法识别")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("image", "captcha.png")
	if err != nil {
		return "", fmt.Errorf("构造 OCR 请求失败: %w", err)
	}
	if _, err := fw.Write(img); err != nil {
		return "", fmt.Errorf("构造 OCR 请求失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("构造 OCR 请求失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, s.url, &buf)
	if err != nil {
		return "", fmt.Errorf("构造 OCR 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := s.client.Do(req)
	if err != nil {
		s.log.Error("OCR 服务请求失败: %v", err)
		return "", fmt.Errorf("OCR 服务请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err = fmt.Errorf("OCR 服务返回异常: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
		s.log.Error("%v", err)
		return "", err
	}

	answer := parseOcrAnswer(body)
	if answer == "" {
		err = errors.New("OCR 服务未返回识别结果")
		s.log.Error("%v", err)
		return "", err
	}
	s.log.Info("验证码识别结果: %s", answer)
	return answer, nil
}

// parseOcrAnswer 兼容纯文本与 JSON 两种返回格式
func parseOcrAnswer(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "{") {
		var m map[string]any
		if json.Unmarshal(body, &m) == nil {
			for _, key := range []string{"result", "code", "text", "answer", "data"} {
				v, ok := m[key]
				if !ok {
					continue
				}
				switch t := v.(type) {
				case string:
					if s := strings.TrimSpace(t); s != "" {
						return s
					}
				case float64:
					return strconv.FormatFloat(t, 'f', -1, 64)
				}
			}
			return ""
		}
	}
	return text
}

// CxCaptcha 处理学习任务中出现的超星验证码
type CxCaptcha struct {
	host      string
	userAgent string
	cookies   string
	client    *http.Client
	solver    Solver
	log       logger
}

// NewCxCaptcha 构造验证码处理器；client 为 nil 时创建默认客户端（跳过 TLS 校验，对齐原版 verify=False）
func NewCxCaptcha(client *http.Client, ua, cookies string, s Solver, log logger) *CxCaptcha {
	if client == nil {
		client = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				// 与原版 requests 会话的 verify=False 保持一致
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			},
		}
	}
	if s == nil {
		s = DisabledSolver{}
	}
	if log == nil {
		log = nopLogger{}
	}
	return &CxCaptcha{
		host:      cxHost,
		userAgent: ua,
		cookies:   cookies,
		client:    client,
		solver:    s,
		log:       log,
	}
}

// applyHeaders 设置与原版一致的请求头
func (c *CxCaptcha) applyHeaders(req *http.Request) {
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.cookies != "" {
		req.Header.Set("Cookie", c.cookies)
	}
	req.Header.Set("Accept", cxAccept)
}

// noRedirectClient 返回不跟随重定向的客户端副本（提交验证码需要观察 302）
func (c *CxCaptcha) noRedirectClient() *http.Client {
	return &http.Client{
		Transport:     c.client.Transport,
		Jar:           c.client.Jar,
		Timeout:       c.client.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// GetCaptcha 获取验证码图片；Content-Type 必须是 image/png
func (c *CxCaptcha) GetCaptcha() ([]byte, error) {
	q := url.Values{}
	q.Set("t", strconv.Itoa(int(rand.Int31()))) // randint(0, 2147483647)
	target := c.host + cxGetPath + "?" + q.Encode()

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("获取验证码失败: %w", err)
	}
	c.applyHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		c.log.Error("获取验证码失败: %v", err)
		return nil, fmt.Errorf("获取验证码失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取验证码失败: HTTP %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	if mediaType != "image/png" {
		// 提供的 Cookies 或 UA 存在问题，导致未能正常获取验证码内容
		return nil, fmt.Errorf("获取验证码失败: Content-Type 为 %q, 期望 image/png", contentType)
	}
	if len(body) == 0 {
		return nil, errors.New("获取验证码失败: 响应内容为空")
	}
	return body, nil
}

// SubmitCaptcha 提交验证码；状态码 302 视为成功
func (c *CxCaptcha) SubmitCaptcha(token string) (bool, error) {
	q := url.Values{}
	q.Set("ucode", token)
	q.Set("app", "0")
	target := c.host + cxSubmitPath + "?" + q.Encode()

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return false, fmt.Errorf("提交验证码失败: %w", err)
	}
	c.applyHeaders(req)

	resp, err := c.noRedirectClient().Do(req)
	if err != nil {
		c.log.Error("提交验证码失败: %v", err)
		return false, fmt.Errorf("提交验证码失败: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusFound { // 302
		return true, nil
	}
	return false, nil
}

// Recognition 识别验证码图片
func (c *CxCaptcha) Recognition(img []byte) (string, error) {
	if c.solver == nil {
		return "", ErrUnavailable
	}
	answer, err := c.solver.Solve(img)
	if err != nil {
		c.log.Error("验证码识别失败: %v", err)
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", errors.New("验证码识别结果为空")
	}
	return answer, nil
}

// TryPass 尝试通过验证码验证流程：取图 -> 识别 -> 提交；任一步失败返回 false（不 panic）
func (c *CxCaptcha) TryPass() bool {
	img, err := c.GetCaptcha()
	if err != nil {
		c.log.Error("验证码自动验证失败: %v", err)
		return false
	}
	token, err := c.Recognition(img)
	if err != nil {
		c.log.Error("验证码自动验证失败: %v", err)
		return false
	}
	ok, err := c.SubmitCaptcha(token)
	if err != nil {
		c.log.Error("验证码自动验证失败: %v", err)
		return false
	}
	if !ok {
		c.log.Warn("验证码提交未通过: ucode=%s", token)
		return false
	}
	c.log.Info("验证码验证通过")
	return true
}

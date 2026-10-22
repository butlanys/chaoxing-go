package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// timeout 所有通知请求的超时时间
const timeout = 5 * time.Second

// New 依据 [notification] 配置构造通知服务；provider 或 url 为空时返回 Noop。
//
// 配置键大小写不敏感：provider / url / tg_chat_id。
// 对应 Python 原版 api/notification.py 的 NotificationFactory.create_service。
func New(conf map[string]string, log logger) Notifier {
	if log == nil {
		log = nopLogger{}
	}

	provider := strings.TrimSpace(get(conf, "provider"))
	if provider == "" {
		log.Info("未找到外部通知配置，已忽略外部通知功能")
		return &noopNotifier{log: log}
	}

	rawURL := strings.TrimSpace(get(conf, "url"))
	if rawURL == "" {
		log.Info("未配置通知服务 url，已忽略该通知服务")
		return &noopNotifier{log: log}
	}

	client := &http.Client{Timeout: timeout}

	switch {
	case strings.EqualFold(provider, "ServerChan"):
		log.Info("已初始化Server酱通知服务，URL: %s", rawURL)
		return &serverChanNotifier{rawURL: rawURL, client: client, log: log}

	case strings.EqualFold(provider, "Qmsg"):
		log.Info("已初始化Qmsg酱通知服务，URL: %s", rawURL)
		return &qmsgNotifier{rawURL: rawURL, client: client, log: log}

	case strings.EqualFold(provider, "Bark"):
		log.Info("已初始化Bark通知服务，URL: %s", rawURL)
		return &barkNotifier{rawURL: rawURL, client: client, log: log}

	case strings.EqualFold(provider, "Telegram"):
		chatID := strings.TrimSpace(get(conf, "tg_chat_id"))
		if chatID == "" {
			log.Info("未找到Telegram的url或tg_chat_id配置，已忽略该通知服务")
			return &noopNotifier{log: log}
		}
		token := extractBotToken(rawURL)
		target := telegramTarget(rawURL, token)
		log.Info("已初始化Telegram通知服务，Chat_id: %s URL: %s", chatID, target)
		return &telegramNotifier{rawURL: target, chatID: chatID, client: client, log: log}
	}

	log.Error("未找到名为 %s 的通知服务提供商，已忽略外部通知功能", provider)
	return &noopNotifier{log: log}
}

// get 读取配置（键大小写不敏感）
func get(conf map[string]string, key string) string {
	if conf == nil {
		return ""
	}
	if v, ok := conf[strings.ToLower(key)]; ok {
		return v
	}
	for k, v := range conf {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

// nopLogger 无日志实现
type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// noopNotifier 未配置通知服务时的空实现：只打日志，不发送
type noopNotifier struct {
	log logger
}

func (n *noopNotifier) Name() string { return "Noop" }

func (n *noopNotifier) Send(msg string) error {
	n.log.Debug("外部通知未配置，已忽略通知消息: %s", msg)
	return nil
}

// serverChanNotifier Server酱：POST <url>，JSON body {text, desp}
// （对齐原版 api/notification.py 的 ServerChan._send）
type serverChanNotifier struct {
	rawURL string
	client *http.Client
	log    logger
}

func (n *serverChanNotifier) Name() string { return "ServerChan" }

func (n *serverChanNotifier) Send(msg string) error {
	// 兼容两个版本的Server酱
	payload := map[string]string{"text": msg, "desp": msg}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("Server酱通知请求构造失败: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, n.rawURL, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("Server酱通知请求构造失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	return send(req, n.client, n.log, "Server酱")
}

// qmsgNotifier Qmsg酱：POST <url>?msg=<text>
// （对齐原版 Qmsg._send：POST + params）
type qmsgNotifier struct {
	rawURL string
	client *http.Client
	log    logger
}

func (n *qmsgNotifier) Name() string { return "Qmsg" }

func (n *qmsgNotifier) Send(msg string) error {
	target, err := url.Parse(n.rawURL)
	if err != nil {
		return fmt.Errorf("Qmsg酱通知 url 解析失败: %w", err)
	}
	q := target.Query()
	q.Set("msg", msg)
	target.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, target.String(), nil)
	if err != nil {
		return fmt.Errorf("Qmsg酱通知请求构造失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	return send(req, n.client, n.log, "Qmsg酱")
}

// barkNotifier Bark：POST <url>?body=<text>
// （对齐原版 Bark._send：POST + params body）
type barkNotifier struct {
	rawURL string
	client *http.Client
	log    logger
}

func (n *barkNotifier) Name() string { return "Bark" }

func (n *barkNotifier) Send(msg string) error {
	target, err := url.Parse(n.rawURL)
	if err != nil {
		return fmt.Errorf("Bark通知 url 解析失败: %w", err)
	}
	q := target.Query()
	q.Set("body", msg)
	target.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, target.String(), nil)
	if err != nil {
		return fmt.Errorf("Bark通知请求构造失败: %w", err)
	}
	return send(req, n.client, n.log, "Bark")
}

// telegramNotifier Telegram：POST https://api.telegram.org/bot<token>/sendMessage，
// form: chat_id + text + parse_mode=HTML（对齐原版 Telegram._send）
type telegramNotifier struct {
	rawURL string
	chatID string
	client *http.Client
	log    logger
}

func (n *telegramNotifier) Name() string { return "Telegram" }

func (n *telegramNotifier) Send(msg string) error {
	form := url.Values{}
	form.Set("chat_id", n.chatID)
	form.Set("text", msg)
	form.Set("parse_mode", "HTML")
	req, err := http.NewRequest(http.MethodPost, n.rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("Telegram通知请求构造失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := n.client.Do(req)
	if err != nil {
		n.log.Error("Telegram通知发送失败: %v", err)
		return fmt.Errorf("Telegram通知发送失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err = fmt.Errorf("Telegram通知发送失败: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
		n.log.Error("%v", err)
		return err
	}
	// Telegram 返回 {"ok":true/false, ...}
	var result struct {
		OK *bool `json:"ok"`
	}
	if json.Unmarshal(body, &result) == nil && result.OK != nil && !*result.OK {
		err = fmt.Errorf("Telegram通知发送失败: %s", strings.TrimSpace(string(body)))
		n.log.Error("%v", err)
		return err
	}
	n.log.Info("Telegram通知发送成功: %s", strings.TrimSpace(string(body)))
	return nil
}

// tgTokenRe 从 url 中提取 bot<token> 段
var tgTokenRe = regexp.MustCompile(`/bot([^/]+)`)

// extractBotToken 从形如 https://api.telegram.org/bot123:ABC/sendMessage 的 url 中提取 token
func extractBotToken(raw string) string {
	if m := tgTokenRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1]
	}
	return ""
}

// telegramTarget 以配置的完整 url 为准；若 url 中缺少 sendMessage 段则用 token 补全
func telegramTarget(raw, token string) string {
	if strings.Contains(raw, "sendMessage") || token == "" {
		return raw
	}
	base := strings.TrimRight(raw, "/")
	if strings.Contains(base, "/bot"+token) {
		return base + "/sendMessage"
	}
	return base + "/bot" + token + "/sendMessage"
}

// send 发送请求并统一处理状态码与日志
func send(req *http.Request, client *http.Client, log logger, name string) error {
	resp, err := client.Do(req)
	if err != nil {
		log.Error("%s通知发送失败: %v", name, err)
		return fmt.Errorf("%s通知发送失败: %w", name, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err = fmt.Errorf("%s通知发送失败: HTTP %d %s", name, resp.StatusCode, strings.TrimSpace(string(body)))
		log.Error("%v", err)
		return err
	}
	log.Info("%s通知发送成功: %s", name, strings.TrimSpace(string(body)))
	return nil
}

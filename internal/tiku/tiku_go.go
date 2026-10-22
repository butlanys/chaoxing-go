package tiku

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// goAPI GO题（网课小工具题库）查询接口（与原版 api/answer.py 的 TikuGo.api 逐字一致）。
const goAPI = "https://q.icodef.com/wyn-nb?v=4"

// 触发流控的关键词。
var goThrottleKeys = []string{"流控限制", "速度太快", "并发限制", "忙不过来"}

// TikuGo GO题（网课小工具题库）实现（对应 Python 的 TikuGo）。
//
// POST https://q.icodef.com/wyn-nb?v=4（form: question=<题目>），
// 带 Authorization 头、最小请求间隔、重试次数与指数退避。
type TikuGo struct {
	*baseProvider
	api     string
	headers map[string]string
	client  *http.Client

	requestLock     sync.Mutex
	lastRequestTime float64
	minInterval     float64
	retryTimes      int
	retryBackoff    float64
}

// goResp GO题接口响应体（code 可能是数字或字符串，data 可能是字符串或对象）。
type goResp struct {
	Code json.RawMessage `json:"code"`
	Data json.RawMessage `json:"data"`
	Msg  json.RawMessage `json:"msg"`
}

// newGoTiku 构造 GO题 题库。
func newGoTiku(conf map[string]string, log logger) *TikuGo {
	b := newBase("GO题（网课小工具题库）", conf, log)
	g := &TikuGo{
		baseProvider: b,
		api:          goAPI,
		headers: map[string]string{
			"Authorization": "",
			"Content-Type":  "application/x-www-form-urlencoded",
		},
		client:       newHTTPClient(15 * time.Second),
		minInterval:  1.0,
		retryTimes:   3,
		retryBackoff: 1.2,
	}
	g.initFromConf(conf)
	b.query = g.queryOne
	return g
}

// initFromConf 读取 go_* 配置项（对齐 Python 的 _init_tiku）。
func (g *TikuGo) initFromConf(conf map[string]string) {
	if v, ok := conf["go_authorization"]; ok {
		g.headers["Authorization"] = v
	}

	if raw, ok := conf["go_min_interval"]; ok && strings.TrimSpace(raw) != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && v >= 0 {
			g.minInterval = v
		} else {
			g.log.Warn("%s配置 go_min_interval 无效，使用默认值 %v", g.name, g.minInterval)
		}
	}

	if raw, ok := conf["go_retry_times"]; ok && strings.TrimSpace(raw) != "" {
		if v, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && v >= 1 {
			g.retryTimes = v
		} else {
			g.log.Warn("%s配置 go_retry_times 无效，使用默认值 %d", g.name, g.retryTimes)
		}
	}

	if raw, ok := conf["go_retry_backoff"]; ok && strings.TrimSpace(raw) != "" {
		if v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil && v >= 0 {
			g.retryBackoff = v
		} else {
			g.log.Warn("%s配置 go_retry_backoff 无效，使用默认值 %v", g.name, g.retryBackoff)
		}
	}
}

// sleepForNextRequest 保证相邻请求间隔不小于 minInterval。
func (g *TikuGo) sleepForNextRequest() {
	g.requestLock.Lock()
	now := float64(time.Now().UnixNano()) / 1e9
	wait := g.lastRequestTime + g.minInterval - now
	if wait < 0 {
		wait = 0
	}
	g.lastRequestTime = now + wait
	g.requestLock.Unlock()
	if wait > 0 {
		time.Sleep(time.Duration(wait * float64(time.Second)))
	}
}

// markRequestFinished 记录本次请求完成时间。
func (g *TikuGo) markRequestFinished() {
	g.requestLock.Lock()
	g.lastRequestTime = float64(time.Now().UnixNano()) / 1e9
	g.requestLock.Unlock()
}

// requestQuestion 发起一次查询请求。
func (g *TikuGo) requestQuestion(question string, attempt int) *http.Response {
	g.sleepForNextRequest()
	form := url.Values{}
	form.Set("question", question)
	req, err := http.NewRequest(http.MethodPost, g.api, bytes.NewBufferString(form.Encode()))
	if err != nil {
		g.log.Error("%s查询异常 (%d/%d): %v", g.name, attempt, g.retryTimes, err)
		g.markRequestFinished()
		return nil
	}
	for k, v := range g.headers {
		req.Header.Set(k, v)
	}
	res, err := g.client.Do(req)
	if err != nil {
		g.log.Error("%s查询异常 (%d/%d): %v", g.name, attempt, g.retryTimes, err)
		g.markRequestFinished()
		return nil
	}
	g.markRequestFinished()
	return res
}

// parsedGo GO题解析结果。
type parsedGo struct {
	code        int
	answer      string
	msg         string
	isThrottled bool
}

// parseResponse 解析 GO题 响应。
func (g *TikuGo) parseResponse(res *http.Response) *parsedGo {
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		g.log.Error("%s查询失败: 状态码 %d", g.name, res.StatusCode)
		return nil
	}
	var raw goResp
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		g.log.Error("%s查询失败: 返回内容不是有效JSON: %v", g.name, err)
		return nil
	}

	code := parseRawInt(raw.Code)
	answer := parseRawData(raw.Data)
	msg := parseRawString(raw.Msg)
	text := answer + " " + msg
	throttled := false
	for _, k := range goThrottleKeys {
		if strings.Contains(text, k) {
			throttled = true
			break
		}
	}
	return &parsedGo{code: code, answer: answer, msg: msg, isThrottled: throttled}
}

// sleepRetry 重试前等待（指数退避：backoff * attempt）。
func (g *TikuGo) sleepRetry(attempt int, reason string, includeMinInterval bool) {
	var sleepSeconds float64
	if includeMinInterval {
		sleepSeconds = g.minInterval
		if v := g.retryBackoff * float64(attempt); v > sleepSeconds {
			sleepSeconds = v
		}
	} else {
		sleepSeconds = g.retryBackoff * float64(attempt)
	}
	g.log.Warn("%s%s，%.1fs 后重试 (%d/%d)", g.name, reason, sleepSeconds, attempt, g.retryTimes)
	if sleepSeconds > 0 {
		time.Sleep(time.Duration(sleepSeconds * float64(time.Second)))
	}
}

// isPlaceholderAnswer GO题未搜到时会返回“李恒雅正在努力撰写中...”占位文案。
func (g *TikuGo) isPlaceholderAnswer(answer, msg string) bool {
	return strings.Contains(answer, "李恒雅") || strings.Contains(msg, "李恒雅")
}

var (
	reGoPrefixFullWidth = regexp.MustCompile(`^【[^】]+】\s*`)
	reGoPrefixBracket   = regexp.MustCompile(`^\[[^\]]+\]\s*`)
)

// queryOne 依次尝试原题、去【..】前缀、去[..]前缀三种候选标题。
func (g *TikuGo) queryOne(q Question) (string, bool) {
	title := q.Title
	candidates := []string{
		title,
		strings.TrimSpace(reGoPrefixFullWidth.ReplaceAllString(title, "")),
		strings.TrimSpace(reGoPrefixBracket.ReplaceAllString(title, "")),
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if ans, ok := g.queryOnce(c); ok {
			return ans, true
		}
	}
	return "", false
}

// queryOnce 带重试与退避的单次标题查询。
func (g *TikuGo) queryOnce(question string) (string, bool) {
	for attempt := 1; attempt <= g.retryTimes; attempt++ {
		res := g.requestQuestion(question, attempt)
		if res == nil {
			if attempt < g.retryTimes {
				g.sleepRetry(attempt, "查询异常", true)
				continue
			}
			break
		}

		parsed := g.parseResponse(res)
		if parsed == nil {
			return "", false
		}

		if parsed.code != 1 {
			if parsed.isThrottled && attempt < g.retryTimes {
				g.sleepRetry(attempt, "触发流控", false)
				continue
			}
			msg := parsed.msg
			if msg == "" {
				msg = "未知错误"
			}
			g.log.Info("%s未命中或失败: %s", g.name, msg)
			return "", false
		}

		if parsed.answer == "" {
			return "", false
		}

		if g.isPlaceholderAnswer(parsed.answer, parsed.msg) {
			if parsed.isThrottled && attempt < g.retryTimes {
				g.sleepRetry(attempt, "命中流控提示", false)
				continue
			}
			return "", false
		}

		return parsed.answer, true
	}
	return "", false
}

// parseRawInt 解析可能是数字或字符串的整数。
func parseRawInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return v
		}
	}
	return 0
}

// parseRawString 解析可能是数字/字符串的文本字段。
func parseRawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(raw))
}

// parseRawData 解析 data 字段：字符串直接返回；对象则取其中的 answer 字段。
func parseRawData(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		if a, ok := obj["answer"]; ok {
			return parseRawString(a)
		}
	}
	return ""
}

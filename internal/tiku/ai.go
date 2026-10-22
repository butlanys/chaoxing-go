package tiku

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// aiDefaultMinInterval AI 题库默认最小请求间隔（秒）。
const aiDefaultMinInterval = 3

// AI 大模型答题实现（对应 Python 的 AI），对接 OpenAI 兼容的 chat/completions 接口。
//
// endpoint 既可以是带 /v1 的 base（如 https://example.com/v1），也可以是完整 URL
// （如 https://example.com/v1/chat/completions），两种写法都会自动补全为 chat/completions。
// 支持 SetWorkFeedback 后把上一轮章节检测错题反馈拼进 prompt 重新作答。
type AI struct {
	*baseProvider
	endpoint           string
	key                string
	model              string
	httpProxy          string
	minIntervalSeconds int
	client             *http.Client

	mu              sync.Mutex
	lastRequestTime float64
}

// chatMessage OpenAI 兼容消息体。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// aiChatResponse chat/completions 响应体。
type aiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// 去除大模型输出中可能存在的 Markdown JSON 包裹。
var reMDJSONWrapper = regexp.MustCompile("(?s)^\\s*```(?:json)?\\s*(.*?)\\s*```\\s*$")

// 去除选项前的字母（如 "A "），防止大模型直接输出字母而非内容。
var reAIOptionPrefix = regexp.MustCompile(`^[A-Z]\s*`)

func removeMDJSONWrapper(s string) string {
	if m := reMDJSONWrapper.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(s)
}

// newAI 构造 AI 题库；endpoint/key 缺失时停用。
func newAI(conf map[string]string, log logger) *AI {
	b := newBase("AI大模型答题", conf, log)
	a := &AI{
		baseProvider:       b,
		endpoint:           strings.TrimSpace(conf["endpoint"]),
		key:                conf["key"],
		model:              strings.TrimSpace(conf["model"]),
		httpProxy:          strings.TrimSpace(conf["http_proxy"]),
		minIntervalSeconds: parseInt(conf["min_interval_seconds"], aiDefaultMinInterval),
	}
	client, err := newHTTPClientWithProxy(30*time.Second, a.httpProxy)
	if err != nil {
		a.log.Warn("%s代理配置无效，忽略代理: %v", b.name, err)
		client = newHTTPClient(30 * time.Second)
	}
	a.client = client

	if a.endpoint == "" || a.key == "" {
		a.log.Error("%s未配置 endpoint/key, 已停用", b.name)
		b.disabled = true
	}
	b.query = a.queryOne
	return a
}

// SetWorkFeedback 设置上一轮章节检测的错误反馈（重做时拼入 prompt）。
func (a *AI) SetWorkFeedback(feedback []string) {
	a.feedback = feedback
}

// buildWorkFeedbackText 将反馈转为提示词文本（对齐 Python 的 _build_work_feedback_text）。
func (a *AI) buildWorkFeedbackText() string {
	if len(a.feedback) == 0 {
		return ""
	}
	lines := []string{
		"你上一次作答的章节检测中有以下题目回答错误，请根据题目与正确答案仔细思考错因，纠正你的判断，本次作答务必保证每道题都正确：",
	}
	lines = append(lines, a.feedback...)
	return strings.Join(lines, "\n")
}

// isDeepseekV4 是否为 DeepSeek V4（其默认思考模式可能导致 content 为空，需显式关闭）。
func (a *AI) isDeepseekV4() bool {
	return strings.Contains(strings.ToLower(a.endpoint), "api.deepseek.com") &&
		strings.HasPrefix(strings.ToLower(a.model), "deepseek-v4")
}

// buildChatURL 兼容 base(/v1) 与完整 URL 两种 endpoint 写法。
func buildChatURL(endpoint string) string {
	e := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	switch {
	case strings.HasSuffix(e, "/chat/completions"):
		return e
	case strings.HasSuffix(e, "/v1"):
		return e + "/chat/completions"
	default:
		return e + "/v1/chat/completions"
	}
}

// waitForInterval 保证两次请求间隔不小于 minIntervalSeconds。
func (a *AI) waitForInterval() {
	if a.lastRequestTime <= 0 {
		return
	}
	interval := float64(time.Now().UnixNano())/1e9 - a.lastRequestTime
	if interval < float64(a.minIntervalSeconds) {
		sleep := float64(a.minIntervalSeconds) - interval
		a.log.Debug("API请求间隔过短, 等待 %v 秒", sleep)
		time.Sleep(time.Duration(sleep * float64(time.Second)))
	}
}

// buildPrompt 依据题型构造 system / user 提示词（逐字对齐 Python）。
func (a *AI) buildPrompt(q Question) (string, string) {
	optionsList := strings.Split(q.Options, "\n")
	cleaned := make([]string, len(optionsList))
	for i, o := range optionsList {
		cleaned[i] = reAIOptionPrefix.ReplaceAllString(o, "")
	}
	options := strings.Join(cleaned, "\n")

	switch q.Type {
	case "single":
		return `本题为单选题，你只能选择一个选项，请根据题目和选项回答问题，以json格式输出正确的选项内容，示例回答：{"Answer": ["答案"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`,
			"题目：" + q.Title + "\n选项：" + options
	case "multiple":
		return `本题为多选题，你必须选择两个或以上选项，请根据题目和选项回答问题，以json格式输出正确的选项内容，示例回答：{"Answer": ["答案1",\n"答案2",\n"答案3"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`,
			"题目：" + q.Title + "\n选项：" + options
	case "completion":
		return `本题为填空题，你必须根据语境和相关知识填入合适的内容，请根据题目回答问题，以json格式输出正确的答案，示例回答：{"Answer": ["答案"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`,
			"题目：" + q.Title
	case "judgement":
		return `本题为判断题，你只能回答正确或者错误，请根据题目回答问题，以json格式输出正确的答案，示例回答：{"Answer": ["正确"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`,
			"题目：" + q.Title
	default:
		return `本题为简答题，你必须根据语境和相关知识填入合适的内容，请根据题目回答问题，以json格式输出正确的答案，示例回答：{"Answer": ["这是我的答案"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`,
			"题目：" + q.Title
	}
}

func (a *AI) queryOne(q Question) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.queryLocked(q)
}

// queryLocked 在持有锁的情况下执行一次查询（含限速、prompt 构造与 JSON 解析容错）。
func (a *AI) queryLocked(q Question) (string, bool) {
	systemPrompt, userContent := a.buildPrompt(q)

	messages := []chatMessage{{Role: "system", Content: systemPrompt}}
	if fb := a.buildWorkFeedbackText(); fb != "" {
		messages = append(messages, chatMessage{Role: "system", Content: fb})
	}
	messages = append(messages, chatMessage{Role: "user", Content: userContent})

	body := map[string]any{"model": a.model, "messages": messages}
	if a.isDeepseekV4() {
		body["thinking"] = map[string]string{"type": "disabled"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		a.log.Error("无法解析大模型输出内容")
		return "", false
	}

	a.waitForInterval()
	a.lastRequestTime = float64(time.Now().UnixNano()) / 1e9

	req, err := http.NewRequest(http.MethodPost, buildChatURL(a.endpoint), bytes.NewReader(payload))
	if err != nil {
		a.log.Error("%s请求构造失败: %v", a.name, err)
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+a.key)
	req.Header.Set("Content-Type", "application/json")

	res, err := a.client.Do(req)
	if err != nil {
		a.log.Error("%s请求失败: %v", a.name, err)
		return "", false
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		a.log.Error("%s请求失败: 状态码 %d", a.name, res.StatusCode)
		return "", false
	}

	var cr aiChatResponse
	if err := json.NewDecoder(res.Body).Decode(&cr); err != nil {
		a.log.Error("无法解析大模型输出内容")
		return "", false
	}
	if len(cr.Choices) == 0 {
		a.log.Error("无法解析大模型输出内容")
		return "", false
	}

	var parsed struct {
		Answer []string `json:"Answer"`
	}
	if err := json.Unmarshal([]byte(removeMDJSONWrapper(cr.Choices[0].Message.Content)), &parsed); err != nil {
		a.log.Error("无法解析大模型输出内容")
		return "", false
	}
	ans := strings.TrimSpace(strings.Join(parsed.Answer, "\n"))
	if ans == "" {
		return "", false
	}
	return ans, true
}

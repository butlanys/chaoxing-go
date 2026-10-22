package tiku

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// siliconflowDefaultEndpoint 硅基流动默认接口地址。
const siliconflowDefaultEndpoint = "https://api.siliconflow.cn/v1/chat/completions"

// siliconflowDefaultModel 硅基流动默认模型（对齐 Python 的 _init_tiku 默认值）。
const siliconflowDefaultModel = "deepseek-ai/DeepSeek-V3"

// SiliconFlow 硅基流动大模型题库（对应 Python 的 SiliconFlow）。
type SiliconFlow struct {
	*baseProvider
	apiEndpoint string
	apiKey      string
	modelName   string
	client      *http.Client

	mu              sync.Mutex
	lastRequestTime float64
	minInterval     int
}

// newSiliconFlow 构造硅基流动题库；未配置 key 时停用。
func newSiliconFlow(conf map[string]string, log logger) *SiliconFlow {
	b := newBase("硅基流动大模型", conf, log)
	s := &SiliconFlow{
		baseProvider: b,
		apiEndpoint:  firstNonEmpty(conf["siliconflow_endpoint"], siliconflowDefaultEndpoint),
		apiKey:       strings.TrimSpace(conf["siliconflow_key"]),
		modelName:    firstNonEmpty(conf["siliconflow_model"], siliconflowDefaultModel),
		client:       newHTTPClient(30 * time.Second),
		minInterval:  parseInt(conf["min_interval_seconds"], aiDefaultMinInterval),
	}
	if s.apiKey == "" {
		s.log.Error("%s未配置 siliconflow_key, 已停用", b.name)
		b.disabled = true
	}
	b.query = s.queryOne
	return s
}

// waitForInterval 保证两次请求间隔不小于 minInterval。
func (s *SiliconFlow) waitForInterval() {
	if s.lastRequestTime <= 0 {
		return
	}
	interval := float64(time.Now().UnixNano())/1e9 - s.lastRequestTime
	if interval < float64(s.minInterval) {
		sleep := float64(s.minInterval) - interval
		s.log.Debug("API请求间隔过短, 等待 %v 秒", sleep)
		time.Sleep(time.Duration(sleep * float64(time.Second)))
	}
}

// buildSystemPrompt 依据题型构造系统提示词（逐字对齐 Python）。
func buildSiliconflowSystemPrompt(typ string) string {
	switch typ {
	case "single":
		return `本题为单选题，请根据题目和选项选择唯一正确答案，输出的是选项的具体内容，而不是内容前的ABCD，并以JSON格式输出：示例回答：{"Answer": ["正确选项内容"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`
	case "multiple":
		return `本题为多选题，请选择所有正确选项，输出的是选项的具体内容，而不是内容前的ABCD，以JSON格式输出：示例回答：{"Answer": ["选项1","选项2"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`
	case "completion":
		return `本题为填空题，请直接给出填空内容，以JSON格式输出：示例回答：{"Answer": ["答案文本"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`
	case "judgement":
		return `本题为判断题，请回答'正确'或'错误'，以JSON格式输出：示例回答：{"Answer": ["正确"]}。除此之外不要输出任何多余的内容，也不要使用MD语法。如果你使用了互联网搜索，也请不要返回搜索的结果和参考资料`
	default:
		return ""
	}
}

func (s *SiliconFlow) queryOne(q Question) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryLocked(q)
}

func (s *SiliconFlow) queryLocked(q Question) (string, bool) {
	payload := map[string]any{
		"model": s.modelName,
		"messages": []chatMessage{
			{Role: "system", Content: buildSiliconflowSystemPrompt(q.Type)},
			{Role: "user", Content: "题目：" + q.Title + "\n选项：" + q.Options},
		},
		"stream":          false,
		"max_tokens":      4096,
		"temperature":     0.7,
		"top_p":           0.7,
		"response_format": map[string]string{"type": "text"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		s.log.Error("%s请求构造失败: %v", s.name, err)
		return "", false
	}

	s.waitForInterval()

	req, err := http.NewRequest(http.MethodPost, s.apiEndpoint, bytes.NewReader(body))
	if err != nil {
		s.log.Error("%s请求构造失败: %v", s.name, err)
		return "", false
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := s.client.Do(req)
	s.lastRequestTime = float64(time.Now().UnixNano()) / 1e9
	if err != nil {
		s.log.Error("硅基流动API异常：%v", err)
		return "", false
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		s.log.Error("API请求失败：%d", res.StatusCode)
		return "", false
	}

	var cr aiChatResponse
	if err := json.NewDecoder(res.Body).Decode(&cr); err != nil {
		s.log.Error("硅基流动API异常：%v", err)
		return "", false
	}
	if len(cr.Choices) == 0 {
		s.log.Error("无法解析大模型输出内容")
		return "", false
	}

	var parsed struct {
		Answer []string `json:"Answer"`
	}
	if err := json.Unmarshal([]byte(removeMDJSONWrapper(cr.Choices[0].Message.Content)), &parsed); err != nil {
		s.log.Error("无法解析大模型输出内容")
		return "", false
	}
	ans := strings.TrimSpace(strings.Join(parsed.Answer, "\n"))
	if ans == "" {
		return "", false
	}
	return ans, true
}

// firstNonEmpty 返回第一个非空（去空白后）字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

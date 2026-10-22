package tiku

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LIKE知识库接口常量（对应 Python 的 TikuLike）。
const (
	likeQueryAPI     = "https://app.datam.site/api/v1/query"
	likeModelsAPI    = "https://app.datam.site/api/v1/query/models"
	likeBalanceAPI   = "https://app.datam.site/api/v1/balance"
	likeHomepage     = "https://www.datam.site"
	likeDefaultModel = "glm-4.5-air"
)

// TikuLike LIKE知识库实现（对应 Python 的 TikuLike）。
// 多 token 随机选择，支持联网搜索 / 视觉 / 模型选择 / 自动重试等配置。
type TikuLike struct {
	*baseProvider
	queryAPI   string
	balanceAPI string
	modelsAPI  string
	homepage   string
	client     *http.Client

	model      string
	retry      bool
	retryTimes int
	tokens     []string
	balance    map[string]int
	search     bool
	vision     bool
	count      int

	mu sync.Mutex
}

// likeResp LIKE 查询响应体。
type likeResp struct {
	Message string `json:"message"`
	Results *struct {
		Output *struct {
			QuestionType *string         `json:"questionType"`
			Answer       json.RawMessage `json:"answer"`
		} `json:"output"`
	} `json:"results"`
}

// newLike 构造 LIKE知识库；无 token 时停用。
func newLike(conf map[string]string, log logger) *TikuLike {
	b := newBase("LIKE知识库", conf, log)
	l := &TikuLike{
		baseProvider: b,
		queryAPI:     likeQueryAPI,
		balanceAPI:   likeBalanceAPI,
		modelsAPI:    likeModelsAPI,
		homepage:     likeHomepage,
		client:       newHTTPClient(300 * time.Second),
		balance:      map[string]int{},
		retryTimes:   3,
	}
	// 读取配置（对齐 Python 的 load_config）
	l.search = parseBool(conf["likeapi_search"], false)
	l.model = firstNonEmpty(conf["likeapi_model"], likeDefaultModel)
	l.vision = parseBool(conf["likeapi_vision"], true)
	l.retry = parseBool(conf["likeapi_retry"], true)
	l.retryTimes = parseInt(conf["likeapi_retry_times"], 3)

	// 载入 token（对齐 Python 的 load_tokens）
	l.tokens = splitList(conf["tokens"])
	if len(l.tokens) == 0 {
		l.log.Error("%s初始化失败: 未加载任何有效的Token", b.name)
		b.disabled = true
	} else {
		l.updateTimes()
	}
	b.query = l.queryOne
	return l
}

// tail 返回字符串末尾 n 个字符（token 打码用）。
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// getAPIBalance 查询单个 token 的余额（对应 Python 的 get_api_balance）。
func (l *TikuLike) getAPIBalance(token string) int {
	if token == "" {
		l.log.Error("%s获取余额失败: 未提供有效的token", l.name)
		return 0
	}
	req, err := http.NewRequest(http.MethodGet, l.balanceAPI, nil)
	if err != nil {
		l.log.Error("%s获取余额失败: %v", l.name, err)
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := l.client.Do(req)
	if err != nil {
		l.log.Error("%sToken余额查询过程中出现错误: %v", l.name, err)
		return 0
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		l.log.Error("%s请求余额接口失败，状态码: %d", l.name, res.StatusCode)
		return 0
	}
	var r struct {
		Balance json.RawMessage `json:"balance"`
	}
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		l.log.Error("%s余额响应解析失败: 响应格式不正确", l.name)
		return 0
	}
	return parseRawInt(r.Balance)
}

// updateTimes 刷新所有 token 的余额（对应 Python 的 update_times）。
func (l *TikuLike) updateTimes() {
	if len(l.tokens) == 0 {
		l.log.Warn("%s未加载任何Token, 无法更新余额", l.name)
		return
	}
	for _, token := range l.tokens {
		bal := l.getAPIBalance(token)
		l.balance[token] = bal
		l.log.Info("当前LIKE知识库Token: ...%s 的剩余查询次数为: %d (仅供参考, 实际次数以查询结果为准)", tail(token, 5), bal)
	}
}

// querySingle 使用指定 token 查询单个问题（对应 Python 的 _query_single）。
func (l *TikuLike) querySingle(token, query string) string {
	if token == "" {
		l.log.Error("%s查询失败: 未提供有效的token", l.name)
		return ""
	}
	if query == "" {
		l.log.Error("%s查询失败: 查询内容为空", l.name)
		return ""
	}
	payload, err := json.Marshal(map[string]any{
		"query":  query,
		"model":  l.model,
		"search": l.search,
		"vision": l.vision,
	})
	if err != nil {
		l.log.Error("%s查询异常: %v", l.name, err)
		return ""
	}
	req, err := http.NewRequest(http.MethodPost, l.queryAPI, bytes.NewReader(payload))
	if err != nil {
		l.log.Error("%s查询异常: %v", l.name, err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := l.client.Do(req)
	if err != nil {
		l.log.Error("%s查询异常: %v", l.name, err)
		return ""
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
		ans, _ := l.parseResponse(res)
		return ans
	case http.StatusUnauthorized:
		l.log.Error("%s认证失败: 请检查Token是否正确或已过期", l.name)
	case http.StatusTooManyRequests:
		l.log.Error("%s请求过于频繁: 已达到API速率限制", l.name)
	case http.StatusInternalServerError:
		l.log.Error("%s服务器内部错误: API服务暂时不可用", l.name)
	case http.StatusBadRequest:
		l.log.Error("%s请求参数错误: 请检查查询内容格式", l.name)
	case http.StatusForbidden:
		l.log.Error("%s访问被拒绝: 可能是Token权限不足", l.name)
	default:
		l.log.Error("%s查询失败: 状态码 %d", l.name, res.StatusCode)
	}
	return ""
}

// parseResponse 解析 LIKE 查询响应（对应 Python 的 _parse_response）。
func (l *TikuLike) parseResponse(res *http.Response) (string, bool) {
	var r likeResp
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		l.log.Error("%s响应解析失败: 响应不是有效的JSON格式", l.name)
		return "", false
	}
	if r.Message != "" {
		l.log.Info("%s响应消息: %s", l.name, r.Message)
	}
	if r.Results == nil || r.Results.Output == nil {
		l.log.Error("%s查询结果格式错误: API返回结果中results/output字段格式不正确", l.name)
		return "", false
	}
	out := r.Results.Output
	if out.QuestionType == nil {
		l.log.Error("%s查询结果中questionType字段不存在", l.name)
		return "", false
	}
	if len(out.Answer) == 0 || string(out.Answer) == "null" {
		l.log.Error("%s查询结果中answer字段不存在", l.name)
		return "", false
	}
	var answer map[string]any
	if err := json.Unmarshal(out.Answer, &answer); err != nil {
		l.log.Error("%s答案格式错误: 不是有效的字典格式", l.name)
		return "", false
	}
	ans := l.extractAnswerByType(*out.QuestionType, answer)
	if ans == "" {
		return "", false
	}
	return ans, true
}

// extractAnswerByType 按题型提取答案（对应 Python 的 _extract_answer_by_type）。
func (l *TikuLike) extractAnswerByType(qType string, answer map[string]any) string {
	switch qType {
	case "CHOICE":
		raw, ok := answer["selectedOptions"]
		if !ok {
			l.log.Error("%sCHOICE类型题目缺少selectedOptions字段", l.name)
			return ""
		}
		list, ok := raw.([]any)
		if !ok || len(list) == 0 {
			l.log.Error("%sCHOICE类型题目没有有效的选项内容", l.name)
			return ""
		}
		valid := make([]string, 0, len(list))
		for _, o := range list {
			if o == nil {
				continue
			}
			if s := strings.TrimSpace(fmt.Sprintf("%v", o)); s != "" {
				valid = append(valid, s)
			}
		}
		if len(valid) == 0 {
			l.log.Error("%sCHOICE类型题目没有有效的选项内容", l.name)
			return ""
		}
		return strings.Join(valid, "\n")
	case "FILL_IN_BLANK":
		raw, ok := answer["blanks"]
		if !ok {
			l.log.Error("%sFILL_IN_BLANK类型题目缺少blanks字段", l.name)
			return ""
		}
		list, ok := raw.([]any)
		if !ok || len(list) == 0 {
			l.log.Error("%sFILL_IN_BLANK类型题目没有有效的填空内容", l.name)
			return ""
		}
		valid := make([]string, 0, len(list))
		for _, b := range list {
			if b == nil {
				continue
			}
			if s := strings.TrimSpace(fmt.Sprintf("%v", b)); s != "" {
				valid = append(valid, s)
			}
		}
		if len(valid) == 0 {
			l.log.Error("%sFILL_IN_BLANK类型题目没有有效的填空内容", l.name)
			return ""
		}
		return strings.Join(valid, "\n")
	case "JUDGMENT":
		raw, ok := answer["isCorrect"]
		if !ok {
			l.log.Error("%sJUDGMENT类型题目缺少isCorrect字段", l.name)
			return ""
		}
		b, ok := raw.(bool)
		if !ok {
			l.log.Error("%sJUDGMENT类型题目缺少isCorrect字段", l.name)
			return ""
		}
		if b {
			return "正确"
		}
		return "错误"
	default:
		raw, ok := answer["otherText"]
		if !ok {
			l.log.Error("%s未知题目类型%s且缺少otherText字段", l.name, qType)
			return ""
		}
		return fmt.Sprintf("%v", raw)
	}
}

// queryOne 单题查询：构造提问文本 -> 随机 token -> 带重试查询 -> 计数更新余额。
func (l *TikuLike) queryOne(q Question) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.tokens) == 0 {
		l.log.Error("%s未加载任何有效的Token", l.name)
		return "", false
	}

	prefixMap := map[string]string{
		"single":     "【单选题】",
		"multiple":   "【多选题】",
		"completion": "【填空题】",
		"judgement":  "【判断题】",
	}
	prefix := prefixMap[q.Type]
	if prefix == "" {
		prefix = "【其他类型题目】"
	}
	options := strings.ReplaceAll(q.Options, "\n", ", ")
	question := prefix + q.Title + "\n"
	if q.Type == "single" || q.Type == "multiple" {
		question += "选项为: " + options + "\n"
	}

	// 随机选择一个 token
	token := l.tokens[rand.Intn(len(l.tokens))]
	if l.balance[token] <= 0 {
		l.log.Error("%s当前Token查询次数不足: ...%s", l.name, tail(token, 5))
		avail := make([]string, 0, len(l.tokens))
		for _, t := range l.tokens {
			if l.balance[t] > 0 {
				avail = append(avail, t)
			}
		}
		if len(avail) == 0 {
			l.log.Error("%s所有Token查询次数都不足", l.name)
			return "", false
		}
		token = avail[rand.Intn(len(avail))]
	}

	ans := ""
	tryTimes := 0
	for ans == "" && l.retry && tryTimes < l.retryTimes {
		ans = l.querySingle(token, question)
		tryTimes++
		if ans != "" {
			l.balance[token]--
			l.log.Info("使用Token ...%s 查询成功，剩余次数: %d", tail(token, 5), l.balance[token])
			break
		} else if tryTimes < l.retryTimes {
			l.log.Warn("使用Token ...%s 查询失败，进行第 %d 次重试...", tail(token, 5), tryTimes+1)
		}
	}

	// 10 次查询后更新余额
	l.count = (l.count + 1) % 10
	if l.count == 0 {
		l.updateTimes()
	}

	if ans == "" {
		return "", false
	}
	return ans, true
}

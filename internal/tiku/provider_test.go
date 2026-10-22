package tiku

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// reqCapture 线程安全地记录服务端收到的请求。
type reqCapture struct {
	mu     sync.Mutex
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

func (c *reqCapture) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Method = r.Method
	c.Path = r.URL.Path
	c.Query = r.URL.Query()
	c.Header = r.Header.Clone()
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		c.Body = b
	}
}

func (c *reqCapture) jsonBody(t *testing.T) map[string]any {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var m map[string]any
	if err := json.Unmarshal(c.Body, &m); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v (%s)", err, string(c.Body))
	}
	return m
}

func (c *reqCapture) form(t *testing.T) url.Values {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	v, err := url.ParseQuery(string(c.Body))
	if err != nil {
		t.Fatalf("请求体不是合法 form: %v", err)
	}
	return v
}

// ---------------- TikuYanxi ----------------

func TestTikuYanxiRequest(t *testing.T) {
	cap := &reqCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"code":1,"data":{"answer":"答案A","times":88},"message":""}`))
	}))
	defer srv.Close()

	y := newYanxi(map[string]string{"tokens": "tok1"}, nil)
	y.api = srv.URL + "/query"
	y.cache = tmpCache(t)

	ans, ok := y.Query(Question{Title: "题干 内容", Type: "single"})
	if !ok || ans != "答案A" {
		t.Fatalf("查询失败: %q %v", ans, ok)
	}
	if cap.Method != http.MethodGet {
		t.Errorf("方法应为 GET, 得到 %s", cap.Method)
	}
	if cap.Path != "/query" {
		t.Errorf("路径应为 /query, 得到 %s", cap.Path)
	}
	if got := cap.Query.Get("question"); got != "题干 内容" {
		t.Errorf("question 参数错误: %q", got)
	}
	if got := cap.Query.Get("token"); got != "tok1" {
		t.Errorf("token 参数错误: %q", got)
	}
	if y.times != 88 {
		t.Errorf("剩余次数未按响应校正: %d", y.times)
	}
	if v, ok := y.cache.GetCache("题干 内容"); !ok || v != "答案A" {
		t.Errorf("命中后未写入缓存: %q %v", v, ok)
	}
}

func TestTikuYanxiMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"answer":"","times":5},"message":"未找到该题目"}`))
	}))
	defer srv.Close()

	y := newYanxi(map[string]string{"tokens": "tok1"}, nil)
	y.api = srv.URL
	y.cache = tmpCache(t)

	if _, ok := y.Query(Question{Title: "题干", Type: "single"}); ok {
		t.Fatal("未命中时不应返回答案")
	}
}

func TestTikuYanxiTokenRotation(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token")
		mu.Lock()
		seen = append(seen, tok)
		mu.Unlock()
		if tok == "tok1" {
			_, _ = w.Write([]byte(`{"code":0,"data":{"answer":"查询次数不足","times":0},"message":"次数不足"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"data":{"answer":"答案B","times":10},"message":""}`))
	}))
	defer srv.Close()

	y := newYanxi(map[string]string{"tokens": "tok1,tok2"}, nil)
	y.api = srv.URL
	y.cache = tmpCache(t)

	ans, ok := y.Query(Question{Title: "题干", Type: "single"})
	if !ok || ans != "答案B" {
		t.Fatalf("token 轮换后应命中: %q %v", ans, ok)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "tok1" || seen[1] != "tok2" {
		t.Fatalf("token 轮换顺序错误: %#v", seen)
	}
}

func TestTikuYanxiNoTokenDisabled(t *testing.T) {
	y := newYanxi(map[string]string{}, nil)
	if !y.Disabled() {
		t.Fatal("未配置 token 时应停用")
	}
}

// ---------------- TikuGo ----------------

func TestTikuGoRequest(t *testing.T) {
	cap := &reqCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"code":1,"data":"答案A","msg":""}`))
	}))
	defer srv.Close()

	g := newGoTiku(map[string]string{
		"go_authorization": "AUTH-TOKEN",
		"go_min_interval":  "0",
		"go_retry_backoff": "0",
		"go_retry_times":   "3",
	}, nil)
	g.api = srv.URL
	g.cache = tmpCache(t)

	ans, ok := g.Query(Question{Title: "题干", Type: "completion"})
	if !ok || ans != "答案A" {
		t.Fatalf("查询失败: %q %v", ans, ok)
	}
	if cap.Method != http.MethodPost {
		t.Errorf("方法应为 POST, 得到 %s", cap.Method)
	}
	if got := cap.Header.Get("Authorization"); got != "AUTH-TOKEN" {
		t.Errorf("Authorization 头错误: %q", got)
	}
	if got := cap.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type 头错误: %q", got)
	}
	if got := cap.form(t).Get("question"); got != "题干" {
		t.Errorf("question 表单字段错误: %q", got)
	}
	if g.minInterval != 0 || g.retryBackoff != 0 || g.retryTimes != 3 {
		t.Errorf("go_* 配置解析错误: %v %v %d", g.minInterval, g.retryBackoff, g.retryTimes)
	}
}

func TestTikuGoRetryOnThrottle(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n < 3 {
			_, _ = w.Write([]byte(`{"code":0,"data":"","msg":"速度太快，触发流控限制"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"data":"答案C","msg":""}`))
	}))
	defer srv.Close()

	g := newGoTiku(map[string]string{"go_min_interval": "0", "go_retry_backoff": "0", "go_retry_times": "3"}, nil)
	g.api = srv.URL
	g.cache = tmpCache(t)

	ans, ok := g.Query(Question{Title: "题干", Type: "completion"})
	if !ok || ans != "答案C" {
		t.Fatalf("流控重试后应命中: %q %v", ans, ok)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Fatalf("重试次数错误, 期望 3 次请求, 实际 %d", calls)
	}
}

func TestTikuGoRetryExhausted(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"code":0,"data":"","msg":"并发限制"}`))
	}))
	defer srv.Close()

	g := newGoTiku(map[string]string{"go_min_interval": "0", "go_retry_backoff": "0", "go_retry_times": "3"}, nil)
	g.api = srv.URL
	g.cache = tmpCache(t)

	if _, ok := g.Query(Question{Title: "题干", Type: "completion"}); ok {
		t.Fatal("流控耗尽重试后不应命中")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Fatalf("重试耗尽应恰好请求 3 次, 实际 %d", calls)
	}
}

func TestTikuGoPlaceholderFiltered(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"code":1,"data":"李恒雅正在努力撰写中...","msg":""}`))
	}))
	defer srv.Close()

	g := newGoTiku(map[string]string{"go_min_interval": "0", "go_retry_backoff": "0", "go_retry_times": "3"}, nil)
	g.api = srv.URL
	g.cache = tmpCache(t)

	if _, ok := g.Query(Question{Title: "题干", Type: "completion"}); ok {
		t.Fatal("占位答案应被过滤")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("占位答案不应触发重试, 实际请求 %d 次", calls)
	}
}

func TestTikuGoCandidateTitles(t *testing.T) {
	var mu sync.Mutex
	var questions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		q := r.PostForm.Get("question")
		mu.Lock()
		questions = append(questions, q)
		mu.Unlock()
		if strings.HasPrefix(q, "【") {
			_, _ = w.Write([]byte(`{"code":1,"data":"","msg":"未找到"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":1,"data":"答案D","msg":""}`))
	}))
	defer srv.Close()

	g := newGoTiku(map[string]string{"go_min_interval": "0", "go_retry_backoff": "0", "go_retry_times": "1"}, nil)
	g.api = srv.URL
	g.cache = tmpCache(t)

	ans, ok := g.Query(Question{Title: "【单选题】题干内容", Type: "completion"})
	if !ok || ans != "答案D" {
		t.Fatalf("去前缀候选应命中: %q %v", ans, ok)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(questions) != 2 {
		t.Fatalf("应尝试两个候选标题, 实际 %#v", questions)
	}
	if questions[1] != "题干内容" {
		t.Fatalf("第二个候选应为去前缀后的标题, 实际 %q", questions[1])
	}
}

// ---------------- TikuAdapter ----------------

func TestTikuAdapterRequest(t *testing.T) {
	cap := &reqCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		_, _ = w.Write([]byte(`{"answer":{"bestAnswer":["选项一","选项二"]}}`))
	}))
	defer srv.Close()

	a := newAdapter(map[string]string{"url": srv.URL}, nil)
	a.cache = tmpCache(t)

	ans, ok := a.Query(Question{Title: "题干", Options: "A. 选项一\nB、选项二", Type: "multiple"})
	if !ok || ans != "选项一\n选项二" {
		t.Fatalf("查询失败: %q %v", ans, ok)
	}
	if cap.Method != http.MethodPost {
		t.Errorf("方法应为 POST, 得到 %s", cap.Method)
	}
	body := cap.jsonBody(t)
	if body["question"] != "题干" {
		t.Errorf("question 字段错误: %v", body["question"])
	}
	if got, _ := body["type"].(float64); got != 1 {
		t.Errorf("多选题 type 应为 1, 得到 %v", body["type"])
	}
	opts, _ := body["options"].([]any)
	if len(opts) != 2 || opts[0] != "选项一" || opts[1] != "选项二" {
		t.Errorf("选项前缀未去除: %#v", opts)
	}
}

func TestTikuAdapterMiss(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answer":{"bestAnswer":[]}}`))
	}))
	defer srv.Close()

	a := newAdapter(map[string]string{"url": srv.URL}, nil)
	a.cache = tmpCache(t)
	if _, ok := a.Query(Question{Title: "题干", Type: "single"}); ok {
		t.Fatal("空 bestAnswer 不应命中")
	}
}

func TestTikuAdapterNoURLDisabled(t *testing.T) {
	a := newAdapter(map[string]string{}, nil)
	if !a.Disabled() {
		t.Fatal("未配置 url 时应停用")
	}
}

// ---------------- AI ----------------

func aiServer(t *testing.T, content string, cap *reqCapture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cap != nil {
			cap.record(r)
		}
		resp := map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"content": content}},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
	}))
}

func TestAIRequest(t *testing.T) {
	cap := &reqCapture{}
	srv := aiServer(t, `{"Answer": ["选项一"]}`, cap)
	defer srv.Close()

	a := newAI(map[string]string{
		"endpoint":             srv.URL,
		"key":                  "test-key",
		"model":                "m1",
		"min_interval_seconds": "0",
	}, nil)
	a.cache = tmpCache(t)

	ans, ok := a.Query(Question{Title: "题干", Options: "A 选项一\nB 选项二", Type: "single"})
	if !ok || ans != "选项一" {
		t.Fatalf("查询失败: %q %v", ans, ok)
	}
	if cap.Path != "/v1/chat/completions" {
		t.Errorf("endpoint 未自动补全 /v1/chat/completions: %s", cap.Path)
	}
	if cap.Method != http.MethodPost {
		t.Errorf("方法应为 POST, 得到 %s", cap.Method)
	}
	if got := cap.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization 头错误: %q", got)
	}
	body := cap.jsonBody(t)
	if body["model"] != "m1" {
		t.Errorf("model 字段错误: %v", body["model"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages 数量错误: %d", len(msgs))
	}
	m0, _ := msgs[0].(map[string]any)
	if m0["role"] != "system" {
		t.Errorf("首条消息应为 system: %#v", m0)
	}
	m1, _ := msgs[1].(map[string]any)
	if m1["role"] != "user" {
		t.Errorf("末条消息应为 user: %#v", m1)
	}
	userContent, _ := m1["content"].(string)
	if !strings.Contains(userContent, "题目：题干") {
		t.Errorf("user 消息应含题目: %q", userContent)
	}
	if !strings.Contains(userContent, "选项一") || strings.Contains(userContent, "A 选项一") {
		t.Errorf("选项字母未去除: %q", userContent)
	}
}

func TestAIEndpointVariants(t *testing.T) {
	for _, ep := range []string{"", "/v1", "/v1/chat/completions"} {
		t.Run("suffix="+ep, func(t *testing.T) {
			cap := &reqCapture{}
			srv := aiServer(t, `{"Answer": ["正确"]}`, cap)
			defer srv.Close()

			a := newAI(map[string]string{
				"endpoint":             srv.URL + ep,
				"key":                  "k",
				"model":                "m",
				"min_interval_seconds": "0",
			}, nil)
			a.cache = tmpCache(t)
			if _, ok := a.Query(Question{Title: "题干", Type: "judgement"}); !ok {
				t.Fatal("查询应命中")
			}
			if cap.Path != "/v1/chat/completions" {
				t.Fatalf("endpoint %q 解析错误: %s", ep, cap.Path)
			}
		})
	}
}

func TestAIWorkFeedback(t *testing.T) {
	cap := &reqCapture{}
	srv := aiServer(t, `{"Answer": ["正确"]}`, cap)
	defer srv.Close()

	a := newAI(map[string]string{
		"endpoint":             srv.URL,
		"key":                  "k",
		"model":                "m",
		"min_interval_seconds": "0",
	}, nil)
	a.cache = tmpCache(t)
	a.SetWorkFeedback([]string{"第1题：你的答案错误，正确答案是 正确"})

	if _, ok := a.Query(Question{Title: "题干", Type: "judgement"}); !ok {
		t.Fatal("查询应命中")
	}
	body := cap.jsonBody(t)
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("带反馈时应为 3 条消息, 实际 %d", len(msgs))
	}
	m1, _ := msgs[1].(map[string]any)
	if m1["role"] != "system" {
		t.Errorf("反馈消息应为 system: %#v", m1)
	}
	fb, _ := m1["content"].(string)
	if !strings.Contains(fb, "你上一次作答的章节检测中有以下题目回答错误") {
		t.Errorf("反馈提示词缺失: %q", fb)
	}
	if !strings.Contains(fb, "正确答案是 正确") {
		t.Errorf("反馈内容缺失: %q", fb)
	}
}

func TestAIMarkdownWrapperAndBadOutput(t *testing.T) {
	cap := &reqCapture{}
	srv := aiServer(t, "```json\n{\"Answer\": [\"正确\"]}\n```", cap)
	defer srv.Close()

	a := newAI(map[string]string{
		"endpoint":             srv.URL,
		"key":                  "k",
		"model":                "m",
		"min_interval_seconds": "0",
	}, nil)
	a.cache = tmpCache(t)
	if ans, ok := a.Query(Question{Title: "题干", Type: "judgement"}); !ok || ans != "正确" {
		t.Fatalf("Markdown 包裹解析失败: %q %v", ans, ok)
	}

	// 无法解析的输出
	srv2 := aiServer(t, "这不是JSON", nil)
	defer srv2.Close()
	a2 := newAI(map[string]string{
		"endpoint":             srv2.URL,
		"key":                  "k",
		"model":                "m",
		"min_interval_seconds": "0",
	}, nil)
	a2.cache = tmpCache(t)
	if _, ok := a2.Query(Question{Title: "题干", Type: "judgement"}); ok {
		t.Fatal("无法解析的输出不应命中")
	}
}

// ---------------- SiliconFlow ----------------

func TestSiliconFlowRequest(t *testing.T) {
	cap := &reqCapture{}
	srv := aiServer(t, `{"Answer": ["正确"]}`, cap)
	defer srv.Close()

	s := newSiliconFlow(map[string]string{
		"siliconflow_key":      "sk-test",
		"siliconflow_model":    "deepseek-ai/DeepSeek-R1",
		"siliconflow_endpoint": srv.URL,
		"min_interval_seconds": "0",
	}, nil)
	s.cache = tmpCache(t)

	ans, ok := s.Query(Question{Title: "题干", Options: "A. 对\nB. 错", Type: "judgement"})
	if !ok || ans != "正确" {
		t.Fatalf("查询失败: %q %v", ans, ok)
	}
	if got := cap.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization 头错误: %q", got)
	}
	if got := cap.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type 头错误: %q", got)
	}
	body := cap.jsonBody(t)
	if body["model"] != "deepseek-ai/DeepSeek-R1" {
		t.Errorf("model 字段错误: %v", body["model"])
	}
	if body["stream"] != false {
		t.Errorf("stream 应为 false: %v", body["stream"])
	}
	if v, _ := body["max_tokens"].(float64); v != 4096 {
		t.Errorf("max_tokens 应为 4096: %v", body["max_tokens"])
	}
	if v, _ := body["temperature"].(float64); v != 0.7 {
		t.Errorf("temperature 应为 0.7: %v", body["temperature"])
	}
	if v, _ := body["top_p"].(float64); v != 0.7 {
		t.Errorf("top_p 应为 0.7: %v", body["top_p"])
	}
	rf, _ := body["response_format"].(map[string]any)
	if rf["type"] != "text" {
		t.Errorf("response_format.type 应为 text: %#v", body["response_format"])
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages 数量错误: %d", len(msgs))
	}
}

func TestSiliconFlowDefaultsAndDisabled(t *testing.T) {
	s := newSiliconFlow(map[string]string{"siliconflow_key": "sk"}, nil)
	if s.apiEndpoint != siliconflowDefaultEndpoint {
		t.Errorf("默认 endpoint 错误: %s", s.apiEndpoint)
	}
	if s.modelName != siliconflowDefaultModel {
		t.Errorf("默认 model 错误: %s", s.modelName)
	}
	if s.Disabled() {
		t.Error("配置 key 后不应停用")
	}

	noKey := newSiliconFlow(map[string]string{}, nil)
	if !noKey.Disabled() {
		t.Error("未配置 key 时应停用")
	}
}

// ---------------- 通用：QueryAll 等长 ----------------

func TestQueryAllLengthMatches(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1,"data":"答案X","msg":""}`))
	}))
	defer srv.Close()

	g := newGoTiku(map[string]string{"go_min_interval": "0", "go_retry_backoff": "0"}, nil)
	g.api = srv.URL
	g.cache = tmpCache(t)

	qs := []Question{
		{Title: "题1", Type: "completion"},
		{Title: "题2", Type: "completion"},
		{Title: "题3", Type: "completion"},
	}
	res := g.QueryAll(qs, 0)
	if len(res) != len(qs) {
		t.Fatalf("返回长度应与输入一致: %d vs %d", len(res), len(qs))
	}
	for i, a := range res {
		if a != "答案X" {
			t.Errorf("第 %d 题答案错误: %q", i, a)
		}
	}
}

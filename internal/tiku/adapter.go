package tiku

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// TikuAdapter 自建题库适配器（对应 Python 的 TikuAdapter），接口地址取自配置项 url。
type TikuAdapter struct {
	*baseProvider
	api    string
	client *http.Client
}

// adapterResp TikuAdapter 响应体。
type adapterResp struct {
	Answer struct {
		BestAnswer []string `json:"bestAnswer"`
	} `json:"answer"`
}

// 选项前缀（如 "A. "、"B、"）正则，对齐 Python 的 r'^[A-Za-z]\.?、?\s?'。
var reAdapterOptionPrefix = regexp.MustCompile(`^[A-Za-z]\.?、?\s?`)

// newAdapter 构造 TikuAdapter 题库；url 为空时停用。
func newAdapter(conf map[string]string, log logger) *TikuAdapter {
	b := newBase("TikuAdapter题库", conf, log)
	a := &TikuAdapter{
		baseProvider: b,
		api:          strings.TrimSpace(conf["url"]),
		client:       newHTTPClient(30 * time.Second),
	}
	if a.api == "" {
		b.log.Error("%s未配置 url, 已停用", b.name)
		b.disabled = true
	}
	b.query = a.queryOne
	return a
}

func (a *TikuAdapter) queryOne(q Question) (string, bool) {
	// 题型映射：single=0 multiple=1 completion=2 judgement=3 其他=4
	var typeCode int
	switch q.Type {
	case "single":
		typeCode = 0
	case "multiple":
		typeCode = 1
	case "completion":
		typeCode = 2
	case "judgement":
		typeCode = 3
	default:
		typeCode = 4
	}

	options := make([]string, 0, 4)
	for _, opt := range strings.Split(q.Options, "\n") {
		options = append(options, reAdapterOptionPrefix.ReplaceAllString(opt, ""))
	}

	payload, err := json.Marshal(map[string]any{
		"question": q.Title,
		"options":  options,
		"type":     typeCode,
	})
	if err != nil {
		a.log.Error("%s请求构造失败: %v", a.name, err)
		return "", false
	}

	res, err := a.client.Post(a.api, "application/json", bytes.NewReader(payload))
	if err != nil {
		a.log.Error("%s查询失败: %v", a.name, err)
		return "", false
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", false
	}
	var r adapterResp
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		a.log.Error("%s查询失败: 返回内容不是有效JSON: %v", a.name, err)
		return "", false
	}
	if len(r.Answer.BestAnswer) == 0 {
		a.log.Error("%s查询失败, 未返回答案", a.name)
		return "", false
	}
	ans := strings.TrimSpace(strings.Join(r.Answer.BestAnswer, "\n"))
	if ans == "" {
		return "", false
	}
	return ans, true
}

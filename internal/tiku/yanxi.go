package tiku

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// yanxiAPI 言溪题库查询接口。
const yanxiAPI = "https://tk.enncy.cn/query"

// TikuYanxi 言溪题库实现（对应 Python 的 TikuYanxi）。
//
// GET https://tk.enncy.cn/query?question=<urlencoded>&token=<token>；
// tokens 支持英文逗号分隔，查询次数用尽时按顺序轮换。
type TikuYanxi struct {
	*baseProvider
	api        string
	client     *http.Client
	tokens     []string
	tokenIndex int
	token      string
	times      int // 剩余查询次数（初始 100，查询后按响应校正）
}

// yanxiResp 言溪接口响应体。
type yanxiResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Answer string `json:"answer"`
		Times  *int   `json:"times"`
	} `json:"data"`
}

// newYanxi 构造言溪题库；未配置 token 时停用。
func newYanxi(conf map[string]string, log logger) *TikuYanxi {
	b := newBase("言溪题库", conf, log)
	y := &TikuYanxi{
		baseProvider: b,
		api:          yanxiAPI,
		client:       newHTTPClient(30 * time.Second),
		times:        100,
		tokens:       splitList(conf["tokens"]),
	}
	if err := y.loadToken(); err != nil {
		b.disabled = true
	}
	b.query = y.queryOne
	return y
}

// loadToken 载入当前 token（对应 Python 的 load_token）。
func (y *TikuYanxi) loadToken() error {
	if y.tokenIndex >= len(y.tokens) {
		y.log.Error("TOKEN用完, 请自行更换再重启脚本")
		return fmt.Errorf("%s TOKEN 已用完, 请更换", y.name)
	}
	y.token = y.tokens[y.tokenIndex]
	return nil
}

func (y *TikuYanxi) queryOne(q Question) (string, bool) {
	return y.queryTitle(q.Title)
}

func (y *TikuYanxi) queryTitle(title string) (string, bool) {
	u, err := url.Parse(y.api)
	if err != nil {
		y.log.Error("%s查询失败: %v", y.name, err)
		return "", false
	}
	params := url.Values{}
	params.Set("question", title)
	params.Set("token", y.token)
	u.RawQuery = params.Encode()

	res, err := y.client.Get(u.String())
	if err != nil {
		y.log.Error("%s查询失败: %v", y.name, err)
		return "", false
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		y.log.Error("%s查询失败: 状态码 %d", y.name, res.StatusCode)
		return "", false
	}

	var r yanxiResp
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		y.log.Error("%s查询失败: 返回内容不是有效JSON: %v", y.name, err)
		return "", false
	}

	if r.Code == 0 {
		// 查询次数到期则轮换 token 并重新搜题
		if y.times == 0 || strings.Contains(r.Data.Answer, "次数不足") {
			y.log.Info("TOKEN查询次数不足, 将会更换并重新搜题")
			y.tokenIndex++
			if err := y.loadToken(); err != nil {
				y.disabled = true
				return "", false
			}
			return y.queryTitle(title)
		}
		times := fmt.Sprintf("%d(仅参考)", y.times)
		if r.Data.Times != nil {
			times = fmt.Sprintf("%d", *r.Data.Times)
		}
		y.log.Error("%s查询失败: 剩余查询数%s: 消息:%s", y.name, times, r.Message)
		return "", false
	}

	if r.Data.Times != nil {
		y.times = *r.Data.Times
	}
	ans := strings.TrimSpace(r.Data.Answer)
	if ans == "" {
		return "", false
	}
	return ans, true
}

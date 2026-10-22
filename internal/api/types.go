// Package api 超星学习通客户端，对应 Python 原版 /root/chaoxing-ref/api/base.py。
//
// 本文件定义跨包契约（类型 + Client 结构体 + 构造函数）。
// 所有 HTTP 方法实现分布在 session.go / login.go / course.go / video.go / work.go / misc.go，
// 答案后处理工具函数在 helpers.go。
package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/butlanys/chaoxing-go/internal/consts"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
	"github.com/butlanys/chaoxing-go/internal/logx"
	"github.com/butlanys/chaoxing-go/internal/tiku"
)

// Account 账号
type Account struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Remark   string `json:"remark"`
}

// Label 返回账号标识（用于日志/进度展示）
func (a Account) Label() string {
	if a.Remark != "" {
		return a.Remark
	}
	return a.Username
}

// StudyResult 任务点学习结果
type StudyResult int

// 结果枚举，与 Python 版 StudyResult 一致
const (
	ResultSuccess StudyResult = iota
	ResultForbidden
	ResultError
	ResultTimeout
)

// IsSuccess 是否成功
func (r StudyResult) IsSuccess() bool { return r == ResultSuccess }

// IsFailure 是否失败
func (r StudyResult) IsFailure() bool { return r != ResultSuccess }

// String 便于日志输出
func (r StudyResult) String() string {
	switch r {
	case ResultSuccess:
		return "success"
	case ResultForbidden:
		return "forbidden"
	case ResultError:
		return "error"
	case ResultTimeout:
		return "timeout"
	}
	return "unknown"
}

// LoginResult 登录结果
type LoginResult struct {
	Status   bool   `json:"status"`
	Msg      string `json:"msg"`
	RealName string `json:"realName"`
}

// Options 客户端选项
type Options struct {
	QueryDelay      float64       // 题库查询间隔（秒）
	WorkMaxRetries  int           // 章节检测最大重做次数
	WorkRedoEnabled bool          // 是否允许自动重做
	Speed           float64       // 视频倍速（上限 2.0）
	CookieFile      string        // 每账号独立 cookie 文件路径
	Timeout         time.Duration // 单请求超时，默认 5s
	Proxy           string        // 可选 HTTP 代理
	NotOpenAction   string        // retry|ask|continue
	// AskFunc "ask" 模式回调；返回 false 表示停止，nil 时按 retry 处理
	AskFunc func(prompt string) bool
}

// RateLimiter 限速器（对应 Python RateLimiter）
type RateLimiter struct {
	mu           sync.Mutex
	lastCall     time.Time
	callInterval float64
}

// NewRateLimiter 构造限速器
func NewRateLimiter(interval float64) *RateLimiter {
	return &RateLimiter{lastCall: time.Now(), callInterval: interval}
}

// Limit 阻塞到满足最小请求间隔；randomTime 为 true 时额外随机等待 [randomMin, randomMax) 秒
func (r *RateLimiter) Limit(randomTime bool, randomMin, randomMax float64) {
	r.mu.Lock()
	now := time.Now()
	baseWait := r.lastCall.Add(time.Duration(r.callInterval * float64(time.Second))).Sub(now)
	if baseWait < 0 {
		baseWait = 0
	}
	extra := time.Duration(0)
	if randomTime && randomMax > randomMin {
		extra = time.Duration(randFloat(randomMin, randomMax) * float64(time.Second))
	}
	wait := baseWait + extra
	r.lastCall = now.Add(wait)
	r.mu.Unlock()
	if wait > 0 {
		time.Sleep(wait)
	}
}

// Client 超星客户端。每个账号一个实例，内部持有独立 cookie jar，可并发使用。
type Client struct {
	account Account
	tiku    tiku.Provider
	log     *logx.Logger
	opt     Options

	http    *http.Client
	jar     http.CookieJar
	rl      *RateLimiter // 普通接口限速（0.5s）
	videoRL *RateLimiter // 视频进度上报限速（2s）

	mu       sync.RWMutex
	uid      string
	fid      string
	rollback int // 章节检测回滚次数（对齐 Python self.rollback_times）
}

// NewClient 构造客户端。tiku 可为 nil（此时章节检测直接跳过）。
func NewClient(acc Account, t tiku.Provider, log *logx.Logger, opt Options) *Client {
	if log == nil {
		log = logx.New(acc.Label())
	}
	if opt.Timeout <= 0 {
		opt.Timeout = consts.DefaultTimeout
	}
	if opt.Speed <= 0 {
		opt.Speed = 1.0
	}
	if opt.Speed > 2.0 {
		opt.Speed = 2.0
	}
	if opt.NotOpenAction == "" {
		opt.NotOpenAction = "retry"
	}
	jar := newFlatJar()
	return &Client{
		account: acc,
		tiku:    t,
		log:     log,
		opt:     opt,
		jar:     jar,
		http:    newHTTPClient(jar, opt),
		rl:      NewRateLimiter(consts.RateInterval),
		videoRL: NewRateLimiter(consts.VideoLogInterval),
	}
}

// Log 返回带账号标签的日志器
func (c *Client) Log() *logx.Logger { return c.log }

// Account 返回账号信息
func (c *Client) Account() Account { return c.account }

// Options 返回客户端选项
func (c *Client) Options() Options { return c.opt }

// Activity 签到活动
type Activity struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    int    `json:"status"`
	Type      int    `json:"type"`
	OtherID   string `json:"otherId"`
	StartTime string `json:"startTime"`
}

// CourseRef 便捷构造：从课程取 (courseId, clazzId, cpi)
func CourseRef(c htmlx.Course) (string, string, string) {
	return c.CourseID, c.ClazzID, c.CPI
}

// RateLimit 遵守最小请求间隔（对齐 Python chaoxing.rate_limiter.limit_rate()）
func (c *Client) RateLimit() { c.rl.Limit(false, 0, 0) }

// RateLimitRandom 在最小间隔之上再随机等待 [min, max) 秒
func (c *Client) RateLimitRandom(min, max float64) { c.rl.Limit(true, min, max) }

// Package tiku 题库（搜题）子系统，对应 Python 原版 /root/chaoxing-ref/api/answer.py。
package tiku

// Question 传给题库的题目（与 htmlx.Question 解耦，由 engine/api 负责转换）
type Question struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Options string `json:"options"`
	Type    string `json:"type"` // single|multiple|completion|judgement|shortanswer
}

// Provider 题库接口
type Provider interface {
	// Name 题库名（如 TikuYanxi）
	Name() string
	// Query 查询单题，第二个返回值为是否命中
	Query(q Question) (string, bool)
	// QueryAll 批量查询，返回与输入等长的切片，未命中填 ""
	QueryAll(qs []Question, delay float64) []string
	// SubmitMode 返回 "" 表示提交，返回 "1" 表示仅保存不提交
	SubmitMode() string
	// CoverRate 最低题库覆盖率
	CoverRate() float64
	// JudgementSelect 判断题答案 -> 是否为"正确"
	JudgementSelect(ans string) bool
	// SetWorkFeedback 设置上一轮章节检测的错误反馈（仅 AI 类题库使用）
	SetWorkFeedback(feedback []string)
	// Disabled 未配置/已停用
	Disabled() bool
	// IsManual 是否为手动输入模式（手动模式下引擎跳过答案校验且始终提交）
	IsManual() bool
	// TrueList 判断题"正确"同义词表
	TrueList() []string
	// FalseList 判断题"错误"同义词表
	FalseList() []string
}

// ManualHandler 手动模式回调，由 GUI/CLI 注入；返回与题目等长的答案切片
type ManualHandler func(qs []Question) []string

// ManualHook 全局手动答题回调（GUI/CLI 在启动时设置）
var ManualHook ManualHandler

// New（根据 [tiku] 配置构造题库，provider 支持英文逗号分隔的多题库回退）的实现见 provider.go：
//
//	func New(conf map[string]string, log logger) (Provider, error)
//
// logger 为最小日志接口，*logx.Logger 天然满足。

// logger 最小日志接口，避免与 logx 包强耦合
type logger interface {
	Trace(format string, a ...any)
	Debug(format string, a ...any)
	Info(format string, a ...any)
	Warn(format string, a ...any)
	Error(format string, a ...any)
	Success(format string, a ...any)
}

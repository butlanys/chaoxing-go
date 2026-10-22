package tiku

import (
	"strings"
	"sync"
)

// TikuManual 手动输入题库（对应 Python 的 TikuManual）。
//
// GUI/CLI 通过设置包级变量 ManualHook 注入交互逻辑；QueryAll 直接调用该回调。
// IsManual()==true，因此引擎会跳过答案校验并始终提交。
type TikuManual struct {
	*baseProvider
	warnOnce sync.Once
}

// newManual 构造手动题库。
func newManual(conf map[string]string, log logger) *TikuManual {
	b := newBase("手动输入题库", conf, log)
	return &TikuManual{baseProvider: b}
}

// IsManual 手动模式恒为 true。
func (m *TikuManual) IsManual() bool { return true }

// skipAnswerValidation 手动模式跳过答案校验。
func (m *TikuManual) skipAnswerValidation() bool { return true }

// QueryAll 调用 ManualHook 获取答案；未注入回调时返回全空并仅告警一次。
func (m *TikuManual) QueryAll(qs []Question, delay float64) []string {
	results := make([]string, len(qs))
	if ManualHook == nil {
		m.warnOnce.Do(func() {
			m.log.Warn("手动模式已启用但未注入答题回调(ManualHook)，本次返回空答案")
		})
		return results
	}
	ans := ManualHook(qs)
	for i := range results {
		if i < len(ans) {
			results[i] = strings.TrimSpace(ans[i])
		}
	}
	return results
}

// Query 单题查询（内部复用 QueryAll）。
func (m *TikuManual) Query(q Question) (string, bool) {
	all := m.QueryAll([]Question{q}, 0)
	if len(all) > 0 && strings.TrimSpace(all[0]) != "" {
		return strings.TrimSpace(all[0]), true
	}
	return "", false
}

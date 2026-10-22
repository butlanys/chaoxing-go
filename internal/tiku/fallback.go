package tiku

import "strings"

// FallbackProvider 多题库回退（对应 Python 的 TikuFallback）：
// 按 provider 配置顺序依次查询，跳过已命中的题目。
type FallbackProvider struct {
	*baseProvider
	providers []Provider
}

// newFallback 构造多题库回退，自动剔除已停用的子题库。
func newFallback(conf map[string]string, log logger, providers []Provider) *FallbackProvider {
	b := newBase("多题库回退", conf, log)
	active := make([]Provider, 0, len(providers))
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		if p == nil || p.Disabled() {
			continue
		}
		active = append(active, p)
		names = append(names, p.Name())
	}
	if len(active) == 0 {
		b.log.Error("多题库回退初始化失败: 没有可用题库")
		b.disabled = true
	} else {
		b.log.Info("多题库回退已启用，查询顺序: %s", strings.Join(names, ", "))
	}
	return &FallbackProvider{baseProvider: b, providers: active}
}

// skipAnswerValidation 回退包装器豁免二次校验（其子题库在各自环节均已校验过）。
func (f *FallbackProvider) skipAnswerValidation() bool { return true }

// Disabled 当所有子题库都停用（或没有子题库）时视为停用。
func (f *FallbackProvider) Disabled() bool {
	if len(f.providers) == 0 {
		return true
	}
	for _, p := range f.providers {
		if !p.Disabled() {
			return false
		}
	}
	return true
}

// IsManual 任一子题库为手动模式时整体视为手动模式。
func (f *FallbackProvider) IsManual() bool {
	for _, p := range f.providers {
		if p.IsManual() {
			return true
		}
	}
	return false
}

// SetWorkFeedback 将章节检测错误反馈透传给支持反馈的子题库。
func (f *FallbackProvider) SetWorkFeedback(feedback []string) {
	f.baseProvider.feedback = feedback
	for _, p := range f.providers {
		p.SetWorkFeedback(feedback)
	}
}

// Query 顺序调用子题库，命中即返回。
func (f *FallbackProvider) Query(q Question) (string, bool) {
	if f.Disabled() {
		return "", false
	}
	for _, p := range f.providers {
		if p.Disabled() {
			continue
		}
		if ans, ok := p.Query(q); ok && strings.TrimSpace(ans) != "" {
			return strings.TrimSpace(ans), true
		}
		f.log.Info("%s 未命中，回退到下一个题库", p.Name())
	}
	return "", false
}

// QueryAll 按顺序回退批量查询：先过缓存，剩余题目依次交给各子题库，
// 已命中的题目不再传给后续题库。
func (f *FallbackProvider) QueryAll(qs []Question, delay float64) []string {
	results := make([]string, len(qs))
	if f.Disabled() {
		return results
	}

	// 标题预处理（不改动入参切片）+ 缓存命中
	titles := make([]string, len(qs))
	skipCache := f.hasFeedback()
	pending := make([]int, 0, len(qs))
	for i := range qs {
		titles[i] = normalizeTitle(qs[i].Title)
		if !skipCache {
			if ans, ok := f.cache.GetCache(titles[i]); ok && strings.TrimSpace(ans) != "" {
				results[i] = strings.TrimSpace(ans)
				continue
			}
		}
		pending = append(pending, i)
	}

	for _, p := range f.providers {
		if len(pending) == 0 {
			break
		}
		if p.Disabled() {
			continue
		}
		sub := make([]Question, len(pending))
		for j, idx := range pending {
			sub[j] = qs[idx]
		}
		subRes := p.QueryAll(sub, delay)
		if len(subRes) != len(pending) {
			// 长度不符，跳过该题库以防答案错位
			f.log.Error("%s 批量查询返回结果长度不匹配，跳过该题库", p.Name())
			continue
		}
		next := make([]int, 0, len(pending))
		for j, idx := range pending {
			if ans := strings.TrimSpace(subRes[j]); ans != "" {
				results[idx] = ans
			} else {
				next = append(next, idx)
			}
		}
		pending = next
	}

	// 命中结果写入缓存
	for i, ans := range results {
		if ans != "" {
			_ = f.cache.AddCache(titles[i], ans)
		}
	}
	return results
}

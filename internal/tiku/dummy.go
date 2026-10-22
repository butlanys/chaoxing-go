package tiku

// DummyProvider 未配置题库时的空实现（对应 Python 的 DummyTiku）：Disabled()==true，
// 所有查询均返回未命中。
type DummyProvider struct {
	*baseProvider
}

// newDummy 构造空题库。
func newDummy(conf map[string]string, log logger) *DummyProvider {
	b := newBase("空/禁用题库", conf, log)
	b.disabled = true
	return &DummyProvider{baseProvider: b}
}

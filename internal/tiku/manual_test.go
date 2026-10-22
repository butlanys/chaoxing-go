package tiku

import "testing"

func TestManualWarnOnceWhenNoHook(t *testing.T) {
	ManualHook = nil
	defer func() { ManualHook = nil }()

	log := &testLogger{}
	m := newManual(map[string]string{}, log)
	if !m.IsManual() {
		t.Fatal("手动题库 IsManual 应为 true")
	}

	qs := []Question{{Title: "题1"}, {Title: "题2"}}
	for i := 0; i < 3; i++ {
		res := m.QueryAll(qs, 0)
		if len(res) != 2 || res[0] != "" || res[1] != "" {
			t.Fatalf("未注入回调应返回等长全空: %#v", res)
		}
	}
	if n := log.count("未注入答题回调"); n != 1 {
		t.Fatalf("未注入回调的告警应只出现一次, 实际 %d 次", n)
	}
}

func TestManualWithHook(t *testing.T) {
	ManualHook = func(qs []Question) []string {
		out := make([]string, len(qs))
		for i := range out {
			out[i] = "答案"
		}
		return out
	}
	defer func() { ManualHook = nil }()

	m := newManual(map[string]string{}, nil)
	res := m.QueryAll([]Question{{Title: "题1"}, {Title: "题2"}}, 0)
	if len(res) != 2 || res[0] != "答案" || res[1] != "答案" {
		t.Fatalf("手动回调结果错误: %#v", res)
	}
	if ans, ok := m.Query(Question{Title: "题"}); !ok || ans != "答案" {
		t.Fatalf("手动单题查询错误: %q %v", ans, ok)
	}
}

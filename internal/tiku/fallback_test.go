package tiku

import (
	"strings"
	"sync"
	"testing"
)

// newFakeProvider 构造一个可控查询行为的假题库（走真实 base 的缓存+校验逻辑）。
func newFakeProvider(t *testing.T, name string, fn func(q Question) (string, bool)) *baseProvider {
	t.Helper()
	b := newBase(name, map[string]string{}, nil)
	b.cache = tmpCache(t)
	b.query = fn
	return b
}

func TestFallbackFirstMissSecondHit(t *testing.T) {
	p1 := newFakeProvider(t, "题库一", func(q Question) (string, bool) { return "", false })
	p2 := newFakeProvider(t, "题库二", func(q Question) (string, bool) { return "答案A", true })

	fb := newFallback(map[string]string{}, nil, []Provider{p1, p2})
	fb.cache = tmpCache(t)

	res := fb.QueryAll([]Question{{Title: "题1", Type: "completion"}}, 0)
	if len(res) != 1 || res[0] != "答案A" {
		t.Fatalf("回退结果错误: %#v", res)
	}

	if ans, ok := fb.Query(Question{Title: "题2", Type: "completion"}); !ok || ans != "答案A" {
		t.Fatalf("单题回退错误: %q %v", ans, ok)
	}
}

func TestFallbackAllMiss(t *testing.T) {
	p1 := newFakeProvider(t, "题库一", func(q Question) (string, bool) { return "", false })
	p2 := newFakeProvider(t, "题库二", func(q Question) (string, bool) { return "", false })

	fb := newFallback(map[string]string{}, nil, []Provider{p1, p2})
	fb.cache = tmpCache(t)

	res := fb.QueryAll([]Question{{Title: "题1", Type: "completion"}, {Title: "题2", Type: "completion"}}, 0)
	if len(res) != 2 || res[0] != "" || res[1] != "" {
		t.Fatalf("全部未命中应返回全空: %#v", res)
	}
	if _, ok := fb.Query(Question{Title: "题1", Type: "completion"}); ok {
		t.Fatal("全部未命中不应有命中")
	}
}

func TestFallbackSkipsHitQuestions(t *testing.T) {
	var mu sync.Mutex
	var p2Questions []string

	p1 := newFakeProvider(t, "题库一", func(q Question) (string, bool) {
		if q.Title == "题1" {
			return "答案一", true
		}
		return "", false
	})
	p2 := newFakeProvider(t, "题库二", func(q Question) (string, bool) {
		mu.Lock()
		p2Questions = append(p2Questions, q.Title)
		mu.Unlock()
		return "答案二", true
	})

	fb := newFallback(map[string]string{}, nil, []Provider{p1, p2})
	fb.cache = tmpCache(t)

	res := fb.QueryAll([]Question{
		{Title: "题1", Type: "completion"},
		{Title: "题2", Type: "completion"},
	}, 0)

	if res[0] != "答案一" || res[1] != "答案二" {
		t.Fatalf("回退结果错误: %#v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(p2Questions) != 1 || p2Questions[0] != "题2" {
		t.Fatalf("已命中的题目不应再传给下一个题库: %#v", p2Questions)
	}
}

func TestFallbackSetWorkFeedback(t *testing.T) {
	var got []string
	p1 := newFakeProvider(t, "题库一", func(q Question) (string, bool) { return "", false })
	p2 := newBase("题库二", map[string]string{}, nil)
	p2.cache = tmpCache(t)
	p2.query = func(q Question) (string, bool) { return "", false }
	// 用一个会记录反馈的实现替换
	rec := &recordingProvider{baseProvider: p2}
	rec.onFeedback = func(fb []string) { got = fb }

	fb := newFallback(map[string]string{}, nil, []Provider{p1, rec})
	fb.cache = tmpCache(t)
	fb.SetWorkFeedback([]string{"第1题答错"})

	if len(got) != 1 || got[0] != "第1题答错" {
		t.Fatalf("反馈未透传: %#v", got)
	}
	if len(fb.feedback) != 1 {
		t.Fatalf("回退题库自身也应记录反馈: %#v", fb.feedback)
	}
}

func TestFallbackDisabledWhenAllChildrenDisabled(t *testing.T) {
	d1 := newDummy(map[string]string{}, nil)
	d2 := newDummy(map[string]string{}, nil)
	fb := newFallback(map[string]string{}, nil, []Provider{d1, d2})
	if !fb.Disabled() {
		t.Fatal("所有子题库停用时回退题库应停用")
	}

	// 无子题库也视为停用
	empty := newFallback(map[string]string{}, nil, nil)
	if !empty.Disabled() {
		t.Fatal("无子题库时回退题库应停用")
	}
}

// recordingProvider 用于验证 SetWorkFeedback 透传。
type recordingProvider struct {
	*baseProvider
	onFeedback func([]string)
}

func (r *recordingProvider) SetWorkFeedback(fb []string) {
	r.baseProvider.feedback = fb
	if r.onFeedback != nil {
		r.onFeedback(fb)
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1下列哪项正确", "下列哪项正确"},
		{"12. 题干（2.5分）", ". 题干"}, // 去开头数字后再去结尾分值（对齐 Python 两条正则）
		{"题干内容（1.0分）", "题干内容"},
		{"题干内容", "题干内容"},
	}
	for _, c := range cases {
		if got := normalizeTitle(c.in); got != c.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 带分值后缀的真实场景
	if got := normalizeTitle("3关于计算机的说法（1.5分）"); strings.HasSuffix(got, "（1.5分）") {
		t.Errorf("应去除分值后缀: %q", got)
	}
}

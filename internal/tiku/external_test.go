package tiku_test

import (
	"testing"

	"github.com/butlanys/chaoxing-go/internal/logx"
	"github.com/butlanys/chaoxing-go/internal/tiku"
)

// 验证外部包可以直接用 *logx.Logger 调用 tiku.New（logger 是包内未导出的最小接口）。
func TestExternalNewWithLogx(t *testing.T) {
	p, err := tiku.New(map[string]string{"provider": "TikuManual"}, logx.New("tester"))
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	if p == nil || !p.IsManual() {
		t.Fatalf("期望手动题库, 得到 %#v", p)
	}
	if p.Name() == "" || p.SubmitMode() != "1" || p.CoverRate() != 0.9 {
		t.Fatalf("公共配置初始化异常: name=%q submit=%q cover=%v", p.Name(), p.SubmitMode(), p.CoverRate())
	}

	// 未注入 ManualHook 时返回全空
	res := p.QueryAll([]tiku.Question{{Title: "题"}}, 0)
	if len(res) != 1 || res[0] != "" {
		t.Fatalf("未注入回调应返回空: %#v", res)
	}

	// 注入回调后应取到手动答案
	tiku.ManualHook = func(qs []tiku.Question) []string {
		out := make([]string, len(qs))
		for i := range out {
			out[i] = "手动答案"
		}
		return out
	}
	defer func() { tiku.ManualHook = nil }()

	res2 := p.QueryAll([]tiku.Question{{Title: "题1"}, {Title: "题2"}}, 0)
	if len(res2) != 2 || res2[0] != "手动答案" || res2[1] != "手动答案" {
		t.Fatalf("手动回调未生效: %#v", res2)
	}
}

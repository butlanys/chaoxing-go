package tiku

import (
	"reflect"
	"testing"
)

func TestNewEmptyProvider(t *testing.T) {
	// provider 缺失
	p, err := New(map[string]string{}, nil)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !p.Disabled() {
		t.Fatal("空 provider 应返回 Disabled 的 DummyProvider")
	}
	if _, ok := p.(*DummyProvider); !ok {
		t.Fatalf("期望 *DummyProvider, 得到 %T", p)
	}

	// provider 为空白
	p2, _ := New(map[string]string{"provider": "   "}, nil)
	if !p2.Disabled() {
		t.Fatal("空白 provider 应为 Disabled")
	}

	// 未命中查询返回等长全空切片
	qs := []Question{{Title: "题1"}, {Title: "题2"}}
	res := p.QueryAll(qs, 0)
	if len(res) != 2 || res[0] != "" || res[1] != "" {
		t.Fatalf("Disabled 题库应返回等长全空切片, 得到 %#v", res)
	}
	if _, ok := p.Query(qs[0]); ok {
		t.Fatal("Disabled 题库不应命中")
	}
}

func TestNewSingleProvider(t *testing.T) {
	p, err := New(map[string]string{"provider": "TikuYanxi", "tokens": "tok1,tok2"}, nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	y, ok := p.(*TikuYanxi)
	if !ok {
		t.Fatalf("期望 *TikuYanxi, 得到 %T", p)
	}
	if y.Disabled() {
		t.Fatal("言溪题库不应停用")
	}
	if len(y.tokens) != 2 {
		t.Fatalf("token 解析失败: %#v", y.tokens)
	}
	if y.Name() != "言溪题库" {
		t.Fatalf("名称错误: %s", y.Name())
	}
}

func TestNewMultiProviderFallback(t *testing.T) {
	p, err := New(map[string]string{"provider": "TikuAdapter, TikuManual", "url": "http://example.com/api"}, nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	fb, ok := p.(*FallbackProvider)
	if !ok {
		t.Fatalf("期望 *FallbackProvider, 得到 %T", p)
	}
	if len(fb.providers) != 2 {
		t.Fatalf("回退题库数量错误: %d", len(fb.providers))
	}
	if !fb.IsManual() {
		t.Fatal("含手动题库时 IsManual 应为 true")
	}
}

func TestNewInvalidProvider(t *testing.T) {
	p, err := New(map[string]string{"provider": "NotExist"}, nil)
	if err != nil {
		t.Fatalf("无效 provider 不应返回错误(Python 语义): %v", err)
	}
	if !p.Disabled() {
		t.Fatal("无效 provider 应返回 Disabled 题库")
	}
}

func TestNewCoverRateAndLists(t *testing.T) {
	// 默认值
	def, _ := New(map[string]string{"provider": "TikuAdapter", "url": "http://x"}, nil)
	if def.CoverRate() != 0.9 {
		t.Fatalf("默认 cover_rate 应为 0.9, 得到 %v", def.CoverRate())
	}
	if !reflect.DeepEqual(def.TrueList(), []string{"正确", "对", "√", "是"}) {
		t.Fatalf("默认 true_list 错误: %#v", def.TrueList())
	}
	if !reflect.DeepEqual(def.FalseList(), []string{"错误", "错", "×", "否", "不对", "不正确"}) {
		t.Fatalf("默认 false_list 错误: %#v", def.FalseList())
	}
	if def.SubmitMode() != "1" {
		t.Fatalf("默认 submit 为假时应为 \"1\"(仅保存), 得到 %q", def.SubmitMode())
	}

	// 自定义值
	custom, _ := New(map[string]string{
		"provider":   "TikuAdapter",
		"url":        "http://x",
		"cover_rate": "0.5",
		"true_list":  "是,好",
		"false_list": "否,坏",
		"submit":     "true",
	}, nil)
	if custom.CoverRate() != 0.5 {
		t.Fatalf("cover_rate 解析错误: %v", custom.CoverRate())
	}
	if !reflect.DeepEqual(custom.TrueList(), []string{"是", "好"}) {
		t.Fatalf("true_list 解析错误: %#v", custom.TrueList())
	}
	if !reflect.DeepEqual(custom.FalseList(), []string{"否", "坏"}) {
		t.Fatalf("false_list 解析错误: %#v", custom.FalseList())
	}
	if custom.SubmitMode() != "" {
		t.Fatalf("submit 为真时应为 \"\"(提交), 得到 %q", custom.SubmitMode())
	}

	// JudgementSelect 使用自定义表
	if !custom.JudgementSelect("好") {
		t.Error("自定义 true_list 项应判定为正确")
	}
	if custom.JudgementSelect("坏") {
		t.Error("自定义 false_list 项应判定为错误")
	}
	// 内置同义词
	if !custom.JudgementSelect("√") {
		t.Error("内置 true 同义词应判定为正确")
	}
	if custom.JudgementSelect("×") {
		t.Error("内置 false 同义词应判定为错误")
	}
}

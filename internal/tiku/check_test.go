package tiku

import (
	"reflect"
	"testing"
)

func TestCut(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"空串", "", nil},
		{"纯空白", "   ", nil},
		{"无分隔符", "ABC", []string{"ABC"}},
		{"两端空白", "  ABC  ", []string{"ABC"}},
		{"英文逗号", "A,B", []string{"A", "B"}},
		{"中文逗号", "A，B", []string{"A", "B"}},
		{"换行", "A\nB", []string{"A", "B"}},
		{"顿号", "A、B", []string{"A", "B"}},
		{"空格", "A B", []string{"A", "B"}},
		{"竖线", "A|B|C", []string{"A", "B", "C"}},
		{"井号", "A#B", []string{"A", "B"}},
		{"回车", "A\rB", []string{"A", "B"}},
		{"制表符", "A\tB", []string{"A", "B"}},
		{"星号", "A*B", []string{"A", "B"}},
		{"反斜杠", `A\B`, []string{"A", "B"}},
		{"英文句点", "A.B", []string{"A", "B"}},
		{"分号非分隔符", "A;B", []string{"A;B"}},
		{"多余分隔符丢弃空段", "A,,B", []string{"A", "B"}},
		{"判断题文本", "正确,对", []string{"正确", "对"}},
		{"换行优先于逗号", "A,B\nC", []string{"A,B", "C"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Cut(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Cut(%q) = %#v, want %#v", c.in, got, c.want)
			}
		})
	}
}

func TestCheckSingle(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"A", true},
		{"选项内容", true},
		{"对", true},
		{"A,B", true},   // 逗号不是强分隔符
		{"A，B", true},   // 中文逗号也不是
		{"A\nB", false}, // 换行是强分隔符
		{"A|B", false},  // 竖线
		{"A#B", false},  // 井号
		{"A\tB", false}, // 制表符
		{"A\rB", false}, // 回车
		{"A、B", false},  // 顿号
		{" A \n B ", false},
	}
	for _, c := range cases {
		if got := CheckSingle(c.in); got != c.want {
			t.Errorf("CheckSingle(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCheckMultiple(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"   ", false},
		{"A", true},
		{"A,B", true},
		{"A\nB", true},
		{"选项一 选项二", true},
	}
	for _, c := range cases {
		if got := CheckMultiple(c.in); got != c.want {
			t.Errorf("CheckMultiple(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCheckJudgement(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"对", 1},
		{"正确", 1},
		{"√", 1},
		{"是", 1},
		{"T", 1},
		{"t", 1},
		{"true", 1},
		{"TRUE", 1},
		{"1", 1},
		{"yes", 1},
		{"Y", 1},
		{" 正确 ", 1},
		{"错", 0},
		{"错误", 0},
		{"×", 0},
		{"否", 0},
		{"F", 0},
		{"false", 0},
		{"0", 0},
		{"no", 0},
		{"N", 0},
		{"不对", 0},
		{"不正确", 0},
		{"x", -1}, // answer_check.py 内置表不含 x
		{"maybe", -1},
		{"", -1},
	}
	for _, c := range cases {
		if got := CheckJudgement(c.in, nil, nil); got != c.want {
			t.Errorf("CheckJudgement(%q) = %d, want %d", c.in, got, c.want)
		}
	}

	// 自定义同义词表
	if got := CheckJudgement("是的是", []string{"是的是"}, nil); got != 1 {
		t.Errorf("自定义 true_list 命中失败: %d", got)
	}
	if got := CheckJudgement("不是的", nil, []string{"不是的"}); got != 0 {
		t.Errorf("自定义 false_list 命中失败: %d", got)
	}
}

func TestCheckCompletion(t *testing.T) {
	if !CheckCompletion("abc") {
		t.Error("非空填空应为 true")
	}
	if CheckCompletion("") {
		t.Error("空填空应为 false")
	}
}

func TestCheckAnswer(t *testing.T) {
	p := newBase("测试题库", map[string]string{}, nil)
	p.cache = NewCache(t.TempDir() + "/c.json")

	cases := []struct {
		name string
		ans  string
		typ  string
		want bool
	}{
		{"单选合法", "选项内容", "single", true},
		{"单选被判定为判断题", "对", "single", false},
		{"单选多段", "A\nB", "single", false},
		{"多选合法", "选项一\n选项二", "multiple", true},
		{"多选判断题文本", "对", "multiple", false},
		{"填空合法", "答案文本", "completion", true},
		{"填空为空", "", "completion", false},
		{"判断正确", "对", "judgement", true},
		{"判断错误", "错误", "judgement", true},
		{"判断无法识别", "maybe", "judgement", false},
		{"未知类型放行", "任意", "shortanswer", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CheckAnswer(c.ans, c.typ, p); got != c.want {
				t.Fatalf("CheckAnswer(%q, %q) = %v, want %v", c.ans, c.typ, got, c.want)
			}
		})
	}

	// 手动模式豁免
	manual := newManual(map[string]string{}, nil)
	if !CheckAnswer("对", "single", manual) {
		t.Error("手动模式应豁免校验")
	}
	// 多题库回退豁免
	fb := newFallback(map[string]string{}, nil, []Provider{p})
	if !CheckAnswer("对", "single", fb) {
		t.Error("多题库回退应豁免校验")
	}
}

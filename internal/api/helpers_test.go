package api

import (
	"math"
	"strings"
	"testing"
)

// 期望值由 Python 参考实现（api/base.py 的同名函数 + difflib.SequenceMatcher）实际运行得到。

func TestNormalizeText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"去字母编号", "A. 中国的首都", "中国的首都"},
		{"异体字映射", "⻛景这边独好", "风景这边独好"},
		{"去空白与标点", "  a b ,c。 ", "abc"},
		{"去括号", "【答案】", "答案"},
		{"顿号编号", "B、选项内容", "选项内容"},
		{"单字母编号会被剥掉", "ABC", "bc"},
		{"空白折叠", "中 文\t测试", "中文测试"},
		{"纯字母被剥空", "a", ""},
		{"空串", "", ""},
		{"点号编号", "B.答案", "答案"},
	}
	for _, tc := range cases {
		if got := NormalizeText(tc.in); got != tc.want {
			t.Errorf("[%s] NormalizeText(%q) = %q, 期望 %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestCleanRes(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"字母点号编号", []string{"A. 答案"}, []string{"答案"}},
		{"字母顿号编号", []string{"B、选项"}, []string{"选项"}},
		{"单字符不剥", []string{"a"}, []string{"a"}},
		{"去逗号", []string{"甲,乙"}, []string{"甲乙"}},
		{"单字母大写不剥", []string{"A"}, []string{"A"}},
		{"括号编号", []string{"A)答案"}, []string{"答案"}},
		{"数字编号不剥", []string{"1、选项"}, []string{"1、选项"}},
		{"去句号", []string{"甲。乙"}, []string{"甲乙"}},
	}
	for _, tc := range cases {
		got := CleanRes(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("[%s] CleanRes(%v) 长度 %d, 期望 %d", tc.name, tc.in, len(got), len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("[%s] CleanRes(%v)[%d] = %q, 期望 %q", tc.name, tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestIsSubsequence(t *testing.T) {
	cases := []struct {
		a, o string
		want bool
	}{
		{"a", "abc", true},
		{"abc", "ab", false},
		{"中国", "我爱中国", true},
		{"ac", "abc", true},
		{"ca", "abc", false},
		{"", "abc", true},
		{"ABC", "abc", true},
		{"abc", "ABC", true},
	}
	for _, tc := range cases {
		if got := IsSubsequence(tc.a, tc.o); got != tc.want {
			t.Errorf("IsSubsequence(%q, %q) = %v, 期望 %v", tc.a, tc.o, got, tc.want)
		}
	}
}

func TestNormalizeAnswerText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"对", "TRUE"},
		{"√", "TRUE"},
		{"1", "TRUE"},
		{"是", "TRUE"},
		{"Y", "TRUE"},
		{"yes", "TRUE"},
		{"错", "FALSE"},
		{"X", "FALSE"},
		{"0", "FALSE"},
		{"不正确", "FALSE"},
		{"N", "FALSE"},
		{"no", "FALSE"},
		{"A", "A"},
		{"AB", "AB"},
		{"A1", "A1"},
		{"A.答案", "答案"},
		{"答案。", "答案"},
		{"  答 案 ", "答案"},
		{"对错", "对错"},
		{"", ""},
		// 归一化只处理空白与标点，"对。"不在同义词集合里，与原版一致地退化为去标点
		{"对。", "对"},
	}
	for _, tc := range cases {
		if got := NormalizeAnswerText(tc.in); got != tc.want {
			t.Errorf("NormalizeAnswerText(%q) = %q, 期望 %q", tc.in, got, tc.want)
		}
	}
}

// TestSequenceRatioPythonGolden 与 CPython difflib.SequenceMatcher.ratio 的实测值逐位比对。
func TestSequenceRatioPythonGolden(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"中国首都北京", "中国首都北京", 1.0},
		{"中国首都北京", "中国的首都是北京", 0.8571428571428571},
		{"中华人民共和国", "中华人民共和共", 0.8571428571428571},
		{"abc", "abd", 0.6666666666666666},
		{"", "", 1.0},
		{"abc", "", 0.0},
		{"答案", "答案A", 0.8},
		{"ABCD", "ABDC", 0.75},
		{"hello world", "hello  world", 0.9565217391304348},
		{"这是答案", "这是答案。", 0.8888888888888888},
		{"A", "B", 0.0},
		{"abcdefg", "xyzabcdefg", 0.8235294117647058},
	}
	for _, tc := range cases {
		got := sequenceRatio(tc.a, tc.b)
		if math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("sequenceRatio(%q, %q) = %v, 期望 %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestBestOptionBySimilarity(t *testing.T) {
	options := []string{"A. 中国首都北京", "B. 上海", "C. 广州"}

	if got := BestOptionBySimilarity("中国首都北京", options, 0.8); got != "A" {
		t.Errorf("完全一致时应命中 A, 实际 %q", got)
	}
	if got := BestOptionBySimilarity("广州", options, 0.8); got != "C" {
		t.Errorf("命中广州时应返回 C, 实际 %q", got)
	}
	if got := BestOptionBySimilarity("完全无关的内容啊", options, 0.8); got != "" {
		t.Errorf("低于阈值应返回空串, 实际 %q", got)
	}
	if got := BestOptionBySimilarity("", options, 0.8); got != "" {
		t.Errorf("空目标应返回空串, 实际 %q", got)
	}
	if got := BestOptionBySimilarity("上海", nil, 0.8); got != "" {
		t.Errorf("空选项应返回空串, 实际 %q", got)
	}
}

func TestMultiCut(t *testing.T) {
	got := MultiCut("A. 甲\nB. 乙")
	want := []string{"A. 甲", "B. 乙"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("MultiCut 换行切分 = %v, 期望 %v", got, want)
	}

	got = MultiCut("甲、乙、丙")
	want = []string{"甲", "乙", "丙"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("MultiCut 顿号切分 = %v, 期望 %v", got, want)
	}

	if got := MultiCut(""); got != nil {
		t.Errorf("MultiCut(\"\") 应返回 nil, 实际 %v", got)
	}
}

func TestRandomAnswer(t *testing.T) {
	multipleOptions := "A. 甲\nB. 乙\nC. 丙\nD. 丁"
	singleOptions := "A. 甲\nB. 乙"

	// 多次抽样，校验取值始终落在合法集合内
	for i := 0; i < 300; i++ {
		got := RandomAnswer(multipleOptions, "multiple")
		if n := len([]rune(got)); n < 2 || n > 4 {
			t.Fatalf("多选题随机答案 %q 长度 %d 超出 [2,4]", got, n)
		}
		if strings.ContainsAny(got, "EFGH") {
			t.Fatalf("多选题随机答案 %q 出现越界选项", got)
		}
		seen := map[rune]bool{}
		prev := rune(0)
		for _, r := range got {
			if seen[r] {
				t.Fatalf("多选题随机答案 %q 存在重复选项", got)
			}
			seen[r] = true
			if prev != 0 && r <= prev {
				t.Fatalf("多选题随机答案 %q 未排序", got)
			}
			prev = r
		}
	}

	for i := 0; i < 100; i++ {
		got := RandomAnswer(singleOptions, "single")
		if got != "A" && got != "B" {
			t.Fatalf("单选题随机答案 %q 不是选项首字母", got)
		}
	}

	for i := 0; i < 50; i++ {
		got := RandomAnswer(multipleOptions, "judgement")
		if got != "true" && got != "false" {
			t.Fatalf("判断题随机答案 %q 非法", got)
		}
	}

	if got := RandomAnswer("", "single"); got != "" {
		t.Errorf("空选项应返回空串, 实际 %q", got)
	}

	// 单选题带 4 个选项时，随机结果只能取自 4 个首字母
	seen := map[string]bool{}
	for i := 0; i < 300; i++ {
		seen[RandomAnswer(multipleOptions, "single")] = true
	}
	for k := range seen {
		if !strings.Contains("ABCD", k) || len(k) != 1 {
			t.Fatalf("单选题随机结果越界: %q", k)
		}
	}
}

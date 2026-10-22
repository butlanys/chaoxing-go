package tiku

import "strings"

// cutChars 答案切分字符表。
// 顺序与字符集必须与 answer_check.py 的 cut_char 一字不差（含 ",", "，", "\n", "、" 等）。
var cutChars = []string{
	"\n", ",", "，", "|", "\r", "\t", "#", "*", "-", "_", "+", "@", "~", "/", "\\", ".", "&", " ", "、",
}

// strongDelimiters 为 check_single 使用的“强分隔符”，用于判断答案是否包含多段内容（即非单选）。
var strongDelimiters = []string{"\n", "|", "#", "\t", "\r", "、"}

// judgeTrueBuiltin / judgeFalseBuiltin 对应 answer_check.py 中 check_judgement 内置的真/假同义词表。
var (
	judgeTrueBuiltin  = []string{"true", "t", "1", "对", "正确", "√", "是", "yes", "y"}
	judgeFalseBuiltin = []string{"false", "f", "0", "错", "错误", "×", "否", "no", "n", "不对", "不正确"}
)

// judgeTrueSelect / judgeFalseSelect 对应 base.py 的 _JUDGE_TRUE_SET/_JUDGE_FALSE_SET（已小写化），
// 供 JudgementSelect 使用；注意其错误集合额外包含 "x"。
var (
	judgeTrueSelect  = []string{"true", "t", "1", "对", "正确", "√", "是", "yes", "y"}
	judgeFalseSelect = []string{"false", "f", "0", "错", "错误", "×", "x", "否", "no", "n", "不对", "不正确"}
)

// Cut 移植 answer_check.cut：
// 按 cutChars 顺序找到第一个在答案中出现过的分隔符进行切分，逐段去空白并丢弃空段；
// 若该分隔符切分后没有有效段落则继续尝试下一个；全部分隔符都不适用时返回去空白后的整体，
// 整体为空则返回 nil（对应 Python 的 None）。
func Cut(answer string) []string {
	for _, ch := range cutChars {
		if !strings.Contains(answer, ch) {
			continue
		}
		res := make([]string, 0, 4)
		for _, opt := range strings.Split(answer, ch) {
			if s := strings.TrimSpace(opt); s != "" {
				res = append(res, s)
			}
		}
		if len(res) > 0 {
			return res
		}
	}
	s := strings.TrimSpace(answer)
	if s != "" {
		return []string{s}
	}
	return nil
}

// CheckSingle 移植 answer_check.check_single：
// 空串视为不合法；只要出现明显的“多段答案”分隔符即判定为非单选。
func CheckSingle(answer string) bool {
	text := strings.TrimSpace(answer)
	if text == "" {
		return false
	}
	for _, sep := range strongDelimiters {
		parts := 0
		for _, p := range strings.Split(text, sep) {
			if strings.TrimSpace(p) != "" {
				parts++
			}
		}
		if parts > 1 {
			return false
		}
	}
	return true
}

// CheckMultiple 移植 answer_check.check_multiple：能被 cut 切出至少一段即视为多选答案。
func CheckMultiple(answer string) bool {
	t := Cut(answer)
	return len(t) > 0
}

// CheckJudgement 移植 answer_check.check_judgement：返回 1(正确)/0(错误)/-1(无法判断)。
func CheckJudgement(answer string, trueList, falseList []string) int {
	val := strings.ToLower(strings.TrimSpace(answer))
	if inStrings(judgeTrueBuiltin, val) || inLowered(trueList, val) {
		return 1
	}
	if inStrings(judgeFalseBuiltin, val) || inLowered(falseList, val) {
		return 0
	}
	return -1
}

// CheckCompletion 移植 answer_check.check_completion：非空即视为有效填空答案。
func CheckCompletion(answer string) bool {
	return len(answer) > 0
}

// CheckAnswer 移植 answer_check.check_answer：
// 手动模式与多题库回退包装器直接信任（豁免常规校验），其余按题型逐条校验。
func CheckAnswer(answer, typ string, p Provider) bool {
	if p == nil {
		return true
	}
	// 手动模式豁免常规校验
	if p.IsManual() {
		return true
	}
	// 多题库回退包装器豁免（其子题库在各自环节均已单独校验过）
	if s, ok := p.(interface{ skipAnswerValidation() bool }); ok && s.skipAnswerValidation() {
		return true
	}

	tl, fl := p.TrueList(), p.FalseList()
	switch typ {
	case "single":
		if CheckSingle(answer) && CheckJudgement(answer, tl, fl) == -1 {
			return true
		}
	case "multiple":
		if CheckMultiple(answer) && CheckJudgement(answer, tl, fl) == -1 {
			return true
		}
	case "completion":
		if CheckCompletion(answer) {
			return true
		}
	case "judgement":
		if CheckJudgement(answer, tl, fl) != -1 {
			return true
		}
	default: // 未知类型不匹配
		return true
	}
	return false
}

// inStrings 判断 val 是否在 list 中（精确匹配）。
func inStrings(list []string, val string) bool {
	for _, s := range list {
		if s == val {
			return true
		}
	}
	return false
}

// inLowered 判断 val 是否命中 list（逐项小写后比较）。
func inLowered(list []string, val string) bool {
	for _, s := range list {
		if strings.ToLower(s) == val {
			return true
		}
	}
	return false
}

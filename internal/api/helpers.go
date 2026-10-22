package api

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/butlanys/chaoxing-go/internal/logx"
	"github.com/butlanys/chaoxing-go/internal/tiku"
)

// 本文件对应 Python 原版 api/base.py 的答案后处理工具函数：
// multi_cut / clean_res / normalize_text / get_option_text / best_option_by_similarity /
// is_subsequence / random_answer / normalize_answer_text，以及若干内部小工具。
//
// 其中 best_option_by_similarity 依赖 difflib.SequenceMatcher.ratio，
// Go 标准库没有等价实现，这里按 CPython difflib 的算法逐行复刻（见 sequenceRatio）。

// pySpaceClass 等价 Python re 的 \s（str 模式，含 Unicode 空白）。
// Go 的 \s 只覆盖 [\t\n\f\r ]，故显式列出 Python 的全部空白字符。
const pySpaceClass = `[\t\n\v\f\r \x{001c}-\x{001f}\x{0085}\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`

var (
	// 对齐 clean_res: r'^[A-Za-z]\s*[.、:：)?）]?\s*|[.,!?;:，。！？；：]'
	reCleanRes = regexp.MustCompile(`^[A-Za-z]` + pySpaceClass + `*[.、:：)?）]?` + pySpaceClass + `*|[.,!?;:，。！？；：]`)

	// 对齐 normalize_text 的前缀剥离: r'^[A-Za-z]\s*[.、:：)?）]?\s*'
	reOptionPrefix = regexp.MustCompile(`^[A-Za-z]` + pySpaceClass + `*[.、:：)?）]?` + pySpaceClass + `*`)

	// 对齐 normalize_text 的空白折叠: r'\s+'
	rePySpace = regexp.MustCompile(pySpaceClass + `+`)

	// 对齐 normalize_text 的标点剥离: r'[，。！？；：,.!?;:()（）\[\]【】"“”‘’\-_/\\|]'
	reNormalizePunct = regexp.MustCompile(`[，。！？；：,.!?;:()（）\[\]【】"“”‘’\-_/\\|]`)

	// 对齐 normalize_answer_text 的字母编号剥离: r'^[A-Z][.、:：)?）]'
	reAnswerLetterPrefix = regexp.MustCompile(`^[A-Z][.、:：)?）]`)

	// 对齐 normalize_answer_text 的标点剥离: r'[，。、！？；：,.!?;:()（）\[\]【】"“”‘’\-_/\\|]'
	reAnswerPunct = regexp.MustCompile(`[，。、！？；：,.!?;:()（）\[\]【】"“”‘’\-_/\\|]`)

	// 对齐 normalize_answer_text 的 re.fullmatch(r'[A-Z0-9]+', s)
	reAnswerAlnum = regexp.MustCompile(`^[A-Z0-9]+$`)
)

// normalizeCharMap 对齐 normalize_text 的常见异体字映射。
var normalizeCharMap = strings.NewReplacer(
	"⻛", "风",
	"⻔", "门",
	"⻋", "车",
	"⻢", "马",
)

// judgeTrueSet / judgeFalseSet 对齐 base.py 的 _JUDGE_TRUE_SET / _JUDGE_FALSE_SET。
var (
	judgeTrueSet = map[string]bool{
		"TRUE": true, "T": true, "1": true, "对": true, "正确": true,
		"√": true, "是": true, "YES": true, "Y": true,
	}
	judgeFalseSet = map[string]bool{
		"FALSE": true, "F": true, "0": true, "错": true, "错误": true, "×": true,
		"X": true, "否": true, "NO": true, "N": true, "不对": true, "不正确": true,
	}
)

// MultiCut 将答案字符串按特定字符切割并返回切割后的答案列表（对齐 base.py multi_cut）。
// 无法切分时返回 nil（对应 Python 的 None）。
func MultiCut(answer string) []string {
	res := tiku.Cut(answer)
	if res == nil {
		logx.Default().Warn("未能从网页中提取题目信息, 以下为相关信息：\n\t%s\n", answer)
		logx.Default().Warn("未能正确提取题目选项信息! 请反馈并提供以上信息")
		return nil
	}
	return res
}

// CleanRes 对齐 base.py clean_res：仅在字符串长度大于 1 时去除开头的字母编号，
// 并去掉各种标点，最后 strip。
func CleanRes(res []string) []string {
	cleaned := make([]string, 0, len(res))
	for _, c := range res {
		// 注意：Python 的 len(c) 按字符计，中文一字算一个
		if utf8.RuneCountInString(c) > 1 {
			c = reCleanRes.ReplaceAllString(c, "")
		}
		cleaned = append(cleaned, strings.TrimSpace(c))
	}
	return cleaned
}

// NormalizeText 对齐 base.py normalize_text：统一异体字 -> 去字母编号 -> 去空白 -> 去标点 -> 小写。
func NormalizeText(text string) string {
	normalized := normalizeCharMap.Replace(text)
	normalized = reOptionPrefix.ReplaceAllString(normalized, "")
	normalized = rePySpace.ReplaceAllString(normalized, "")
	normalized = reNormalizePunct.ReplaceAllString(normalized, "")
	return strings.ToLower(normalized)
}

// getOptionText 对齐 base.py get_option_text（未导出，与 CONTRACT 的公开面保持一致）。
func getOptionText(option string) string {
	return strings.TrimSpace(reOptionPrefix.ReplaceAllString(option, ""))
}

// NormalizeAnswerText 对齐 base.py normalize_answer_text：
// 消除 对/错 与 true/false、全半角标点等格式差异（归一化是确定性的）。
func NormalizeAnswerText(ans string) string {
	s := rePySpace.ReplaceAllString(ans, "")
	s = strings.ToUpper(s)
	if judgeTrueSet[s] {
		return "TRUE"
	}
	if judgeFalseSet[s] {
		return "FALSE"
	}
	if reAnswerAlnum.MatchString(s) {
		return s
	}
	s = reAnswerLetterPrefix.ReplaceAllString(s, "")
	s = reAnswerPunct.ReplaceAllString(s, "")
	return s
}

// IsSubsequence 对齐 base.py is_subsequence：大小写不敏感的子序列判断。
func IsSubsequence(a, o string) bool {
	iter := []rune(strings.ToLower(o))
	idx := 0
	for _, c := range strings.ToLower(a) {
		found := false
		for idx < len(iter) {
			cur := iter[idx]
			idx++
			if cur == c {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// BestOptionBySimilarity 对齐 base.py best_option_by_similarity：
// 用 difflib.SequenceMatcher 等价相似度挑最像的选项，达到阈值返回其首字母，否则返回 ""。
func BestOptionBySimilarity(target string, options []string, threshold float64) string {
	if target == "" || len(options) == 0 {
		return ""
	}
	targetNorm := NormalizeText(target)
	if targetNorm == "" {
		return ""
	}

	bestLetter := ""
	bestScore := 0.0
	for _, option := range options {
		optionNorm := NormalizeText(getOptionText(option))
		if optionNorm == "" {
			continue
		}
		score := sequenceRatio(targetNorm, optionNorm)
		if score > bestScore {
			bestScore = score
			bestLetter = firstRune(option)
		}
	}

	if bestScore >= threshold {
		logx.Default().Info("相似度兜底匹配成功: %s (score=%.2f, threshold=%.2f)", bestLetter, bestScore, threshold)
		return bestLetter
	}
	return ""
}

// RandomAnswer 对齐 base.py random_answer：按题型随机作答。
func RandomAnswer(options, qType string) string {
	answer := ""
	if options == "" {
		return answer
	}

	switch qType {
	case "multiple":
		logx.Default().Debug("当前选项列表[cut前] -> %s", options)
		opList := MultiCut(options)
		logx.Default().Debug("当前选项列表[cut后] -> %v", opList)

		if len(opList) == 0 {
			logx.Default().Error("选项为空, 未能正确提取题目选项信息! 请反馈并提供以上信息")
			return answer
		}

		availableOptions := len(opList)
		selectCount := 0

		// 根据可用选项数量调整可能选择的选项数
		if availableOptions <= 1 {
			selectCount = availableOptions
		} else {
			maxPossible := min(4, availableOptions)
			minPossible := min(2, availableOptions)

			weightsMap := map[int][]float64{
				2: {1.0},
				3: {0.3, 0.7},
				4: {0.1, 0.5, 0.4},
				5: {0.1, 0.4, 0.3, 0.2},
			}

			weights, ok := weightsMap[maxPossible]
			if !ok {
				weights = []float64{0.3, 0.4, 0.3}
			}
			possibleCounts := make([]int, 0, maxPossible-minPossible+1)
			for c := minPossible; c <= maxPossible; c++ {
				possibleCounts = append(possibleCounts, c)
			}
			if len(weights) > len(possibleCounts) {
				weights = weights[:len(possibleCounts)]
			}

			weightsSum := 0.0
			for _, w := range weights {
				weightsSum += w
			}
			if weightsSum > 0 {
				for i := range weights {
					weights[i] /= weightsSum
				}
			}

			selectCount = weightedChoice(possibleCounts, weights)
		}

		selected := randomSample(opList, selectCount)
		for _, option := range selected {
			answer += firstRune(option) // 取首字为答案，例如 A 或 B
		}
		answer = sortUniqueRunes(answer)
	case "single":
		lines := strings.Split(options, "\n")
		answer = firstRune(lines[rand.Intn(len(lines))]) // 取首字为答案, 例如 A 或 B
	case "judgement":
		if rand.Intn(2) == 0 {
			answer = "true"
		} else {
			answer = "false"
		}
	}
	logx.Default().Info("随机选择 -> %s", answer)
	return answer
}

// weightedChoice 对应 Python random.choices(population, weights=weights, k=1)[0]。
func weightedChoice(population []int, weights []float64) int {
	if len(population) == 0 {
		return 0
	}
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total <= 0 {
		return population[rand.Intn(len(population))]
	}
	r := rand.Float64() * total
	acc := 0.0
	for i, w := range weights {
		if i >= len(population) {
			break
		}
		acc += w
		if r < acc {
			return population[i]
		}
	}
	return population[len(population)-1]
}

// randomSample 对应 Python random.sample(list, k)（不放回抽样，顺序不影响最终答案）。
func randomSample(list []string, k int) []string {
	if k <= 0 {
		return nil
	}
	if k > len(list) {
		k = len(list)
	}
	idx := make([]int, len(list))
	for i := range idx {
		idx[i] = i
	}
	for i := 0; i < k; i++ {
		j := i + rand.Intn(len(idx)-i)
		idx[i], idx[j] = idx[j], idx[i]
	}
	out := make([]string, 0, k)
	for _, i := range idx[:k] {
		out = append(out, list[i])
	}
	return out
}

// ---- difflib.SequenceMatcher.ratio 的等价实现 ----

// matchBlock 匹配块（对应 difflib 的 Match 具名元组）
type matchBlock struct {
	a, b, size int
}

// sequenceRatio 等价 Python difflib.SequenceMatcher(None, a, b).ratio()。
// ratio = 2 * M / T，M 为全部匹配块的字符总数，T = len(a) + len(b)。
func sequenceRatio(a, b string) float64 {
	ar, br := []rune(a), []rune(b)
	length := len(ar) + len(br)
	if length == 0 {
		return 1.0
	}
	b2j := make(map[rune][]int, len(br))
	for j, c := range br {
		b2j[c] = append(b2j[c], j)
	}
	matches := 0
	for _, blk := range matchingBlocks(ar, br, b2j) {
		matches += blk.size
	}
	return 2.0 * float64(matches) / float64(length)
}

// longestMatch 等价 difflib.SequenceMatcher.find_longest_match（junk=None，故无 bjunk 分支）。
func longestMatch(a, b []rune, alo, ahi, blo, bhi int, b2j map[rune][]int) (int, int, int) {
	besti, bestj, bestsize := alo, blo, 0
	j2len := map[int]int{}
	for i := alo; i < ahi; i++ {
		newj2len := map[int]int{}
		for _, j := range b2j[a[i]] {
			if j < blo {
				continue
			}
			if j >= bhi {
				break
			}
			k := j2len[j-1] + 1
			newj2len[j] = k
			if k > bestsize {
				besti, bestj, bestsize = i-k+1, j-k+1, k
			}
		}
		j2len = newj2len
	}
	// 与 junk 无关的两段扩展（对齐 CPython 源码）
	for besti > alo && bestj > blo && a[besti-1] == b[bestj-1] {
		besti, bestj, bestsize = besti-1, bestj-1, bestsize+1
	}
	for besti+bestsize < ahi && bestj+bestsize < bhi && a[besti+bestsize] == b[bestj+bestsize] {
		bestsize++
	}
	return besti, bestj, bestsize
}

// matchingBlocks 等价 difflib.SequenceMatcher.get_matching_blocks。
func matchingBlocks(a, b []rune, b2j map[rune][]int) []matchBlock {
	la, lb := len(a), len(b)
	queue := [][4]int{{0, la, 0, lb}}
	blocks := make([]matchBlock, 0, 8)
	for len(queue) > 0 {
		q := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		alo, ahi, blo, bhi := q[0], q[1], q[2], q[3]
		i, j, k := longestMatch(a, b, alo, ahi, blo, bhi, b2j)
		if k > 0 {
			blocks = append(blocks, matchBlock{i, j, k})
			if alo < i && blo < j {
				queue = append(queue, [4]int{alo, i, blo, j})
			}
			if i+k < ahi && j+k < bhi {
				queue = append(queue, [4]int{i + k, ahi, j + k, bhi})
			}
		}
	}
	sort.Slice(blocks, func(x, y int) bool {
		if blocks[x].a != blocks[y].a {
			return blocks[x].a < blocks[y].a
		}
		if blocks[x].b != blocks[y].b {
			return blocks[x].b < blocks[y].b
		}
		return blocks[x].size < blocks[y].size
	})

	// 合并相邻块，并追加哨兵 (la, lb, 0)
	nonAdjacent := make([]matchBlock, 0, len(blocks)+1)
	i1, j1, k1 := 0, 0, 0
	for _, blk := range blocks {
		if i1+k1 == blk.a && j1+k1 == blk.b {
			k1 += blk.size
		} else {
			if k1 > 0 {
				nonAdjacent = append(nonAdjacent, matchBlock{i1, j1, k1})
			}
			i1, j1, k1 = blk.a, blk.b, blk.size
		}
	}
	if k1 > 0 {
		nonAdjacent = append(nonAdjacent, matchBlock{i1, j1, k1})
	}
	nonAdjacent = append(nonAdjacent, matchBlock{la, lb, 0})
	return nonAdjacent
}

// ---- 内部小工具 ----

// firstRune 返回字符串的首个字符（对应 Python 的 s[:1]）。
func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

// sortUniqueRunes 对应 Python 的 "".join(sorted(set(answer)))。
func sortUniqueRunes(s string) string {
	seen := make(map[rune]bool, len(s))
	rs := make([]rune, 0, len(s))
	for _, r := range s {
		if seen[r] {
			continue
		}
		seen[r] = true
		rs = append(rs, r)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i] < rs[j] })
	return string(rs)
}

// anyString 把 JSON 解出的任意值转成字符串（Python str() 语义的近似）。
func anyString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return t.String()
	}
	return fmt.Sprintf("%v", v)
}

// jsonInt 把 JSON 解出的任意值转成 int（对应 Python int(...)）。
func jsonInt(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
		if f, err := t.Float64(); err == nil {
			return int(f)
		}
		return 0
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return int(f)
		}
		return 0
	case bool:
		if t {
			return 1
		}
		return 0
	}
	return 0
}

// jsonFloat 把 JSON 解出的任意值转成 float64（对应 Python float(...)）。
func jsonFloat(v any) float64 {
	switch t := v.(type) {
	case nil:
		return 0
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f
		}
		return 0
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return f
		}
		return 0
	case bool:
		if t {
			return 1
		}
		return 0
	}
	return 0
}

// jsonBool 把 JSON 解出的任意值转成 bool。
func jsonBool(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true") || strings.TrimSpace(t) == "1"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case json.Number:
		return t.String() != "0"
	}
	return false
}

// truncate 按字符截断（对应 Python 的 resp.text[:200]）。
func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}

// orDefault 空串时返回占位文案（对应 Python 的 `x or '(空)'`）。
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

package tiku

import (
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 默认判断题同义词表（对应 config_template.ini 的 [tiku] 默认值）。
const (
	defaultTrueList  = "正确,对,√,是"
	defaultFalseList = "错误,错,×,否,不对,不正确"
)

// nopLogger 空日志器：当 New 的 log 参数为 nil 时使用，避免空指针。
type nopLogger struct{}

func (nopLogger) Trace(string, ...any)   {}
func (nopLogger) Debug(string, ...any)   {}
func (nopLogger) Info(string, ...any)    {}
func (nopLogger) Warn(string, ...any)    {}
func (nopLogger) Error(string, ...any)   {}
func (nopLogger) Success(string, ...any) {}

// baseProvider 所有题库的公共实现（对应 Python 的 Tiku 抽象基类）。
// 负责：通用配置（提交模式/覆盖率/判断题同义词表）、缓存、标题预处理、答案校验与批量查询骨架。
// 子类只需通过 query 字段注入真正的网络查询实现。
type baseProvider struct {
	name       string
	log        logger
	submitMode string  // "" = 提交, "1" = 仅保存
	coverRate  float64 // 最低题库覆盖率
	trueList   []string
	falseList  []string
	disabled   bool
	cache      *Cache
	feedback   []string // 章节检测错误反馈（仅 AI 类题库使用）

	// query 单题原始查询实现（不含缓存/校验），由子类注入
	query func(q Question) (string, bool)
}

// newBase 依据 [tiku] 配置构造公共部分。
func newBase(name string, conf map[string]string, log logger) *baseProvider {
	if log == nil {
		log = nopLogger{}
	}
	b := &baseProvider{name: name, log: log, cache: defaultCache()}
	b.applyCommon(conf)
	return b
}

// applyCommon 解析通用配置项。
func (b *baseProvider) applyCommon(conf map[string]string) {
	// 提交模式：submit 为真 -> ""(提交)，否则 -> "1"(仅保存)
	if parseBool(conf["submit"], false) {
		b.submitMode = ""
	} else {
		b.submitMode = "1"
	}
	b.coverRate = parseFloat(conf["cover_rate"], 0.9)
	b.trueList = splitListDefault(conf["true_list"], defaultTrueList)
	b.falseList = splitListDefault(conf["false_list"], defaultFalseList)
}

// ---- Provider 接口的公共实现 ----

// Name 题库名。
func (b *baseProvider) Name() string { return b.name }

// SubmitMode 返回 "" 表示提交，"1" 表示仅保存。
func (b *baseProvider) SubmitMode() string { return b.submitMode }

// CoverRate 最低题库覆盖率。
func (b *baseProvider) CoverRate() float64 { return b.coverRate }

// TrueList 判断题“正确”同义词表。
func (b *baseProvider) TrueList() []string { return b.trueList }

// FalseList 判断题“错误”同义词表。
func (b *baseProvider) FalseList() []string { return b.falseList }

// Disabled 是否停用。
func (b *baseProvider) Disabled() bool { return b.disabled }

// IsManual 是否手动输入模式（默认 false，TikuManual 覆写）。
func (b *baseProvider) IsManual() bool { return false }

// SetWorkFeedback 默认空实现（仅 AI 类题库覆写）。
func (b *baseProvider) SetWorkFeedback([]string) {}

// hasFeedback 是否处于章节检测重做模式（有错误反馈则跳过缓存）。
func (b *baseProvider) hasFeedback() bool { return len(b.feedback) > 0 }

// JudgementSelect 将判断题答案文本映射为 bool（true 表示选“正确”）。
// 对齐 Python Tiku.judgement_select：内置同义词表 + 自定义 true_list/false_list，无法判断时随机。
func (b *baseProvider) JudgementSelect(answer string) bool {
	if b.disabled {
		return false
	}
	val := strings.ToLower(strings.TrimSpace(answer))

	if inStrings(judgeTrueSelect, val) {
		return true
	}
	if inStrings(judgeFalseSelect, val) {
		return false
	}
	if inLowered(b.trueList, val) || inStrings(b.trueList, val) {
		return true
	}
	if inLowered(b.falseList, val) || inStrings(b.falseList, val) {
		return false
	}
	b.log.Error("无法判断答案 -> %s 对应的是正确还是错误, 请自行判断并加入配置文件重启脚本, 本次将会随机选择选项", val)
	return rand.Intn(2) == 0
}

// Query 查询单题：标题预处理 -> 查缓存 -> 网络查询 -> 校验 -> 写缓存。
func (b *baseProvider) Query(q Question) (string, bool) {
	if b.disabled {
		return "", false
	}
	q.Title = normalizeTitle(q.Title)

	if !b.hasFeedback() {
		if ans, ok := b.cache.GetCache(q.Title); ok && strings.TrimSpace(ans) != "" {
			ans = strings.TrimSpace(ans)
			b.log.Info("从缓存中获取答案：%s -> %s", q.Title, ans)
			return ans, true
		}
	}

	if b.query == nil {
		return "", false
	}
	ans, ok := b.query(q)
	if !ok {
		b.log.Error("从%s获取答案失败：%s", b.name, q.Title)
		return "", false
	}
	ans = strings.TrimSpace(ans)
	if ans == "" {
		b.log.Error("从%s获取答案失败：%s", b.name, q.Title)
		return "", false
	}

	b.log.Info("从%s获取答案：%s -> %s", b.name, q.Title, ans)
	if CheckAnswer(ans, q.Type, b) {
		_ = b.cache.AddCache(q.Title, ans)
		return ans, true
	}
	b.log.Info("从%s获取到的答案类型与题目类型不符，已舍弃", b.name)
	return "", false
}

// QueryAll 批量查询：返回与输入等长的切片，未命中填 ""。
// 先整体过缓存，剩余题目逐题查询（delay 为题目间隔秒数，0 表示不额外等待）。
func (b *baseProvider) QueryAll(qs []Question, delay float64) []string {
	results := make([]string, len(qs))
	if b.disabled {
		return results
	}

	skipCache := b.hasFeedback()
	pending := make([]int, 0, len(qs))
	for i := range qs {
		title := normalizeTitle(qs[i].Title)
		if !skipCache {
			if ans, ok := b.cache.GetCache(title); ok && strings.TrimSpace(ans) != "" {
				ans = strings.TrimSpace(ans)
				b.log.Info("从缓存中获取答案：%s -> %s", title, ans)
				results[i] = ans
				continue
			}
		}
		pending = append(pending, i)
	}

	for _, idx := range pending {
		if delay > 0 {
			time.Sleep(time.Duration(delay * float64(time.Second)))
		}
		q := qs[idx]
		q.Title = normalizeTitle(q.Title)
		if ans, ok := b.Query(q); ok {
			results[idx] = ans
		}
	}
	return results
}

// ---- 标题预处理 ----

var (
	reLeadDigits = regexp.MustCompile(`^\d+`)
	reTailScore  = regexp.MustCompile(`（\d+\.\d+分）$`)
)

// normalizeTitle 去除标题开头的数字编号与结尾的“（x.x分）”分值（对齐 Python 的两条正则）。
func normalizeTitle(title string) string {
	title = reLeadDigits.ReplaceAllString(title, "")
	title = reTailScore.ReplaceAllString(title, "")
	return title
}

// ---- 配置解析辅助 ----

// splitList 按英文逗号切分并去除空项。
func splitList(s string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitListDefault 按英文逗号切分；结果为空时返回 def（def 亦按逗号切分）。
func splitListDefault(s, def string) []string {
	if out := splitList(s); len(out) > 0 {
		return out
	}
	return splitList(def)
}

// parseBool 宽松布尔解析：true/1/yes/y/on 视为真。
func parseBool(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "y", "on":
		return true
	case "false", "0", "no", "n", "off":
		return false
	case "":
		return def
	}
	return def
}

// parseFloat 宽松浮点解析，失败返回 def。
func parseFloat(s string, def float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return v
}

// parseInt 宽松整型解析，失败返回 def。
func parseInt(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

// ---- New ----

// providerCtor 题库构造函数。
type providerCtor func(conf map[string]string, log logger) Provider

// providerRegistry 题库注册表（对应 Python 的 PROVIDER_REGISTRY）。
var providerRegistry = map[string]providerCtor{
	"TikuYanxi":   func(c map[string]string, l logger) Provider { return newYanxi(c, l) },
	"TikuGo":      func(c map[string]string, l logger) Provider { return newGoTiku(c, l) },
	"TikuLike":    func(c map[string]string, l logger) Provider { return newLike(c, l) },
	"TikuAdapter": func(c map[string]string, l logger) Provider { return newAdapter(c, l) },
	"AI":          func(c map[string]string, l logger) Provider { return newAI(c, l) },
	"SiliconFlow": func(c map[string]string, l logger) Provider { return newSiliconFlow(c, l) },
	"TikuManual":  func(c map[string]string, l logger) Provider { return newManual(c, l) },
}

// New 依据 [tiku] 配置构造题库。
//
//   - provider 为空/缺失 -> 返回 Disabled 的 DummyProvider；
//   - 单个 provider -> 对应实例；
//   - 多个（英文逗号分隔）-> FallbackProvider 顺序回退。
//
// 同时初始化 SubmitMode、CoverRate（默认 0.9）、TrueList/FalseList（默认 正确,对,√,是 / 错误,错,×,否,不对,不正确）。
func New(conf map[string]string, log logger) (Provider, error) {
	if log == nil {
		log = nopLogger{}
	}
	if conf == nil {
		conf = map[string]string{}
	}

	names := splitList(conf["provider"])
	if len(names) == 0 {
		log.Error("未找到题库配置, 已忽略题库功能")
		return newDummy(conf, log), nil
	}

	invalid := make([]string, 0, len(names))
	for _, n := range names {
		if _, ok := providerRegistry[n]; !ok {
			invalid = append(invalid, n)
		}
	}
	if len(invalid) > 0 {
		log.Error("题库provider配置无效: %s", strings.Join(invalid, ", "))
		return newDummy(conf, log), nil
	}

	if len(names) == 1 {
		return providerRegistry[names[0]](conf, log), nil
	}

	providers := make([]Provider, 0, len(names))
	for _, n := range names {
		providers = append(providers, providerRegistry[n](conf, log))
	}
	return newFallback(conf, log, providers), nil
}

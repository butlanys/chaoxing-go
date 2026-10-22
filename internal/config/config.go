// Package config 负责 config.ini 的读取与写回。
//
// 对应 Python 原版 /root/chaoxing-ref/main.py 的 load_config_from_file /
// build_config_from_args / str_to_bool，默认值与 config_template.ini 一致。
// 节名与键名大小写不敏感（读取时统一按小写处理）。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/ini.v1"
)

// 各节名（写回时按此顺序输出）
var sectionNames = []string{"common", "tiku", "notification"}

// 默认值，与 config_template.ini + main.py:load_config_from_file 一致
const (
	DefaultSpeed          = 1.0
	MaxSpeed              = 2.0
	MinSpeed              = 1.0
	DefaultJobs           = 4
	DefaultNotOpenAction  = "retry"
	DefaultRetryInterval  = 1.0
	DefaultWorkMaxRetries = 3
	DefaultTargetCount    = 100
	DefaultCoverRate      = 0.9
	DefaultDelay          = 1.0
)

// Common 对应 [common] 节
type Common struct {
	UseCookies       bool     `json:"use_cookies"`        // 是否使用 cookies 登录
	Username         string   `json:"username"`           // 手机号账号
	Password         string   `json:"password"`           // 登录密码
	CourseList       []string `json:"course_list"`        // 要学习的课程 ID 列表
	Speed            float64  `json:"speed"`              // 视频播放倍速，取值 [1.0, 2.0]
	Jobs             int      `json:"jobs"`               // 同时进行的章节数量
	NotOpenAction    string   `json:"notopen_action"`     // retry|ask|continue
	RetryInterval    float64  `json:"retry_interval"`     // 重试等待时间(秒)
	WorkRedoEnabled  bool     `json:"work_redo_enabled"`  // 章节检测答错后是否自动重做
	WorkMaxRetries   int      `json:"work_max_retries"`   // 章节检测最大重做次数
	AddLearningCount bool     `json:"add_learning_count"` // 是否增加章节学习次数
	TargetCount      int      `json:"target_count"`       // 章节学习次数目标总次数
}

// Config 完整配置
type Config struct {
	Common       Common            `json:"common"`
	Tiku         map[string]string `json:"tiku"`         // [tiku] 节原始键值（键小写）
	Notification map[string]string `json:"notification"` // [notification] 节原始键值（键小写）
	Path         string            `json:"path"`         // 来源 ini 路径
}

// [tiku] 节键的规范顺序（写回时优先按此顺序输出，其余键按字典序追加）
var tikuKeyOrder = []string{
	"provider", "check_llm_connection", "submit", "cover_rate", "delay", "tokens",
	"likeapi_search", "likeapi_vision", "likeapi_model", "likeapi_retry", "likeapi_retry_times",
	"url", "go_authorization", "go_min_interval", "go_retry_times", "go_retry_backoff",
	"endpoint", "key", "model", "min_interval_seconds", "http_proxy",
	"siliconflow_key", "siliconflow_model", "siliconflow_endpoint",
	"manual_mode_default", "manual_mode_separator", "true_list", "false_list",
	"ocr_url",
}

// [notification] 节键的规范顺序
var notificationKeyOrder = []string{"provider", "url", "tg_chat_id"}

// [common] 节键的规范顺序
var commonKeyOrder = []string{
	"use_cookies", "username", "password", "course_list", "speed", "jobs",
	"notopen_action", "retry_interval", "work_redo_enabled", "work_max_retries",
	"add_learning_count", "target_count",
}

// StrToBool 复刻 Python 版 main.py:str_to_bool
// 仅 "1"/"true"/"yes"/"y"/"on"（忽略大小写与首尾空白）为真，其余为假。
func StrToBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}

// clampSpeed 把倍速限制在 [1.0, 2.0]
func clampSpeed(v float64) float64 {
	if v < MinSpeed {
		return MinSpeed
	}
	if v > MaxSpeed {
		return MaxSpeed
	}
	return v
}

// formatFloat 格式化浮点数：整数值保留一位小数（1 -> "1.0"），与模板风格一致
func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// parseFloat 解析浮点数，失败时返回默认值
func parseFloat(s string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return f
	}
	return def
}

// parseCommaList 逗号切分并去空白、丢弃空项
func parseCommaList(s string) []string {
	out := []string{}
	for _, item := range strings.Split(s, ",") {
		if v := strings.TrimSpace(item); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Default 返回与 config_template.ini 等价的默认配置
func Default() *Config {
	return &Config{
		Common: Common{
			UseCookies:       false,
			Username:         "xxx",
			Password:         "xxx",
			CourseList:       []string{"xxx", "xxx", "xxx"},
			Speed:            DefaultSpeed,
			Jobs:             DefaultJobs,
			NotOpenAction:    DefaultNotOpenAction,
			RetryInterval:    DefaultRetryInterval,
			WorkRedoEnabled:  false,
			WorkMaxRetries:   DefaultWorkMaxRetries,
			AddLearningCount: false,
			TargetCount:      DefaultTargetCount,
		},
		Tiku: map[string]string{
			"provider":              "TikuYanxi",
			"check_llm_connection":  "true",
			"submit":                "false",
			"cover_rate":            "0.9",
			"delay":                 "1.0",
			"tokens":                "",
			"likeapi_search":        "false",
			"likeapi_vision":        "true",
			"likeapi_model":         "glm-4.5-air",
			"likeapi_retry":         "true",
			"likeapi_retry_times":   "3",
			"url":                   "",
			"go_authorization":      "",
			"go_min_interval":       "1.0",
			"go_retry_times":        "3",
			"go_retry_backoff":      "1.2",
			"endpoint":              "",
			"key":                   "",
			"model":                 "",
			"min_interval_seconds":  "3",
			"http_proxy":            "",
			"siliconflow_key":       "",
			"siliconflow_model":     "deepseek-ai/DeepSeek-R1",
			"siliconflow_endpoint":  "https://api.siliconflow.cn/v1/chat/completions",
			"manual_mode_default":   "batch",
			"manual_mode_separator": ";",
			"true_list":             "正确,对,√,是",
			"false_list":            "错误,错,×,否,不对,不正确",
		},
		Notification: map[string]string{
			"provider":   "ServerChan",
			"url":        "",
			"tg_chat_id": "XXXXXX",
		},
	}
}

// Load 从 ini 文件读取配置，缺失的键使用默认值
func Load(path string) (*Config, error) {
	cfg := Default()
	cfg.Path = path
	// 与 Python 版一致：配置文件中没有的账号信息视为空，而不是模板占位符
	cfg.Common.Username = ""
	cfg.Common.Password = ""
	cfg.Common.CourseList = []string{}

	f, err := ini.LoadSources(ini.LoadOptions{
		Insensitive:         true, // 节名/键名统一转小写
		IgnoreInlineComment: true, // 与 Python configparser 默认行为一致：值中的 ; # 不作为注释
	}, path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if sec, err := f.GetSection("common"); err == nil {
		loadCommon(sec, &cfg.Common)
	}

	cfg.Tiku = map[string]string{}
	if sec, err := f.GetSection("tiku"); err == nil {
		cfg.Tiku = sectionToMap(sec)
		// delay / cover_rate 解析为 float，解析失败回退默认值
		for key, def := range map[string]float64{"delay": DefaultDelay, "cover_rate": DefaultCoverRate} {
			cfg.Tiku[key] = formatFloat(parseFloat(cfg.Tiku[key], def))
		}
	}

	cfg.Notification = map[string]string{}
	if sec, err := f.GetSection("notification"); err == nil {
		cfg.Notification = sectionToMap(sec)
	}

	return cfg, nil
}

// loadCommon 读取 [common] 节，缺失或解析失败的键保留默认值
func loadCommon(sec *ini.Section, c *Common) {
	if v, ok := rawValue(sec, "use_cookies"); ok {
		c.UseCookies = StrToBool(v)
	}
	if v, ok := rawValue(sec, "username"); ok {
		c.Username = strings.TrimSpace(v)
	}
	if v, ok := rawValue(sec, "password"); ok {
		c.Password = strings.TrimSpace(v)
	}
	if v, ok := rawValue(sec, "course_list"); ok {
		if v = strings.TrimSpace(v); v != "" {
			c.CourseList = parseCommaList(v)
		} else {
			c.CourseList = []string{}
		}
	}
	if v, ok := rawValue(sec, "speed"); ok {
		c.Speed = parseFloat(v, DefaultSpeed)
	}
	c.Speed = clampSpeed(c.Speed)
	if v, ok := rawValue(sec, "jobs"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			c.Jobs = n
		}
	}
	if v, ok := rawValue(sec, "notopen_action"); ok {
		if v = strings.TrimSpace(v); v != "" {
			c.NotOpenAction = v
		}
	}
	if v, ok := rawValue(sec, "retry_interval"); ok {
		c.RetryInterval = parseFloat(v, DefaultRetryInterval)
	}
	if v, ok := rawValue(sec, "work_redo_enabled"); ok {
		c.WorkRedoEnabled = StrToBool(v)
	}
	// work_max_retries 解析失败也回退 3
	if v, ok := rawValue(sec, "work_max_retries"); ok {
		c.WorkMaxRetries = DefaultWorkMaxRetries
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			c.WorkMaxRetries = n
		}
	}
	if v, ok := rawValue(sec, "add_learning_count"); ok {
		c.AddLearningCount = StrToBool(v)
	}
	if v, ok := rawValue(sec, "target_count"); ok {
		c.TargetCount = DefaultTargetCount
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			c.TargetCount = n
		}
	}
}

// rawValue 读取键的原始字符串；键不存在时返回 ("", false)
func rawValue(sec *ini.Section, key string) (string, bool) {
	k, err := sec.GetKey(strings.ToLower(key))
	if err != nil {
		return "", false
	}
	return k.String(), true
}

// sectionToMap 把节内所有键值转成小写键的 map
func sectionToMap(sec *ini.Section) map[string]string {
	m := map[string]string{}
	for _, k := range sec.Keys() {
		m[strings.ToLower(k.Name())] = k.String()
	}
	return m
}

// TikuGet 读取 [tiku] 配置项（键大小写不敏感），空值或缺失时返回 def
func (c *Config) TikuGet(key, def string) string {
	if c == nil || c.Tiku == nil {
		return def
	}
	if v, ok := c.Tiku[strings.ToLower(key)]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

// TikuFloat 读取 [tiku] 浮点配置项，解析失败返回 def
func (c *Config) TikuFloat(key string, def float64) float64 {
	return parseFloat(c.TikuGet(key, formatFloat(def)), def)
}

// TikuBool 读取 [tiku] 布尔配置项，解析失败返回 def
func (c *Config) TikuBool(key string, def bool) bool {
	v := c.TikuGet(key, "")
	if strings.TrimSpace(v) == "" {
		return def
	}
	return StrToBool(v)
}

// commonMap 把 Common 转成 [common] 节键值
func (c *Config) commonMap() map[string]string {
	return map[string]string{
		"use_cookies":        strconv.FormatBool(c.Common.UseCookies),
		"username":           c.Common.Username,
		"password":           c.Common.Password,
		"course_list":        strings.Join(c.Common.CourseList, ","),
		"speed":              formatFloat(clampSpeed(c.Common.Speed)),
		"jobs":               strconv.Itoa(c.Common.Jobs),
		"notopen_action":     c.Common.NotOpenAction,
		"retry_interval":     formatFloat(c.Common.RetryInterval),
		"work_redo_enabled":  strconv.FormatBool(c.Common.WorkRedoEnabled),
		"work_max_retries":   strconv.Itoa(c.Common.WorkMaxRetries),
		"add_learning_count": strconv.FormatBool(c.Common.AddLearningCount),
		"target_count":       strconv.Itoa(c.Common.TargetCount),
	}
}

// target 返回 节名 -> 键 -> 值 的写回目标
func (c *Config) target() map[string]map[string]string {
	return map[string]map[string]string{
		"common":       c.commonMap(),
		"tiku":         c.Tiku,
		"notification": c.Notification,
	}
}

// preferredKeys 返回某节键的规范输出顺序
func preferredKeys(section string) []string {
	switch section {
	case "common":
		return commonKeyOrder
	case "tiku":
		return tikuKeyOrder
	case "notification":
		return notificationKeyOrder
	}
	return nil
}

// orderedKeys 先按规范顺序输出，其余键按字典序追加
func orderedKeys(section string, m map[string]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(m))
	for _, k := range preferredKeys(section) {
		if _, ok := m[k]; ok && !seen[k] {
			out = append(out, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(m))
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// render 生成完整的 ini 文本
func (c *Config) render() string {
	var b strings.Builder
	for i, name := range sectionNames {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[" + name + "]\n")
		m := c.target()[name]
		for _, k := range orderedKeys(name, m) {
			b.WriteString(k + " = " + m[k] + "\n")
		}
	}
	return b.String()
}

// parseSectionHeader 解析 "[common]" 形式的节头
func parseSectionHeader(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if len(s) < 2 || !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return "", false
	}
	return strings.TrimSpace(s[1 : len(s)-1]), true
}

// parseKeyLine 解析 "key = value" 行，返回小写键名
func parseKeyLine(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, ";") || strings.HasPrefix(s, "#") || strings.HasPrefix(s, "[") {
		return "", false
	}
	idx := strings.IndexAny(s, "=:")
	if idx <= 0 {
		return "", false
	}
	key := strings.TrimSpace(s[:idx])
	if key == "" {
		return "", false
	}
	return strings.ToLower(key), true
}

// mergeInto 在保留原文件注释的前提下就地替换已知键，并把缺失的键追加到对应节末尾
func (c *Config) mergeInto(text string) string {
	target := c.target()
	seen := map[string]map[string]bool{}
	cur := ""
	secStart := 0 // 当前节头在 out 中的位置（用于避免把新增键插到节末尾的空行之后）
	var out []string

	flush := func() {
		m, ok := target[cur]
		if !ok || m == nil {
			return
		}
		if seen[cur] == nil {
			seen[cur] = map[string]bool{}
		}
		missing := make([]string, 0, len(m))
		for _, k := range orderedKeys(cur, m) {
			if !seen[cur][k] {
				missing = append(missing, k+" = "+m[k])
				seen[cur][k] = true
			}
		}
		if len(missing) == 0 {
			return
		}
		// 先把节末尾的空行摘下，保证新增键紧贴节内容
		var tail []string
		for len(out) > secStart && strings.TrimSpace(out[len(out)-1]) == "" {
			tail = append([]string{out[len(out)-1]}, tail...)
			out = out[:len(out)-1]
		}
		out = append(out, missing...)
		out = append(out, tail...)
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if name, ok := parseSectionHeader(line); ok {
			flush() // 离开当前节前补齐缺失键
			cur = strings.ToLower(name)
			if seen[cur] == nil {
				seen[cur] = map[string]bool{}
			}
			out = append(out, line)
			secStart = len(out)
			continue
		}
		if key, ok := parseKeyLine(line); ok {
			if m, ok2 := target[cur]; ok2 && m != nil {
				if v, exists := m[key]; exists {
					out = append(out, key+" = "+v)
					seen[cur][key] = true
					continue
				}
			}
		}
		out = append(out, line)
	}
	flush() // 文件末尾的节

	// 原文件中完全没有的节，整体追加
	for _, name := range sectionNames {
		if seen[name] != nil {
			continue
		}
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, "["+name+"]")
		for _, k := range orderedKeys(name, target[name]) {
			out = append(out, k+" = "+target[name][k])
		}
	}

	// 统一保留一个结尾换行
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(append(out, ""), "\n")
}

// Save 写回 ini 文件（权限 0644）。
//
// 若文件已存在，则逐行就地替换已知键、保留原有注释；原文件中不存在的键追加到对应节末尾。
func (c *Config) Save(path string) error {
	if path == "" {
		path = c.Path
	}
	if path == "" {
		return errors.New("未指定配置文件路径")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建配置目录失败: %w", err)
		}
	}

	if old, err := os.ReadFile(path); err == nil {
		return os.WriteFile(path, []byte(c.mergeInto(string(old))), 0o644)
	}
	return os.WriteFile(path, []byte(c.render()), 0o644)
}

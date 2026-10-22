package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestStrToBool 覆盖 "1/true/yes/y/on/TRUE/其他" 的解析
func TestStrToBool(t *testing.T) {
	trues := []string{"1", "true", "yes", "y", "on", "TRUE", "True", "  yes  ", "ON", "Y"}
	for _, s := range trues {
		if !StrToBool(s) {
			t.Errorf("StrToBool(%q) = false, 期望 true", s)
		}
	}
	falses := []string{"", "0", "false", "no", "n", "off", "其他", "2", "yesno", "tru"}
	for _, s := range falses {
		if StrToBool(s) {
			t.Errorf("StrToBool(%q) = true, 期望 false", s)
		}
	}
}

// TestLoadDefaults 缺失的键回退默认值
func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("testdata/partial.ini")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	c := cfg.Common
	if c.Username != "13900000000" {
		t.Errorf("username = %q", c.Username)
	}
	if c.Speed != DefaultSpeed {
		t.Errorf("speed = %v, 期望 %v", c.Speed, DefaultSpeed)
	}
	if c.Jobs != DefaultJobs {
		t.Errorf("jobs = %v, 期望 %v", c.Jobs, DefaultJobs)
	}
	if c.NotOpenAction != DefaultNotOpenAction {
		t.Errorf("notopen_action = %q, 期望 %q", c.NotOpenAction, DefaultNotOpenAction)
	}
	if c.RetryInterval != DefaultRetryInterval {
		t.Errorf("retry_interval = %v, 期望 %v", c.RetryInterval, DefaultRetryInterval)
	}
	if c.WorkMaxRetries != DefaultWorkMaxRetries {
		t.Errorf("work_max_retries = %v, 期望 %v", c.WorkMaxRetries, DefaultWorkMaxRetries)
	}
	if c.TargetCount != DefaultTargetCount {
		t.Errorf("target_count = %v, 期望 %v", c.TargetCount, DefaultTargetCount)
	}
	if len(c.CourseList) != 0 {
		t.Errorf("course_list = %v, 期望空", c.CourseList)
	}
	if c.UseCookies || c.WorkRedoEnabled || c.AddLearningCount {
		t.Errorf("布尔项默认应为 false: %+v", c)
	}
	if cfg.Path != "testdata/partial.ini" {
		t.Errorf("path = %q", cfg.Path)
	}
}

// TestLoadTypesAndClamp 类型转换、逗号切分、speed 上限
func TestLoadTypesAndClamp(t *testing.T) {
	cfg, err := Load("testdata/basic.ini")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	c := cfg.Common
	if !c.UseCookies {
		t.Error("use_cookies 应为 true (YES)")
	}
	if c.Username != "13800000000" {
		t.Errorf("username 应去掉空白, 实际 %q", c.Username)
	}
	if !reflect.DeepEqual(c.CourseList, []string{"101", "102", "103", "104"}) {
		t.Errorf("course_list = %v", c.CourseList)
	}
	if c.Speed != MaxSpeed {
		t.Errorf("speed = %v, 期望被 clamp 到 %v", c.Speed, MaxSpeed)
	}
	if c.Jobs != 2 {
		t.Errorf("jobs = %v", c.Jobs)
	}
	if c.NotOpenAction != "continue" {
		t.Errorf("notopen_action = %q", c.NotOpenAction)
	}
	if c.RetryInterval != 0.5 {
		t.Errorf("retry_interval = %v", c.RetryInterval)
	}
	if !c.WorkRedoEnabled {
		t.Error("work_redo_enabled 应为 true (on)")
	}
	if c.WorkMaxRetries != 3 {
		t.Errorf("work_max_retries 解析失败应回退 3, 实际 %v", c.WorkMaxRetries)
	}
	if c.AddLearningCount {
		t.Error("add_learning_count 应为 false (0)")
	}
	if c.TargetCount != 50 {
		t.Errorf("target_count = %v", c.TargetCount)
	}

	// tiku: delay/cover_rate 解析为 float
	if got := cfg.Tiku["delay"]; got != "2.5" {
		t.Errorf("tiku delay = %q", got)
	}
	if got := cfg.Tiku["cover_rate"]; got != "0.75" {
		t.Errorf("tiku cover_rate = %q", got)
	}
	if got := cfg.TikuFloat("cover_rate", 0.9); got != 0.75 {
		t.Errorf("TikuFloat(cover_rate) = %v", got)
	}
	if got := cfg.TikuFloat("不存在的键", 0.9); got != 0.9 {
		t.Errorf("TikuFloat 缺失键应返回默认值, 实际 %v", got)
	}
	if got := cfg.TikuGet("provider", ""); got != "TikuGo,TikuYanxi" {
		t.Errorf("TikuGet(provider) = %q", got)
	}
	if cfg.TikuBool("go_retry_times", false) {
		t.Error("go_retry_times=5 不是合法布尔值, 期望 false")
	}
	if !cfg.TikuBool("missing", true) {
		t.Error("缺失键应返回默认值 true")
	}
	if got := cfg.Notification["provider"]; got != "Bark" {
		t.Errorf("notification provider = %q", got)
	}
}

// TestLoadCaseInsensitive 节名/键名大小写不敏感
func TestLoadCaseInsensitive(t *testing.T) {
	cfg, err := Load("testdata/upper.ini")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Common.Speed != 1.5 {
		t.Errorf("speed = %v", cfg.Common.Speed)
	}
	if cfg.Common.Jobs != 8 {
		t.Errorf("jobs = %v", cfg.Common.Jobs)
	}
	if !reflect.DeepEqual(cfg.Common.CourseList, []string{"111", "222"}) {
		t.Errorf("course_list = %v", cfg.Common.CourseList)
	}
	if !cfg.Common.UseCookies {
		t.Error("use_cookies 应为 true")
	}
	if got := cfg.Tiku["likeapi_search"]; got != "true" {
		t.Errorf("likeapi_search = %q", got)
	}
	if !cfg.TikuBool("likeapi_search", false) {
		t.Error("TikuBool(likeapi_search) 应为 true")
	}
	if cfg.Notification["url"] != "https://qmsg.zendee.cn/send/abcd" {
		t.Errorf("notification url = %q", cfg.Notification["url"])
	}
}

// TestSpeedLowerClamp 低于 1 的倍速被抬到 1
func TestSpeedLowerClamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.ini")
	if err := os.WriteFile(path, []byte("[common]\nspeed = 0.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Common.Speed != MinSpeed {
		t.Errorf("speed = %v, 期望 %v", cfg.Common.Speed, MinSpeed)
	}
}

// TestLoadMissingFile 文件不存在时返回 error
func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("testdata/不存在的文件.ini"); err == nil {
		t.Fatal("文件不存在时 Load 应返回 error")
	}
}

// TestDefaultMatchesTemplate Default() 与 config_template.ini 等价
func TestDefaultMatchesTemplate(t *testing.T) {
	loaded, err := Load("../../config_template.ini")
	if err != nil {
		t.Fatalf("Load 模板失败: %v", err)
	}
	def := Default()
	if !reflect.DeepEqual(def.Common, loaded.Common) {
		t.Errorf("Common 不一致:\n默认 = %+v\n模板 = %+v", def.Common, loaded.Common)
	}
	if !reflect.DeepEqual(def.Tiku, loaded.Tiku) {
		for k := range def.Tiku {
			if def.Tiku[k] != loaded.Tiku[k] {
				t.Errorf("tiku[%s] 默认=%q 模板=%q", k, def.Tiku[k], loaded.Tiku[k])
			}
		}
		for k := range loaded.Tiku {
			if _, ok := def.Tiku[k]; !ok {
				t.Errorf("tiku 模板多出键 %s=%q", k, loaded.Tiku[k])
			}
		}
	}
	if !reflect.DeepEqual(def.Notification, loaded.Notification) {
		t.Errorf("Notification 不一致: 默认=%v 模板=%v", def.Notification, loaded.Notification)
	}
}

// TestSaveRoundTrip Save 后再 Load 能还原配置
func TestSaveRoundTrip(t *testing.T) {
	cfg, err := Load("testdata/basic.ini")
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	out := filepath.Join(t.TempDir(), "sub", "config.ini")
	if err := cfg.Save(out); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("文件权限 = %v, 期望 0644", info.Mode().Perm())
	}

	again, err := Load(out)
	if err != nil {
		t.Fatalf("重新 Load 失败: %v", err)
	}
	if !reflect.DeepEqual(cfg.Common, again.Common) {
		t.Errorf("Common 未能还原:\n之前 = %+v\n之后 = %+v", cfg.Common, again.Common)
	}
	if !reflect.DeepEqual(cfg.Tiku, again.Tiku) {
		t.Errorf("Tiku 未能还原:\n之前 = %v\n之后 = %v", cfg.Tiku, again.Tiku)
	}
	if !reflect.DeepEqual(cfg.Notification, again.Notification) {
		t.Errorf("Notification 未能还原:\n之前 = %v\n之后 = %v", cfg.Notification, again.Notification)
	}

	// 修改后再写回，值应更新
	again.Common.Speed = 1.5
	again.Common.Username = "newuser"
	again.Tiku["provider"] = "TikuLike"
	if err := again.Save(out); err != nil {
		t.Fatalf("第二次 Save 失败: %v", err)
	}
	third, err := Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if third.Common.Speed != 1.5 || third.Common.Username != "newuser" || third.Tiku["provider"] != "TikuLike" {
		t.Errorf("就地替换失败: %+v %v", third.Common, third.Tiku)
	}
}

// TestSaveKeepsCommentsAndUnknownKeys 就地替换保留注释与未知键，缺失键追加到节末尾
func TestSaveKeepsCommentsAndUnknownKeys(t *testing.T) {
	src, err := os.ReadFile("testdata/commented.ini")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.ini")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	cfg.Common.Username = "newuser"
	cfg.Common.Password = "newpass"
	cfg.Common.Speed = 2
	cfg.Tiku["delay"] = "3.5"
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"; 这是原有注释，必须保留",
		"; 账号相关",
		"; 题库注释",
		"unknown_key = keepme",
		"username = newuser",
		"speed = 2.0",
		"delay = 3.5",
		"[common]",
		"[tiku]",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("写回结果缺少 %q\n---\n%s", want, text)
		}
	}
	if strings.Contains(text, "olduser") || strings.Contains(text, "oldpass") {
		t.Errorf("旧值未被替换:\n%s", text)
	}
	// 原文件没有 [notification] 节，应被整体追加
	if !strings.Contains(text, "[notification]") {
		t.Errorf("缺失的节未被追加:\n%s", text)
	}

	reload, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reload.Common.Username != "newuser" || reload.Common.Speed != 2 {
		t.Errorf("重新加载不一致: %+v", reload.Common)
	}
	// 未在模板中的键（unknown_key）保留但不会被解析进结构体
	if reload.Tiku["delay"] != "3.5" {
		t.Errorf("tiku delay = %q", reload.Tiku["delay"])
	}
}

// TestSaveEmptyPath 空路径报错
func TestSaveEmptyPath(t *testing.T) {
	cfg := Default()
	if err := cfg.Save(""); err == nil {
		t.Fatal("空路径应返回 error")
	}
}

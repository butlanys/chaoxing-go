// Package logx 提供带账号标签、可挂载多出口(Sink)的日志设施。
//
// 与原版 loguru 的用法对应: CLI 下打印到 stderr, GUI 下通过 Sink 推送 Wails event。
package logx

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Level 日志级别
type Level int

// 级别定义，顺序与 Python 版语义一致
const (
	LevelTrace Level = iota
	LevelDebug
	LevelInfo
	LevelSuccess
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelTrace:   "trace",
	LevelDebug:   "debug",
	LevelInfo:    "info",
	LevelSuccess: "success",
	LevelWarn:    "warn",
	LevelError:   "error",
}

// String 返回级别小写名
func (l Level) String() string {
	if s, ok := levelNames[l]; ok {
		return s
	}
	return "info"
}

// ParseLevel 解析级别字符串，无法识别时返回 LevelInfo
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return LevelTrace
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "success":
		return LevelSuccess
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	}
	return LevelInfo
}

// Entry 一条日志记录，可直接 JSON 序列化给前端
type Entry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Account string    `json:"account"`
	Msg     string    `json:"msg"`
}

// Sink 日志出口
type Sink func(Entry)

var (
	mu       sync.RWMutex
	sinks    []Sink
	minLevel = LevelInfo
)

// AddSink 注册一个日志出口
func AddSink(s Sink) {
	if s == nil {
		return
	}
	mu.Lock()
	sinks = append(sinks, s)
	mu.Unlock()
}

// SetLevel 设置全局最低输出级别
func SetLevel(l Level) {
	mu.Lock()
	minLevel = l
	mu.Unlock()
}

// GetLevel 读取当前最低输出级别
func GetLevel() Level {
	mu.RLock()
	defer mu.RUnlock()
	return minLevel
}

func emit(e Entry) {
	mu.RLock()
	lv := minLevel
	snapshot := make([]Sink, len(sinks))
	copy(snapshot, sinks)
	mu.RUnlock()

	if levelValue(e.Level) < lv {
		return
	}
	for _, s := range snapshot {
		s(e)
	}
}

func levelValue(name string) Level {
	switch name {
	case "trace":
		return LevelTrace
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "success":
		return LevelSuccess
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	}
	return LevelInfo
}

// Logger 带账号标签的日志器
type Logger struct {
	account string
}

// New 构造日志器，account 为账号标识（手机号/备注），空串表示全局
func New(account string) *Logger { return &Logger{account: account} }

// Default 返回全局日志器
func Default() *Logger { return &Logger{} }

// With 派生一个带新账号标签的日志器
func (l *Logger) With(account string) *Logger { return &Logger{account: account} }

// Account 返回当前账号标签
func (l *Logger) Account() string { return l.account }

func (l *Logger) logf(level Level, format string, a ...any) {
	msg := format
	if len(a) > 0 {
		msg = fmt.Sprintf(format, a...)
	}
	emit(Entry{Time: time.Now(), Level: level.String(), Account: l.account, Msg: msg})
}

// Trace 输出 trace 级别日志
func (l *Logger) Trace(format string, a ...any) { l.logf(LevelTrace, format, a...) }

// Debug 输出 debug 级别日志
func (l *Logger) Debug(format string, a ...any) { l.logf(LevelDebug, format, a...) }

// Info 输出 info 级别日志
func (l *Logger) Info(format string, a ...any) { l.logf(LevelInfo, format, a...) }

// Success 输出 success 级别日志
func (l *Logger) Success(format string, a ...any) { l.logf(LevelSuccess, format, a...) }

// Warn 输出 warn 级别日志
func (l *Logger) Warn(format string, a ...any) { l.logf(LevelWarn, format, a...) }

// Error 输出 error 级别日志
func (l *Logger) Error(format string, a ...any) { l.logf(LevelError, format, a...) }

// StderrSink 返回一个把日志写到 stderr 的出口（CLI 用）
func StderrSink() Sink {
	var outMu sync.Mutex
	return func(e Entry) {
		outMu.Lock()
		defer outMu.Unlock()
		prefix := ""
		if e.Account != "" {
			prefix = "[" + e.Account + "] "
		}
		fmt.Fprintf(os.Stderr, "%s %-7s %s%s\n", e.Time.Format("15:04:05"), strings.ToUpper(e.Level), prefix, e.Msg)
	}
}

// MemorySink 返回一个把最近 n 条日志留在内存中的出口（GUI 首屏回填用）
func MemorySink(n int) (Sink, func() []Entry) {
	var m sync.Mutex
	buf := make([]Entry, 0, n)
	sink := func(e Entry) {
		m.Lock()
		buf = append(buf, e)
		if len(buf) > n {
			buf = buf[len(buf)-n:]
		}
		m.Unlock()
	}
	get := func() []Entry {
		m.Lock()
		defer m.Unlock()
		out := make([]Entry, len(buf))
		copy(out, buf)
		return out
	}
	return sink, get
}

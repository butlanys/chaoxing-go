package tiku

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// testLogger 记录日志的测试用日志器。
type testLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (l *testLogger) add(level, format string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, level+": "+fmt.Sprintf(format, a...))
}

func (l *testLogger) Trace(f string, a ...any)   { l.add("trace", f, a...) }
func (l *testLogger) Debug(f string, a ...any)   { l.add("debug", f, a...) }
func (l *testLogger) Info(f string, a ...any)    { l.add("info", f, a...) }
func (l *testLogger) Warn(f string, a ...any)    { l.add("warn", f, a...) }
func (l *testLogger) Error(f string, a ...any)   { l.add("error", f, a...) }
func (l *testLogger) Success(f string, a ...any) { l.add("success", f, a...) }

// contains 判断是否记录了包含 sub 的日志。
func (l *testLogger) contains(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, m := range l.msgs {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

// count 返回包含 sub 的日志条数。
func (l *testLogger) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, m := range l.msgs {
		if strings.Contains(m, sub) {
			n++
		}
	}
	return n
}

// tmpCache 返回一个落在临时目录下的缓存，避免污染工作目录。
func tmpCache(t *testing.T) *Cache {
	t.Helper()
	return NewCache(filepath.Join(t.TempDir(), "cache.json"))
}

package tiku

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCacheWriteRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	c := NewCache(path)

	if _, ok := c.GetCache("不存在的题目"); ok {
		t.Fatal("未写入的题目不应命中")
	}

	if err := c.AddCache("题目一", "答案一"); err != nil {
		t.Fatalf("AddCache 失败: %v", err)
	}
	if err := c.AddCache("题目二", "答案二"); err != nil {
		t.Fatalf("AddCache 失败: %v", err)
	}

	if v, ok := c.GetCache("题目一"); !ok || v != "答案一" {
		t.Fatalf("GetCache 命中错误: %q %v", v, ok)
	}

	// 落盘内容应为合法 JSON map，且中文不转义
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取缓存文件失败: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("缓存文件不是合法 JSON: %v", err)
	}
	if m["题目二"] != "答案二" {
		t.Fatalf("落盘内容缺失: %#v", m)
	}
	if !strings.Contains(string(raw), "题目二") {
		t.Fatal("缓存文件应保留原始中文（不转义）")
	}

	// 重新打开应能读到旧数据
	c2 := NewCache(path)
	if v, ok := c2.GetCache("题目一"); !ok || v != "答案一" {
		t.Fatalf("重新加载缓存失败: %q %v", v, ok)
	}

	// 目录不存在时也能自动创建
	nested := filepath.Join(t.TempDir(), "a", "b", "cache.json")
	c3 := NewCache(nested)
	if err := c3.AddCache("q", "a"); err != nil {
		t.Fatalf("嵌套目录写入失败: %v", err)
	}
}

func TestCacheConcurrent(t *testing.T) {
	c := NewCache(filepath.Join(t.TempDir(), "cache.json"))
	const workers = 100
	const perWorker = 10

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				key := fmt.Sprintf("q-%d-%d", id, j)
				val := fmt.Sprintf("a-%d-%d", id, j)
				if err := c.AddCache(key, val); err != nil {
					t.Errorf("并发 AddCache 失败: %v", err)
					return
				}
				if v, ok := c.GetCache(key); ok && v != val {
					t.Errorf("并发读数据不一致: %s -> %q, want %q", key, v, val)
				}
				_, _ = c.GetCache("missing")
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		for j := 0; j < perWorker; j++ {
			key := fmt.Sprintf("q-%d-%d", i, j)
			val := fmt.Sprintf("a-%d-%d", i, j)
			if v, ok := c.GetCache(key); !ok || v != val {
				t.Fatalf("最终数据不一致: %s -> %q %v, want %q", key, v, ok, val)
			}
		}
	}
}

func TestCacheCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	if err := os.WriteFile(path, []byte("这不是合法的 JSON 内容"), 0o644); err != nil {
		t.Fatal(err)
	}

	log := &testLogger{}
	c := NewCache(path)
	c.SetLogger(log)

	if v, ok := c.GetCache("q"); ok {
		t.Fatalf("损坏文件应返回空缓存, 却命中 %q", v)
	}
	if !log.contains("已损坏") {
		t.Error("应记录缓存损坏日志")
	}

	// 备份文件应存在
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "cache.json.bak.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("未生成备份文件: %v", entries)
	}

	// 损坏后仍可继续写入
	if err := c.AddCache("新题", "新答案"); err != nil {
		t.Fatalf("损坏后写入失败: %v", err)
	}
	if v, ok := c.GetCache("新题"); !ok || v != "新答案" {
		t.Fatalf("损坏后写入读回失败: %q %v", v, ok)
	}
}

func TestCacheRecoveryFromGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	// 前后有垃圾但中间是合法 JSON，应能恢复
	if err := os.WriteFile(path, []byte("garbage {\"题目\":\"答案\"} trailing"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewCache(path)
	if v, ok := c.GetCache("题目"); !ok || v != "答案" {
		t.Fatalf("应从垃圾文本中恢复出 JSON: %q %v", v, ok)
	}
}

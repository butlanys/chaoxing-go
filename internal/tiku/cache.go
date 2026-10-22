package tiku

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultCacheFile 默认缓存文件名（对应 Python 的 CacheDAO.DEFAULT_CACHE_FILE）。
const DefaultCacheFile = "cache.json"

// Cache 题目缓存（对应 Python 的 CacheDAO）。
//
// 以 JSON map[题目文本]答案 的形式落盘；内存中持有完整映射，读写受 sync.RWMutex 保护；
// 落盘采用「临时文件 + fsync + os.Rename」原子替换；JSON 损坏时备份为 cache.json.bak.<unix>
// 并返回空缓存（绝不 panic）。
type Cache struct {
	path string
	log  logger

	once sync.Once
	mu   sync.RWMutex
	data map[string]string
}

// NewCache 构造一个以 path 为落盘位置的缓存；path 为空时使用 DefaultCacheFile。
// 注意：与 Python 不同，这里不会在构造时立刻创建文件，而是在首次写入时创建。
func NewCache(path string) *Cache {
	if strings.TrimSpace(path) == "" {
		path = DefaultCacheFile
	}
	return &Cache{path: path, log: nopLogger{}, data: map[string]string{}}
}

// SetLogger 注入日志器（可选）。
func (c *Cache) SetLogger(l logger) {
	if l != nil {
		c.log = l
	}
}

// Path 返回缓存文件路径。
func (c *Cache) Path() string { return c.path }

// loadOnce 惰性加载落盘内容（只执行一次）。
func (c *Cache) loadOnce() {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		// 文件不存在或不可读，使用空缓存继续
		c.data = map[string]string{}
		return
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err == nil {
		if m == nil {
			m = map[string]string{}
		}
		c.data = m
		return
	}

	// JSON 解析失败，尝试从原始文本中恢复有效 JSON 段（对齐 Python 的恢复策略）
	text := string(raw)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end != -1 && start < end {
		var recovered map[string]string
		if err := json.Unmarshal([]byte(text[start:end+1]), &recovered); err == nil {
			c.log.Error("缓存文件 JSON 解析失败，已从原始文本中恢复部分内容: %s", c.path)
			if recovered == nil {
				recovered = map[string]string{}
			}
			c.data = recovered
			return
		}
	}

	// 无法恢复：备份损坏文件并返回空缓存
	bakName := fmt.Sprintf("%s.bak.%d", filepath.Base(c.path), time.Now().Unix())
	bakPath := filepath.Join(filepath.Dir(c.path), bakName)
	if err := os.WriteFile(bakPath, raw, 0o644); err != nil {
		c.log.Error("备份损坏缓存失败: %v", err)
	} else {
		c.log.Error("缓存文件已损坏，已备份为: %s，将使用空缓存继续运行", bakPath)
	}
	c.data = map[string]string{}
}

// GetCache 读取缓存；第二个返回值为是否命中。
func (c *Cache) GetCache(question string) (string, bool) {
	c.once.Do(c.loadOnce)
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[question]
	return v, ok
}

// AddCache 写入缓存并原子落盘。
func (c *Cache) AddCache(question, answer string) error {
	c.once.Do(c.loadOnce)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = map[string]string{}
	}
	c.data[question] = answer
	return c.writeLocked()
}

// writeLocked 在持有写锁的情况下原子写盘（临时文件 + fsync + rename）。
func (c *Cache) writeLocked() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.log.Error("创建缓存目录失败: %v", err)
		return err
	}

	buf, err := json.MarshalIndent(c.data, "", "    ")
	if err != nil {
		c.log.Error("序列化缓存失败: %v", err)
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(c.path)+".tmp*")
	if err != nil {
		c.log.Error("创建缓存临时文件失败: %v", err)
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		c.log.Error("写入缓存失败: %v", err)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		c.log.Error("缓存 fsync 失败: %v", err)
		return err
	}
	if err := tmp.Close(); err != nil {
		c.log.Error("关闭缓存临时文件失败: %v", err)
		return err
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		c.log.Error("原子替换缓存文件失败: %v", err)
		return err
	}
	return nil
}

// defaultCache 包级共享缓存（对应 Python 各处 new CacheDAO() 使用工作目录下的 cache.json）。
var (
	defaultCacheOnce sync.Once
	defaultCacheVal  *Cache
)

func defaultCache() *Cache {
	defaultCacheOnce.Do(func() {
		defaultCacheVal = NewCache(DefaultCacheFile)
	})
	return defaultCacheVal
}

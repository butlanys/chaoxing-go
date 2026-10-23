package font

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TableName 字体映射表文件名
const TableName = "font_map_table.json"

// Table 是 resource/font_map_table.json 的加载结果，提供 char<->hash 双向查询。
// 对应 Python 版 api/cxsecret_font.py 的 FontHashDAO。
type Table struct {
	charMap map[string]string // 字形名(如 "uni4E00"/"A") -> hash
	hashMap map[string]string // hash -> 字形名
	path    string            // 实际加载的文件路径
}

// LoadTable 加载字体映射表。
// 路径解析顺序（对齐原版 resource_path + 打包环境兼容）：
//  1. 传入的 path（非空且存在）
//  2. <可执行文件目录>/resource/font_map_table.json
//  3. ./resource/font_map_table.json
//  4. ../../resource/font_map_table.json
//  5. 编译期内嵌的映射表（//go:embed，保证单文件可执行程序开箱可用）
//
// 候选文件不存在时继续尝试下一个；一旦命中但内容非法，直接返回错误（与原版一致，不做静默兜底）。
func LoadTable(path string) (*Table, error) {
	candidates := tableCandidates(path)
	var lastErr error
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			lastErr = err
			continue
		}
		t, err := loadTableFile(p)
		if err != nil {
			return nil, fmt.Errorf("加载字体映射表失败: %s - %w", p, err)
		}
		return t, nil
	}
	// 磁盘上都没有 -> 回退到内嵌映射表
	if len(embeddedTableJSON) > 0 {
		t, err := loadTableBytes(embeddedTableJSON, embeddedTableSource)
		if err != nil {
			return nil, fmt.Errorf("加载内嵌字体映射表失败: %w", err)
		}
		return t, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("未找到 %s", TableName)
	}
	return nil, fmt.Errorf("加载字体映射表失败: %v (已尝试 %v)", lastErr, candidates)
}

// tableCandidates 返回按优先级排列的候选路径。
func tableCandidates(path string) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	add(path)
	if exe, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(exe), "resource", TableName))
	}
	add(filepath.Join(".", "resource", TableName))
	add(filepath.Join("..", "..", "resource", TableName))
	return out
}

// loadTableFile 读取并解析单个 JSON 映射表文件。
func loadTableFile(path string) (*Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadTableBytes(raw, path)
}

// loadTableBytes 解析映射表内容；source 仅用于错误信息与 Path() 展示。
func loadTableBytes(raw []byte, source string) (*Table, error) {
	keys, values, err := decodeOrderedStringMap(raw)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", source, err)
	}
	charMap := make(map[string]string, len(keys))
	hashMap := make(map[string]string, len(keys))
	for i, k := range keys {
		charMap[k] = values[i]
		// 与原版一致: {hash: char for char, hash in char_map.items()}
		// 同一个 hash 出现多次时，按文件中的先后顺序「后者覆盖前者」（与 Python dict 推导式相同），
		// 因此这里用流式解析保留 JSON 键顺序，避免 Go map 随机遍历带来的不确定性。
		hashMap[values[i]] = k
	}
	return &Table{charMap: charMap, hashMap: hashMap, path: source}, nil
}

// decodeOrderedStringMap 按 JSON 中的原始顺序解析 {"k": "v", ...}，返回键值切片。
func decodeOrderedStringMap(raw []byte) ([]string, []string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, nil, fmt.Errorf("顶层不是 JSON 对象")
	}
	var keys, values []string
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := kt.(string)
		if !ok {
			return nil, nil, fmt.Errorf("键不是字符串: %v", kt)
		}
		var val string
		if err := dec.Decode(&val); err != nil {
			return nil, nil, fmt.Errorf("键 %q 的值不是字符串: %w", key, err)
		}
		keys = append(keys, key)
		values = append(values, val)
	}
	if _, err := dec.Token(); err != nil { // 消费结尾的 '}'
		return nil, nil, err
	}
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("映射表为空")
	}
	return keys, values, nil
}

// FindChar 通过字体哈希值查找对应的 Unicode 字形名（如 "uni4E00"）。
// 对齐 Python FontHashDAO.find_char。
func (t *Table) FindChar(hash string) (string, bool) {
	if t == nil || t.hashMap == nil {
		return "", false
	}
	c, ok := t.hashMap[hash]
	return c, ok
}

// FindHash 通过字形名查找对应的字体哈希值。
// 对齐 Python FontHashDAO.find_hash。
func (t *Table) FindHash(char string) (string, bool) {
	if t == nil || t.charMap == nil {
		return "", false
	}
	h, ok := t.charMap[char]
	return h, ok
}

// Len 映射表条目数
func (t *Table) Len() int {
	if t == nil {
		return 0
	}
	return len(t.charMap)
}

// Path 实际加载的文件路径
func (t *Table) Path() string {
	if t == nil {
		return ""
	}
	return t.path
}

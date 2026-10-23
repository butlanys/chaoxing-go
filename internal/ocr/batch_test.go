package ocr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestBatchAgreement 对一整个目录的验证码跑识别，把结果写成 JSON 供外部比对。
//
// 用途：量化"与 Python 版 ddddocr 的逐张一致率"这一实际指标。
// 由于 ORT 的 Sigmoid/Tanh 用的是 Eigen 的近似实现，纯 Go 无法做到位级一致，
// 量化 round() 会把 1 ULP 的浮点差放大成个别字符的差异。
// 所以这里不追求逐层一致，而是量化端到端一致率。
//
// 用法: OCR_BATCH_DIR=/path/to/imgs OCR_BATCH_OUT=/tmp/go_results.json \
//         go test -run TestBatchAgreement ./internal/ocr/
func TestBatchAgreement(t *testing.T) {
	dir := os.Getenv("OCR_BATCH_DIR")
	out := os.Getenv("OCR_BATCH_OUT")
	if dir == "" || out == "" {
		t.Skip("未设置 OCR_BATCH_DIR / OCR_BATCH_OUT")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		switch filepath.Ext(e.Name()) {
		case ".jpg", ".jpeg", ".png", ".bmp":
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	res := map[string]string{}
	fails := map[string]string{}
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			fails[n] = err.Error()
			continue
		}
		got, err := r.Recognize(data)
		if err != nil {
			fails[n] = err.Error()
			continue
		}
		res[n] = got.Text
	}
	blob, _ := json.MarshalIndent(map[string]any{"ok": res, "err": fails}, "", " ")
	if err := os.WriteFile(out, blob, 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("已识别 %d 张，失败 %d 张，结果写入 %s\n", len(res), len(fails), out)
}

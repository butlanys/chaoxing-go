package crypto

import (
	"encoding/json"
	"os"
	"testing"
)

// TestZZDumpCrypto 临时交叉验证：导出 Encrypt/VideoEnc 结果供 Python 比对。
func TestZZDumpCrypto(t *testing.T) {
	out := os.Getenv("CHAOXING_CRYPTO_OUT")
	if out == "" {
		t.Skip("未设置 CHAOXING_CRYPTO_OUT")
	}
	cases := []string{"", "13800138000", "中文测试", "0123456789abcdef", "0123456789abcde",
		"u2oh6Vu^HWe4_AES", "密文解码测试：康熙部首⼀⼁⼂"}
	m := map[string]string{}
	for _, c := range cases {
		m[c] = Encrypt(c)
	}
	m["<video1>"] = VideoEnc("123456", "7890123", "987654", "111222333", 125, 500)
	m["<video2>"] = VideoEnc("aaa", "bbb", "ccc", "ddd", 0, 0)
	buf, _ := json.Marshal(m)
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

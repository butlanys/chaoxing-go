package crypto

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// pythonGolden 由 Python 参考实现(pyaes + api/cipher.py)实际运行得到，用作字节级对齐基准。
var pythonGolden = []struct {
	name      string
	plaintext string
	want      string
}{
	{"空串", "", "LXYPGIOJyFUvuG1SPO0nVQ=="},
	{"手机号", "13800138000", "yYcVatS+4J+JGnm88UM56A=="},
	{"中文", "中文测试", "tnJ1vnflpedzPuWwE+9i/A=="},
	{"正好一整块", "0123456789abcdef", "hmUF/tdyZDwfeahGBesCEAAvdr+vfwefCe/qjDPK7dA="},
	{"差一字节满块", "0123456789abcde", "FAiK0B2hDQz0gNMdoOoKlg=="},
	{"长串", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"YaUb/y6VdOUq8BImCZkv2EvFxe2b2JSWYe11G/GzkPIrXoqmOrf5OXGdlEheNBJgjiINhABlh/Hl2pCCnqn0fOfJLm0/yJ40FFY+MSf0biA="},
	{"密钥本身", "u2oh6Vu^HWe4_AES", "ulZQcVrOacDUDizP11W0E4FFPgELu/nYgZFwzeSLFj4="},
	{"含康熙部首", "密文解码测试：康熙部首⼀⼁⼂", "ScdABwjVUyOgeiwanqH7cp6t6eTtLgefQHU6UjT+lLipixWaYAPUT1YZpQOguTpx"},
}

// TestEncryptMatchesPython 与 Python 参考实现输出逐字节比对。
func TestEncryptMatchesPython(t *testing.T) {
	for _, tc := range pythonGolden {
		got := Encrypt(tc.plaintext)
		if got != tc.want {
			t.Errorf("[%s] Encrypt(%q) = %q, 期望 %q", tc.name, tc.plaintext, got, tc.want)
		}
	}
}

// TestEncryptDecryptRoundTrip 往返一致性（校验 IV / padding 正确）。
func TestEncryptDecryptRoundTrip(t *testing.T) {
	for _, tc := range pythonGolden {
		got, err := Decrypt(Encrypt(tc.plaintext))
		if err != nil {
			t.Fatalf("[%s] Decrypt 失败: %v", tc.name, err)
		}
		if got != tc.plaintext {
			t.Errorf("[%s] 往返结果 %q, 期望 %q", tc.name, got, tc.plaintext)
		}
	}
}

// TestEncryptBlockAlignment PKCS7 分组与填充校验：
// 用 crypto/aes 手工按 CBC 规则逐块解密（不依赖 crypto/cipher 的 CBC 实现），
// 检查密文分组、IV 链接与 PKCS7 填充是否都正确。
func TestEncryptBlockAlignment(t *testing.T) {
	key := []byte("u2oh6Vu^HWe4_AES") // 与 consts.AESKey 逐字一致
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{"", "a", "13800138000", "0123456789abcdef", "0123456789abcde",
		"中文测试", string(bytes.Repeat([]byte("x"), 4096))}
	for _, pt := range cases {
		ct := Encrypt(pt)
		raw, err := base64.StdEncoding.DecodeString(ct)
		if err != nil {
			t.Fatalf("Encrypt(%q) 输出不是合法标准 base64: %v", pt, err)
		}
		if len(raw)%aes.BlockSize != 0 {
			t.Errorf("密文长度 %d 未按 %d 字节对齐", len(raw), aes.BlockSize)
		}
		wantLen := (len([]byte(pt))/aes.BlockSize + 1) * aes.BlockSize
		if len(raw) != wantLen {
			t.Errorf("明文 %d 字节 -> 密文 %d 字节, 期望 %d", len([]byte(pt)), len(raw), wantLen)
		}
		// 手工 CBC 解密: P_i = D(C_i) xor C_{i-1}, 首块 xor IV(=key)
		prev := append([]byte{}, key...)
		plain := make([]byte, 0, len(raw))
		for i := 0; i < len(raw); i += aes.BlockSize {
			dec := make([]byte, aes.BlockSize)
			block.Decrypt(dec, raw[i:i+aes.BlockSize])
			for j := 0; j < aes.BlockSize; j++ {
				dec[j] ^= prev[j]
			}
			plain = append(plain, dec...)
			prev = raw[i : i+aes.BlockSize]
		}
		pad := int(plain[len(plain)-1])
		if pad <= 0 || pad > aes.BlockSize {
			t.Fatalf("非法 PKCS7 填充值 %d", pad)
		}
		if !bytes.Equal(plain[len(plain)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
			t.Errorf("PKCS7 填充字节不一致: %v", plain[len(plain)-pad:])
		}
		if got := string(plain[:len(plain)-pad]); got != pt {
			t.Errorf("手工解密结果 %q, 期望 %q", got, pt)
		}
	}
}

// TestEncryptManualCBC 用 crypto/aes 手工按 CBC 规则逐块加密（不依赖 crypto/cipher 的 CBC 实现），
// 独立复算密文，确认分组链接与 IV 取值都正确。
func TestEncryptManualCBC(t *testing.T) {
	key := []byte("u2oh6Vu^HWe4_AES") // 与 consts.AESKey 逐字一致
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range pythonGolden {
		// 手工 PKCS7
		pt := []byte(tc.plaintext)
		pad := aes.BlockSize - len(pt)%aes.BlockSize
		padded := append(append([]byte{}, pt...), bytes.Repeat([]byte{byte(pad)}, pad)...)
		// 手工 CBC: prev = IV, C_i = E(P_i xor prev)
		prev := append([]byte{}, key...) // IV == key
		out := make([]byte, 0, len(padded))
		buf := make([]byte, aes.BlockSize)
		for i := 0; i < len(padded); i += aes.BlockSize {
			for j := 0; j < aes.BlockSize; j++ {
				buf[j] = padded[i+j] ^ prev[j]
			}
			enc := make([]byte, aes.BlockSize)
			block.Encrypt(enc, buf)
			out = append(out, enc...)
			prev = enc
		}
		want := base64.StdEncoding.EncodeToString(out)
		if got := Encrypt(tc.plaintext); got != want {
			t.Errorf("[%s] 手工 CBC 复算 %q, Encrypt 输出 %q", tc.name, want, got)
		}
	}
}

// TestVideoEncMD5Input 校验参与 md5 的原始串逐字与 Python 版一致。
func TestVideoEncMD5Input(t *testing.T) {
	got := VideoEncMD5Input("123456", "7890123", "987654", "111222333", 125, 500)
	want := "[123456][7890123][987654][111222333][125000][d_yHJ!$pdA~5][500000][0_500]"
	if got != want {
		t.Fatalf("VideoEncMD5Input = %q, 期望 %q", got, want)
	}
	got2 := VideoEncMD5Input("aaa", "bbb", "ccc", "ddd", 0, 0)
	want2 := "[aaa][bbb][ccc][ddd][0][d_yHJ!$pdA~5][0][0_0]"
	if got2 != want2 {
		t.Fatalf("VideoEncMD5Input = %q, 期望 %q", got2, want2)
	}
}

// TestVideoEncPythonGolden md5 hex 与 Python hashlib 输出一致。
func TestVideoEncPythonGolden(t *testing.T) {
	cases := []struct {
		clazz, user, job, object string
		playTime, duration       int
		want                     string
	}{
		{"123456", "7890123", "987654", "111222333", 125, 500, "37a225f576b17ac6faa5aa8665860367"},
		{"aaa", "bbb", "ccc", "ddd", 0, 0, "70f6310a8c04b176c56fb3507a8fa225"},
	}
	for _, tc := range cases {
		got := VideoEnc(tc.clazz, tc.user, tc.job, tc.object, tc.playTime, tc.duration)
		if got != tc.want {
			t.Errorf("VideoEnc = %q, 期望 %q", got, tc.want)
		}
		if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(got) {
			t.Errorf("VideoEnc 输出不是 32 位小写 hex: %q", got)
		}
	}
}

// TestTimestamp 毫秒时间戳
func TestTimestamp(t *testing.T) {
	s := Timestamp()
	if len(s) != 13 {
		t.Fatalf("时间戳 %q 不是 13 位毫秒串", s)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("时间戳 %q 解析失败: %v", s, err)
	}
	now := time.Now().UnixMilli()
	if diff := now - v; diff < -1000 || diff > 2000 {
		t.Errorf("时间戳 %d 与当前时间 %d 偏差过大", v, now)
	}
	time.Sleep(2 * time.Millisecond)
	if v2, _ := strconv.ParseInt(Timestamp(), 10, 64); v2 < v {
		t.Errorf("时间戳出现倒退: %d -> %d", v, v2)
	}
}

// Package crypto 提供与超星服务端约定的加密/签名工具。
// 与 Python 原版 api/cipher.py、api/base.py:get_enc 逐字节对齐。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/butlanys/chaoxing-go/internal/consts"
)

// pkcs7Padding 按 PKCS7 规则填充到 blockSize 的整数倍。
// 与原版 pkcs7_padding 一致：即使长度已对齐也会补满一整块。
func pkcs7Padding(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padded := make([]byte, len(data)+padding)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(padding)
	}
	return padded
}

// pkcs7Unpadding 去除 PKCS7 填充，仅用于测试/自检。
func pkcs7Unpadding(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("密文长度 %d 不是分组大小 %d 的整数倍", len(data), blockSize)
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, fmt.Errorf("非法的 PKCS7 填充字节: %d", padding)
	}
	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, fmt.Errorf("PKCS7 填充字节不一致: %d", data[i])
		}
	}
	return data[:len(data)-padding], nil
}

// Encrypt AES-128-CBC + PKCS7 + 标准 base64，key/iv 均为 consts.AESKey。
// 与原版 AESCipher.encrypt 字节级一致（登录 uname/password 加密用）。
func Encrypt(plaintext string) string {
	key := []byte(consts.AESKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		// 密钥为固定常量，长度合法，正常不会走到这里；不向调用方 panic。
		return ""
	}
	padded := pkcs7Padding([]byte(plaintext), aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	// IV 与原版一致，同样取 AESKey
	cipher.NewCBCEncrypter(block, key).CryptBlocks(ciphertext, padded)
	return base64.StdEncoding.EncodeToString(ciphertext)
}

// Decrypt AES-128-CBC 解密 + 去 PKCS7 填充，主要供自检/单测使用。
func Decrypt(ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("base64 解码失败: %w", err)
	}
	key := []byte(consts.AESKey)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return "", fmt.Errorf("密文长度 %d 非法", len(raw))
	}
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, key).CryptBlocks(plain, raw)
	unpadded, err := pkcs7Unpadding(plain, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(unpadded), nil
}

// VideoEncMD5Input 返回 VideoEnc 参与 md5 的原始字符串，仅用于测试与自检。
// 逐字对齐 api/base.py:656 get_enc。
//
// 其中的 consts.VideoEncSalt 是目标平台的公开协议常量（硬编码在超星播放器前端 JS 中），
// 不是本项目凭据，详见 internal/consts/consts.go 的说明。
func VideoEncMD5Input(clazzID, userID, jobID, objectID string, playingTime, duration int) string {
	return fmt.Sprintf("[%s][%s][%s][%s][%d][%s][%d][0_%d]",
		clazzID, userID, jobID, objectID, playingTime*1000, consts.VideoEncSalt, duration*1000, duration)
}

// VideoEnc 计算视频进度上报的 enc 参数（md5 hex）。
// 对齐 Python: md5(f"[{clazzId}][{userid}][{jobid}][{objectId}][{playingTime*1000}][{salt}][{duration*1000}][0_{duration}]")
func VideoEnc(clazzID, userID, jobID, objectID string, playingTime, duration int) string {
	sum := md5.Sum([]byte(VideoEncMD5Input(clazzID, userID, jobID, objectID, playingTime, duration)))
	return hex.EncodeToString(sum[:])
}

// Timestamp 毫秒时间戳字符串，对齐 Python get_timestamp(): str(int(time.time() * 1000))
func Timestamp() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10)
}

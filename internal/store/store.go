// Package store 负责账号的持久化。
//
// 账号保存在 JSON 文件中（默认 ~/.chaoxing-go/accounts.json，权限 0600），
// 密码字段落盘前使用本机密钥（~/.chaoxing-go/key.bin，32 字节随机数，权限 0600）
// 做 AES-GCM 加密后 base64 编码，读取时自动解密。
package store

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultDirName 默认配置目录名（位于用户主目录下）
	DefaultDirName = ".chaoxing-go"
	// accountsFileName 账号文件名
	accountsFileName = "accounts.json"
	// keyFileName 本机密钥文件名
	keyFileName = "key.bin"
	// keySize AES-256 密钥长度
	keySize = 32
)

// Account 账号
type Account struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Password   string    `json:"password"` // 内存中为明文，落盘前加密
	Remark     string    `json:"remark"`
	Enabled    bool      `json:"enabled"`
	UseCookies bool      `json:"use_cookies"`
	Cookies    string    `json:"cookies"`
	CourseIDs  []string  `json:"course_ids"`
	LastLogin  time.Time `json:"last_login"`
	LastStatus string    `json:"last_status"`
}

// logger 最小日志接口，*logx.Logger 天然满足
type logger interface {
	Info(format string, a ...any)
	Warn(format string, a ...any)
	Error(format string, a ...any)
	Debug(format string, a ...any)
}

type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

var (
	logMu sync.RWMutex
	log   logger = nopLogger{}
)

// SetLogger 设置包级日志器（可选，默认不输出）
func SetLogger(l logger) {
	logMu.Lock()
	defer logMu.Unlock()
	if l == nil {
		log = nopLogger{}
		return
	}
	log = l
}

func logf() logger {
	logMu.RLock()
	defer logMu.RUnlock()
	return log
}

// Store 账号存储；所有方法并发安全
type Store struct {
	mu       sync.RWMutex
	path     string
	key      []byte
	accounts []Account
}

// DefaultPath 返回默认账号文件路径 ~/.chaoxing-go/accounts.json
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取用户主目录: %w", err)
	}
	return filepath.Join(home, DefaultDirName, accountsFileName), nil
}

// Open 打开账号存储；path 为空时使用 ~/.chaoxing-go/accounts.json。
//
// 目录不存在则创建（0700），账号文件权限 0600；文件不存在视为空列表；
// JSON 损坏时把原文件备份为 accounts.json.bak.<unix> 并返回空列表（不 panic）。
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	path = filepath.Clean(path)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建账号目录失败: %w", err)
	}

	key, err := loadOrCreateKey(filepath.Join(dir, keyFileName))
	if err != nil {
		return nil, err
	}

	s := &Store{path: path, key: key}
	accounts, err := s.load()
	if err != nil {
		return nil, err
	}
	s.accounts = accounts
	return s, nil
}

// Path 返回账号文件路径
func (s *Store) Path() string { return s.path }

// load 读取并解密账号列表
func (s *Store) load() ([]Account, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Account{}, nil // 文件不存在视为空列表
		}
		return nil, fmt.Errorf("读取账号文件失败: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return []Account{}, nil
	}

	var accounts []Account
	if err := json.Unmarshal(data, &accounts); err != nil {
		// JSON 损坏：备份原文件后返回空列表
		bak := fmt.Sprintf("%s.bak.%d", s.path, time.Now().Unix())
		if err2 := os.Rename(s.path, bak); err2 != nil {
			logf().Warn("账号文件损坏且备份失败: %v (原始错误: %v)", err2, err)
		} else {
			logf().Warn("账号文件已损坏，已备份为 %s: %v", bak, err)
		}
		return []Account{}, nil
	}
	if accounts == nil {
		accounts = []Account{}
	}

	for i := range accounts {
		if accounts[i].Password == "" {
			continue
		}
		plain, err := s.decrypt(accounts[i].Password)
		if err != nil {
			// 解密失败时置空密码，不影响其它账号
			logf().Warn("账号 %s 的密码解密失败，已置空: %v", accounts[i].Username, err)
			accounts[i].Password = ""
			continue
		}
		accounts[i].Password = plain
	}
	return accounts, nil
}

// List 返回账号列表的副本
func (s *Store) List() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Account, len(s.accounts))
	for i := range s.accounts {
		out[i] = copyAccount(s.accounts[i])
	}
	return out
}

// Get 按 ID 查询账号
func (s *Store) Get(id string) (Account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			return copyAccount(s.accounts[i]), true
		}
	}
	return Account{}, false
}

// Add 新增账号，ID 为空时自动生成 UUID
func (s *Store) Add(a Account) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a.ID == "" {
		a.ID = newUUID()
	} else {
		for i := range s.accounts {
			if s.accounts[i].ID == a.ID {
				return Account{}, fmt.Errorf("账号已存在: %s", a.ID)
			}
		}
	}
	s.accounts = append(s.accounts, copyAccount(a))
	if err := s.saveLocked(); err != nil {
		s.accounts = s.accounts[:len(s.accounts)-1] // 回滚内存状态
		return Account{}, err
	}
	return copyAccount(a), nil
}

// Update 按 ID 更新账号
func (s *Store) Update(a Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.accounts {
		if s.accounts[i].ID == a.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("账号不存在: %s", a.ID)
	}
	old := s.accounts[idx]
	s.accounts[idx] = copyAccount(a)
	if err := s.saveLocked(); err != nil {
		s.accounts[idx] = old // 回滚
		return err
	}
	return nil
}

// Delete 按 ID 删除账号
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("账号不存在: %s", id)
	}
	old := s.accounts
	s.accounts = append(append([]Account{}, s.accounts[:idx]...), s.accounts[idx+1:]...)
	if err := s.saveLocked(); err != nil {
		s.accounts = old // 回滚
		return err
	}
	return nil
}

// Save 把当前账号列表写回磁盘
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// saveLocked 落盘（调用方需持有写锁）：密码加密后原子写入
func (s *Store) saveLocked() error {
	disk := make([]Account, len(s.accounts))
	for i := range s.accounts {
		disk[i] = copyAccount(s.accounts[i])
		enc, err := s.encrypt(disk[i].Password)
		if err != nil {
			return fmt.Errorf("加密密码失败: %w", err)
		}
		disk[i].Password = enc
	}

	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化账号失败: %w", err)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".accounts-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 成功后 rename，删除失败无副作用

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("设置账号文件权限失败: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入账号文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入账号文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("写入账号文件失败: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("写入账号文件失败: %w", err)
	}
	return os.Chmod(s.path, 0o600)
}

// copyAccount 复制账号（含 CourseIDs 深拷贝）
func copyAccount(a Account) Account {
	if a.CourseIDs != nil {
		ids := make([]string, len(a.CourseIDs))
		copy(ids, a.CourseIDs)
		a.CourseIDs = ids
	}
	return a
}

// loadOrCreateKey 读取本机密钥，不存在时生成 32 字节随机密钥
func loadOrCreateKey(path string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil {
		if len(data) == keySize {
			return data, nil
		}
		logf().Warn("密钥文件 %s 长度异常(%d)，将重新生成，已有密码需要重新输入", path, len(data))
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成本机密钥失败: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("写入本机密钥失败: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("设置本机密钥权限失败: %w", err)
	}
	return key, nil
}

// gcm 返回 AES-GCM 实例
func (s *Store) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("初始化加密器失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化加密器失败: %w", err)
	}
	return gcm, nil
}

// encrypt 加密密码：AES-GCM(nonce||ciphertext) 后 base64
func (s *Store) encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	gcm, err := s.gcm()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decrypt 解密密码
func (s *Store) decrypt(enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("base64 解码失败: %w", err)
	}
	gcm, err := s.gcm()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密文长度不足")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}
	return string(plain), nil
}

// newUUID 生成 UUID v4（用 crypto/rand，避免引入新依赖）
func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下退化为时间戳，仍保证唯一性概率足够
		now := time.Now().UnixNano()
		for i := 0; i < 8; i++ {
			b[i] = byte(now >> (8 * (7 - i)))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

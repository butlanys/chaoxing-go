package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/butlanys/chaoxing-go/internal/logx"
)

// captureLogger 记录日志，供断言使用
type captureLogger struct {
	mu     sync.Mutex
	infos  []string
	warns  []string
	errs   []string
	debugs []string
}

func (c *captureLogger) Info(format string, a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.infos = append(c.infos, fmt.Sprintf(format, a...))
}
func (c *captureLogger) Warn(format string, a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warns = append(c.warns, fmt.Sprintf(format, a...))
}
func (c *captureLogger) Error(format string, a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, fmt.Sprintf(format, a...))
}
func (c *captureLogger) Debug(format string, a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.debugs = append(c.debugs, fmt.Sprintf(format, a...))
}
func (c *captureLogger) warnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.warns)
}

// openTemp 在临时目录打开一个 Store
func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	return s, path
}

// TestDefaultPathOpenEmptyPath path 为空时使用 ~/.chaoxing-go/accounts.json
func TestDefaultPathOpenEmptyPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath 失败: %v", err)
	}
	want := filepath.Join(home, DefaultDirName, "accounts.json")
	if p != want {
		t.Errorf("DefaultPath = %q, 期望 %q", p, want)
	}

	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\") 失败: %v", err)
	}
	if s.Path() != want {
		t.Errorf("Path = %q, 期望 %q", s.Path(), want)
	}
	dirInfo, err := os.Stat(filepath.Dir(want))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("目录权限 = %v, 期望 0700", dirInfo.Mode().Perm())
	}
	if len(s.List()) != 0 {
		t.Errorf("空目录应返回空列表, 实际 %d 条", len(s.List()))
	}
}

// TestCRUD 增删改查 + 自动生成 UUID
func TestCRUD(t *testing.T) {
	s, path := openTemp(t)

	a, err := s.Add(Account{Username: "13800000000", Password: "p@ss", Remark: "主账号", Enabled: true, CourseIDs: []string{"1", "2"}})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	if len(a.ID) != 36 || strings.Count(a.ID, "-") != 4 {
		t.Errorf("ID 应为 UUID, 实际 %q", a.ID)
	}
	if a.ID[14] != '4' {
		t.Errorf("ID 应为 v4 UUID, 实际 %q", a.ID)
	}

	got, ok := s.Get(a.ID)
	if !ok {
		t.Fatal("Get 未找到刚添加的账号")
	}
	if got.Username != "13800000000" || got.Password != "p@ss" || got.Remark != "主账号" || !got.Enabled {
		t.Errorf("Get 结果不正确: %+v", got)
	}
	if len(s.List()) != 1 {
		t.Errorf("List 长度 = %d", len(s.List()))
	}

	// 返回值必须是副本，修改不影响存储
	got.Username = "changed"
	got.CourseIDs[0] = "999"
	again, _ := s.Get(a.ID)
	if again.Username != "13800000000" || again.CourseIDs[0] != "1" {
		t.Errorf("返回值应为副本: %+v", again)
	}

	// Update
	got, _ = s.Get(a.ID)
	got.Remark = "备注已改"
	got.CourseIDs = []string{"7"}
	if err := s.Update(got); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	got2, _ := s.Get(a.ID)
	if got2.Remark != "备注已改" || len(got2.CourseIDs) != 1 || got2.CourseIDs[0] != "7" {
		t.Errorf("Update 未生效: %+v", got2)
	}

	// Delete
	if err := s.Delete(a.ID); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	if _, ok := s.Get(a.ID); ok {
		t.Error("Delete 后仍能查到")
	}
	if len(s.List()) != 0 {
		t.Errorf("Delete 后 List = %d 条", len(s.List()))
	}

	// 不存在的 ID
	if err := s.Update(Account{ID: "nope"}); err == nil {
		t.Error("Update 不存在的账号应返回 error")
	}
	if err := s.Delete("nope"); err == nil {
		t.Error("Delete 不存在的账号应返回 error")
	}
	// 重复 ID
	if _, err := s.Add(Account{ID: "dup", Username: "x"}); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	if _, err := s.Add(Account{ID: "dup", Username: "y"}); err == nil {
		t.Error("重复 ID 应返回 error")
	}

	// 落盘文件必须存在且为合法 JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("账号文件不存在: %v", err)
	}
	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("落盘内容不是合法 JSON: %v", err)
	}
}

// TestFilePermissions 账号文件 0600、密钥文件 0600、目录 0700
func TestFilePermissions(t *testing.T) {
	// 注意：t.TempDir() 本身受 umask 影响，这里验证 Open 自己创建的目录
	dir := filepath.Join(t.TempDir(), "chaoxing-go")
	path := filepath.Join(dir, "accounts.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	if _, err := s.Add(Account{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("账号文件权限 = %v, 期望 0600", info.Mode().Perm())
	}

	keyInfo, err := os.Stat(filepath.Join(filepath.Dir(path), keyFileName))
	if err != nil {
		t.Fatalf("密钥文件不存在: %v", err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Errorf("密钥文件权限 = %v, 期望 0600", keyInfo.Mode().Perm())
	}
	if keyInfo.Size() != keySize {
		t.Errorf("密钥长度 = %d, 期望 %d", keyInfo.Size(), keySize)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("目录权限 = %v, 期望 0700", dirInfo.Mode().Perm())
	}

	// 落盘内容中密码不得为明文
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"password": "p"`) {
		t.Errorf("密码以明文落盘:\n%s", data)
	}
}

// TestPasswordEncryptedAndReload 落盘为密文，重启(重新 Open)后能正确解密
func TestPasswordEncryptedAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	a, err := s.Add(Account{Username: "13900000000", Password: "秘密密码🔒", Enabled: true})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}

	// 重新 Open 模拟重启
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("重新 Open 失败: %v", err)
	}
	got, ok := s2.Get(a.ID)
	if !ok {
		t.Fatal("重启后未找到账号")
	}
	if got.Password != "秘密密码🔒" {
		t.Errorf("重启后密码 = %q, 期望 %q", got.Password, "秘密密码🔒")
	}

	// 空密码也能 round-trip
	b, err := s2.Add(Account{Username: "empty", Password: ""})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	s3, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := s3.Get(b.ID); g.Password != "" {
		t.Errorf("空密码 = %q", g.Password)
	}

	// 修改密码后重新打开仍是明文
	got.Password = "新密码"
	if err := s2.Update(got); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	s4, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := s4.Get(a.ID); g.Password != "新密码" {
		t.Errorf("更新后密码 = %q", g.Password)
	}
}

// TestCorruptJSONRecovery JSON 损坏时备份并返回空列表
func TestCorruptJSONRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(path, []byte("{这不是合法 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}

	log := &captureLogger{}
	SetLogger(log)
	t.Cleanup(func() { SetLogger(nil) })

	s, err := Open(path)
	if err != nil {
		t.Fatalf("损坏 JSON 不应返回 error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("损坏 JSON 应返回空列表, 实际 %d 条", len(s.List()))
	}
	if log.warnCount() == 0 {
		t.Error("应打 Warn 日志")
	}

	// 备份文件存在
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "accounts.json.bak.") {
			found = true
		}
	}
	if !found {
		t.Errorf("未找到备份文件: %v", entries)
	}

	// 备份后可正常写入
	if _, err := s.Add(Account{Username: "u", Password: "p"}); err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s2.List()) != 1 {
		t.Errorf("恢复后 List = %d 条", len(s2.List()))
	}
}

// TestMissingFile 文件不存在视为空列表
func TestMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "accounts.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("空存储 List = %d 条", len(s.List()))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Open 不应主动创建账号文件")
	}
}

// TestEmptyFile 空文件视为空列表
func TestEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.json")
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("空文件 List = %d 条", len(s.List()))
	}
}

// TestDecryptFailureEmptiesPassword 解密失败时置空密码并 Warn
func TestDecryptFailureEmptiesPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounts.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add(Account{Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}
	// 篡改密码字段
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var accounts []map[string]any
	if err := json.Unmarshal(data, &accounts); err != nil {
		t.Fatal(err)
	}
	accounts[0]["password"] = "bm90LWEtY2lwaGVydGV4dA==" // "not-a-ciphertext"
	out, err := json.Marshal(accounts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}

	log := &captureLogger{}
	SetLogger(log)
	t.Cleanup(func() { SetLogger(nil) })

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	list := s2.List()
	if len(list) != 1 {
		t.Fatalf("List = %d 条", len(list))
	}
	if list[0].Password != "" {
		t.Errorf("解密失败应置空密码, 实际 %q", list[0].Password)
	}
	if log.warnCount() == 0 {
		t.Error("解密失败应打 Warn 日志")
	}
	if list[0].Username != "u" {
		t.Errorf("其它字段不应受影响: %+v", list[0])
	}
}

// TestConcurrentAccess 50 个 goroutine 并发增删改查不 panic
func TestConcurrentAccess(t *testing.T) {
	s, _ := openTemp(t)

	const workers = 50
	ids := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a, err := s.Add(Account{Username: fmt.Sprintf("u%d", i), Password: fmt.Sprintf("p%d", i), CourseIDs: []string{"1"}})
			if err != nil {
				t.Errorf("Add 失败: %v", err)
				return
			}
			ids[i] = a.ID
		}(i)
	}
	wg.Wait()

	if len(s.List()) != workers {
		t.Fatalf("并发 Add 后 List = %d 条, 期望 %d", len(s.List()), workers)
	}

	// 并发读 + 改
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := ids[i]
			if a, ok := s.Get(id); ok {
				a.Remark = "并发改"
				a.LastStatus = "ok"
				a.LastLogin = time.Now()
				if err := s.Update(a); err != nil {
					t.Errorf("Update 失败: %v", err)
				}
			}
			_ = s.List()
			_ = s.Path()
		}(i)
	}
	wg.Wait()

	// 并发删一半
	for i := 0; i < workers; i += 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Delete(ids[i]); err != nil {
				t.Errorf("Delete 失败: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := len(s.List()); got != workers/2 {
		t.Fatalf("并发删除后 List = %d 条, 期望 %d", got, workers/2)
	}
	// 落盘内容应可重新解析
	if _, err := Open(s.Path()); err != nil {
		t.Fatalf("并发写后重新 Open 失败: %v", err)
	}
}

// 编译期断言：*logx.Logger 满足本包 logger 接口（CONTRACT 要求可直接传入）
var _ logger = (*logx.Logger)(nil)

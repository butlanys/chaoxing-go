package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/butlanys/chaoxing-go/internal/api"
	"github.com/butlanys/chaoxing-go/internal/config"
	"github.com/butlanys/chaoxing-go/internal/engine"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
	"github.com/butlanys/chaoxing-go/internal/logx"
	"github.com/butlanys/chaoxing-go/internal/notify"
	"github.com/butlanys/chaoxing-go/internal/store"
	"github.com/butlanys/chaoxing-go/internal/tiku"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是暴露给前端的 Wails 应用对象（多账号管理 + 并发刷课调度）。
type App struct {
	ctx context.Context

	store   *store.Store
	cfg     *config.Config
	cfgPath string
	dataDir string
	log     *logx.Logger

	manager *engine.Manager
	getLogs func() []logx.Entry

	mu       sync.RWMutex
	courses  map[string][]htmlx.Course
	statuses map[string]engine.Progress
	lastErr  string
}

// AccountInput 前端提交的账号表单
type AccountInput struct {
	ID         string   `json:"id"`
	Username   string   `json:"username"`
	Password   string   `json:"password"` // 为空表示保持原密码不变
	Remark     string   `json:"remark"`
	Enabled    bool     `json:"enabled"`
	UseCookies bool     `json:"use_cookies"`
	Cookies    string   `json:"cookies"`
	CourseIDs  []string `json:"course_ids"`
}

// AccountView 返回给前端的账号视图（不含明文密码）
type AccountView struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	Remark      string   `json:"remark"`
	Enabled     bool     `json:"enabled"`
	UseCookies  bool     `json:"use_cookies"`
	HasPassword bool     `json:"has_password"`
	HasCookies  bool     `json:"has_cookies"`
	CourseIDs   []string `json:"course_ids"`
	// LastLogin 用 RFC3339 字符串，避免 Wails 绑定生成器无法处理 time.Time
	LastLogin  string `json:"last_login"`
	LastStatus string `json:"last_status"`
	Label      string `json:"label"`
}

// RunRequest 启动刷课任务的请求
type RunRequest struct {
	AccountIDs       []string            `json:"account_ids"`
	CourseIDs        map[string][]string `json:"course_ids"` // 账号ID -> 课程 key(courseId/clazzId) 列表
	Speed            float64             `json:"speed"`
	Jobs             int                 `json:"jobs"`
	NotOpenAction    string              `json:"notopen_action"`
	RetryInterval    float64             `json:"retry_interval"`
	AddLearningCount bool                `json:"add_learning_count"`
	TargetCount      int                 `json:"target_count"`
}

// NewApp 构造应用对象
func NewApp() *App {
	return &App{
		log:      logx.New(""),
		courses:  map[string][]htmlx.Course{},
		statuses: map[string]engine.Progress{},
	}
}

// startup 由 Wails 在窗口创建后回调
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 日志出口: 内存回填 + 前端事件推送
	sink, get := logx.MemorySink(2000)
	a.getLogs = get
	logx.AddSink(sink)
	logx.AddSink(func(e logx.Entry) {
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "log", e)
		}
	})

	// 数据目录 ~/.chaoxing-go
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	a.dataDir = filepath.Join(home, ".chaoxing-go")
	_ = os.MkdirAll(a.dataDir, 0o700)

	st, err := store.Open(filepath.Join(a.dataDir, "accounts.json"))
	if err != nil {
		a.log.Error("加载账号数据失败: %v", err)
	} else {
		a.store = st
	}

	a.cfgPath = filepath.Join(a.dataDir, "config.ini")
	cfg, err := config.Load(a.cfgPath)
	if err != nil || cfg == nil {
		cfg = config.Default()
		if err := cfg.Save(a.cfgPath); err != nil {
			a.log.Warn("写入默认配置失败: %v", err)
		}
	}
	a.cfg = cfg

	// 任务管理器: 进度 -> 前端事件
	a.manager = engine.NewManager(a.log, engine.Hooks{
		OnProgress: func(p engine.Progress) {
			a.mu.Lock()
			a.statuses[p.Account] = p
			a.mu.Unlock()
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "progress", p)
			}
		},
		OnNotify: func(msg string) {
			a.log.Info("%s", msg)
		},
		AskFunc: func(prompt string) bool {
			if a.ctx == nil {
				return false
			}
			res, err := wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
				Type:    wruntime.QuestionDialog,
				Title:   "需要确认",
				Message: prompt,
				Buttons: []string{"继续", "停止"},
			})
			if err != nil {
				return false
			}
			return res == "继续"
		},
	})

	logx.SetLevel(logx.LevelInfo)

	// 验证码 OCR 后端配置（无 ocr_url 时自动降级为不可用，不影响主流程）
	api.OCRConfig = a.cfg.Tiku

	a.log.Info("chaoxing-go 已启动, 数据目录: %s", a.dataDir)
}

// shutdown 由 Wails 在退出前回调
func (a *App) shutdown(ctx context.Context) {
	if a.manager != nil {
		a.manager.Stop()
	}
	if a.store != nil {
		_ = a.store.Save()
	}
}

// AppInfo 返回应用元信息
func (a *App) AppInfo() map[string]string {
	return map[string]string{
		"name":     "chaoxing-go",
		"version":  "1.1.1",
		"dataDir":  a.dataDir,
		"config":   a.cfgPath,
		"upstream": "https://github.com/Samueli924/chaoxing",
		"license":  "GPL-3.0",
	}
}

// ---------- 账号管理 ----------

func toView(sa store.Account) AccountView {
	last := ""
	if !sa.LastLogin.IsZero() {
		last = sa.LastLogin.Format(time.RFC3339)
	}
	return AccountView{
		ID:          sa.ID,
		Username:    sa.Username,
		Remark:      sa.Remark,
		Enabled:     sa.Enabled,
		UseCookies:  sa.UseCookies,
		HasPassword: sa.Password != "",
		HasCookies:  strings.TrimSpace(sa.Cookies) != "",
		CourseIDs:   sa.CourseIDs,
		LastLogin:   last,
		LastStatus:  sa.LastStatus,
		Label:       labelOf(sa),
	}
}

func labelOf(sa store.Account) string {
	if sa.Remark != "" {
		return sa.Remark
	}
	if sa.Username != "" {
		return sa.Username
	}
	return sa.ID
}

// ListAccounts 列出全部账号
func (a *App) ListAccounts() []AccountView {
	if a.store == nil {
		return nil
	}
	list := a.store.List()
	out := make([]AccountView, 0, len(list))
	for _, sa := range list {
		out = append(out, toView(sa))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

// AddAccount 新增账号
func (a *App) AddAccount(in AccountInput) (AccountView, error) {
	if a.store == nil {
		return AccountView{}, errors.New("账号存储未初始化")
	}
	if strings.TrimSpace(in.Username) == "" && !in.UseCookies {
		return AccountView{}, errors.New("手机号不能为空")
	}
	sa, err := a.store.Add(store.Account{
		Username:   strings.TrimSpace(in.Username),
		Password:   in.Password,
		Remark:     in.Remark,
		Enabled:    in.Enabled,
		UseCookies: in.UseCookies,
		Cookies:    in.Cookies,
		CourseIDs:  in.CourseIDs,
	})
	if err != nil {
		return AccountView{}, err
	}
	a.log.Info("已新增账号: %s", labelOf(sa))
	return toView(sa), nil
}

// UpdateAccount 更新账号（Password 为空表示保持原值）
func (a *App) UpdateAccount(in AccountInput) error {
	if a.store == nil {
		return errors.New("账号存储未初始化")
	}
	old, ok := a.store.Get(in.ID)
	if !ok {
		return fmt.Errorf("账号不存在: %s", in.ID)
	}
	old.Username = strings.TrimSpace(in.Username)
	old.Remark = in.Remark
	old.Enabled = in.Enabled
	old.UseCookies = in.UseCookies
	old.Cookies = in.Cookies
	old.CourseIDs = in.CourseIDs
	if in.Password != "" {
		old.Password = in.Password
	}
	if err := a.store.Update(old); err != nil {
		return err
	}
	a.log.Info("已更新账号: %s", labelOf(old))
	return nil
}

// DeleteAccount 删除账号
func (a *App) DeleteAccount(id string) error {
	if a.store == nil {
		return errors.New("账号存储未初始化")
	}
	if err := a.store.Delete(id); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.courses, id)
	delete(a.statuses, id)
	a.mu.Unlock()
	a.log.Info("已删除账号: %s", id)
	return nil
}

// ---------- 登录与课程 ----------

func (a *App) newClient(sa store.Account) *api.Client {
	tk, err := tiku.New(a.cfg.Tiku, a.log)
	if err != nil {
		a.log.Warn("题库初始化失败(将跳过章节检测): %v", err)
	}
	opt := api.Options{
		QueryDelay:      a.cfg.TikuFloat("delay", 0),
		WorkMaxRetries:  a.cfg.Common.WorkMaxRetries,
		WorkRedoEnabled: a.cfg.Common.WorkRedoEnabled,
		Speed:           a.cfg.Common.Speed,
		NotOpenAction:   a.cfg.Common.NotOpenAction,
		CookieFile:      filepath.Join(a.dataDir, "cookies_"+sa.ID+".txt"),
	}
	if proxy := a.cfg.TikuGet("http_proxy", ""); proxy != "" {
		opt.Proxy = proxy
	}
	return api.NewClient(api.Account{Username: sa.Username, Password: sa.Password, Remark: labelOf(sa)}, tk, a.log.With(labelOf(sa)), opt)
}

// LoginAccount 登录指定账号
func (a *App) LoginAccount(id string) (api.LoginResult, error) {
	sa, ok := a.store.Get(id)
	if !ok {
		return api.LoginResult{}, fmt.Errorf("账号不存在: %s", id)
	}
	cli := a.newClient(sa)
	res, err := cli.Login(sa.UseCookies)
	if err != nil {
		sa.LastStatus = "登录异常: " + err.Error()
		_ = a.store.Update(sa)
		return res, err
	}
	sa.LastStatus = res.Msg
	if res.Status {
		sa.LastLogin = time.Now()
		sa.Cookies = cli.CookieString()
	}
	if err := a.store.Update(sa); err != nil {
		a.log.Warn("保存账号状态失败: %v", err)
	}
	return res, nil
}

// FetchCourses 拉取账号的课程列表并缓存
func (a *App) FetchCourses(id string) ([]htmlx.Course, error) {
	sa, ok := a.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("账号不存在: %s", id)
	}
	cli := a.newClient(sa)
	if res, err := cli.Login(sa.UseCookies); err != nil || !res.Status {
		if err != nil {
			return nil, fmt.Errorf("登录失败: %w", err)
		}
		return nil, errors.New("登录失败: " + res.Msg)
	}
	courses, err := cli.CourseList()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.courses[id] = courses
	a.mu.Unlock()
	return courses, nil
}

// CachedCourses 返回内存中缓存的课程列表
func (a *App) CachedCourses(id string) []htmlx.Course {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.courses[id]
}

// ---------- 任务调度 ----------

// StartRun 启动多账号并发刷课
func (a *App) StartRun(req RunRequest) error {
	if a.manager == nil {
		return errors.New("任务管理器未初始化")
	}
	if a.manager.Running() {
		return errors.New("已有任务在运行，请先停止")
	}
	if len(req.AccountIDs) == 0 {
		return errors.New("请至少选择一个账号")
	}

	notifier := notify.New(a.cfg.Notification, a.log)
	tk, tkErr := tiku.New(a.cfg.Tiku, a.log)
	if tkErr != nil {
		a.log.Warn("题库初始化失败(将跳过章节检测): %v", tkErr)
	}

	speed := req.Speed
	if speed < 1 {
		speed = 1
	}
	if speed > 2 {
		speed = 2
	}
	jobs := req.Jobs
	if jobs <= 0 {
		jobs = 4
	}
	action := req.NotOpenAction
	if action == "" {
		action = "retry"
	}

	var tasks []engine.AccountTask
	for _, id := range req.AccountIDs {
		sa, ok := a.store.Get(id)
		if !ok {
			a.log.Warn("跳过不存在的账号: %s", id)
			continue
		}
		all := a.CachedCourses(id)
		if len(all) == 0 {
			a.log.Warn("账号 %s 尚未拉取课程列表, 已跳过", labelOf(sa))
			continue
		}
		selected := pickCourses(all, req.CourseIDs[id])
		tasks = append(tasks, engine.AccountTask{
			Key:        id,
			Account:    api.Account{Username: sa.Username, Password: sa.Password, Remark: labelOf(sa)},
			UseCookies: sa.UseCookies,
			CookieFile: filepath.Join(a.dataDir, "cookies_"+sa.ID+".txt"),
			Courses:    selected,
			Tiku:       tk,
			Notify:     notifier,
			Options: engine.Options{
				Speed:            speed,
				Jobs:             jobs,
				NotOpenAction:    action,
				RetryInterval:    req.RetryInterval,
				MaxTries:         5,
				AddLearningCount: req.AddLearningCount,
				TargetCount:      req.TargetCount,
				QueryDelay:       a.cfg.TikuFloat("delay", 0),
				WorkMaxRetries:   a.cfg.Common.WorkMaxRetries,
				WorkRedoEnabled:  a.cfg.Common.WorkRedoEnabled,
			},
		})
	}
	if len(tasks) == 0 {
		return errors.New("没有可执行的账号任务（请先拉取课程列表并勾选课程）")
	}

	a.mu.Lock()
	a.statuses = map[string]engine.Progress{}
	a.mu.Unlock()

	a.log.Info("开始执行任务: %d 个账号并发", len(tasks))
	go func() {
		if err := a.manager.Start(tasks); err != nil {
			a.log.Error("任务执行失败: %v", err)
			a.mu.Lock()
			a.lastErr = err.Error()
			a.mu.Unlock()
		}
		if a.ctx != nil {
			wruntime.EventsEmit(a.ctx, "done", a.GetStatuses())
		}
	}()
	return nil
}

func pickCourses(all []htmlx.Course, keys []string) []htmlx.Course {
	if len(keys) == 0 {
		return all
	}
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	out := make([]htmlx.Course, 0, len(keys))
	seen := map[string]bool{}
	for _, c := range all {
		if set[c.CourseID] || set[c.Key()] {
			if seen[c.Key()] {
				continue
			}
			seen[c.Key()] = true
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

// StopRun 停止当前任务
func (a *App) StopRun() error {
	if a.manager == nil {
		return errors.New("任务管理器未初始化")
	}
	a.manager.Stop()
	a.log.Warn("已请求停止所有任务")
	return nil
}

// GetStatuses 返回所有账号的任务进度
func (a *App) GetStatuses() []engine.Progress {
	if a.manager != nil {
		for _, p := range a.manager.Status() {
			a.mu.Lock()
			a.statuses[p.Account] = p
			a.mu.Unlock()
		}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]engine.Progress, 0, len(a.statuses))
	for _, p := range a.statuses {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out
}

// IsRunning 是否有任务在跑
func (a *App) IsRunning() bool { return a.manager != nil && a.manager.Running() }

// ---------- 日志 ----------

// GetLogs 返回最近的日志（GUI 首次加载回填）
func (a *App) GetLogs(limit int) []logx.Entry {
	if a.getLogs == nil {
		return nil
	}
	all := a.getLogs()
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all
}

// SetLogLevel 设置日志级别（trace/debug/info/warn/error）
func (a *App) SetLogLevel(level string) { logx.SetLevel(logx.ParseLevel(level)) }

// ---------- 配置 ----------

// LoadConfig 返回当前配置
func (a *App) LoadConfig() *config.Config { return a.cfg }

// SaveConfig 保存配置到数据目录
func (a *App) SaveConfig(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("配置为空")
	}
	a.cfg = cfg
	if err := cfg.Save(a.cfgPath); err != nil {
		return err
	}
	a.log.Info("配置已保存: %s", a.cfgPath)
	return nil
}

// LoadConfigFile 从任意 ini 文件导入配置
func (a *App) LoadConfigFile(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	a.cfg = cfg
	a.cfgPath = path
	a.log.Info("已载入配置文件: %s", path)
	return cfg, nil
}

// SaveConfigAs 另存配置到指定路径
func (a *App) SaveConfigAs(path string, cfg *config.Config) error {
	if cfg == nil {
		cfg = a.cfg
	}
	if err := cfg.Save(path); err != nil {
		return err
	}
	a.cfgPath = path
	a.log.Info("配置已另存为: %s", path)
	return nil
}

// PickConfigFile 打开文件选择框
func (a *App) PickConfigFile() (string, error) {
	if a.ctx == nil {
		return "", errors.New("窗口未就绪")
	}
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择配置文件 (config.ini)",
		Filters: []wruntime.FileFilter{
			{DisplayName: "INI 配置", Pattern: "*.ini"},
			{DisplayName: "所有文件", Pattern: "*.*"},
		},
	})
}

// ExportResults 导出本次运行结果到文件，返回文件路径
func (a *App) ExportResults() (string, error) {
	path := filepath.Join(a.dataDir, fmt.Sprintf("result_%s.txt", time.Now().Format("20060102_150405")))
	var sb strings.Builder
	sb.WriteString("chaoxing-go 运行结果\n")
	sb.WriteString("时间: " + time.Now().Format(time.RFC3339) + "\n\n")
	for _, p := range a.GetStatuses() {
		fmt.Fprintf(&sb, "[%s] 课程: %s 章节: %s 进度: %d/%d 失败: %d 状态: %s\n",
			p.Account, p.Course, p.Chapter, p.Done, p.Total, p.Failed, p.State)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

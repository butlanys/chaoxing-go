// Package engine 负责任务点调度与多账号并发执行。
//
// 语义与 Python 原版 main.py 的 ChapterTask / JobProcessor / process_chapter / process_job 对齐。
package engine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/butlanys/chaoxing-go/internal/api"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
	"github.com/butlanys/chaoxing-go/internal/logx"
	"github.com/butlanys/chaoxing-go/internal/notify"
	"github.com/butlanys/chaoxing-go/internal/tiku"
)

// bytesTrim 去掉首尾空白
func bytesTrim(b []byte) []byte { return bytes.TrimSpace(b) }

// ChapterResult 章节处理结果
type ChapterResult int

// 章节结果枚举，与 Python 版 ChapterResult 一致
const (
	ChapterSuccess ChapterResult = iota
	ChapterError
	ChapterNotOpen
	ChapterPending
)

// String 便于日志输出
func (c ChapterResult) String() string {
	switch c {
	case ChapterSuccess:
		return "success"
	case ChapterError:
		return "error"
	case ChapterNotOpen:
		return "not_open"
	case ChapterPending:
		return "pending"
	}
	return "unknown"
}

// Options 运行参数
type Options struct {
	Speed            float64
	Jobs             int // 单账号并发章节数
	NotOpenAction    string
	RetryInterval    float64
	MaxTries         int
	AddLearningCount bool
	TargetCount      int
	QueryDelay       float64 // 题库查询间隔（秒）
	WorkMaxRetries   int     // 章节检测最大重做次数
	WorkRedoEnabled  bool    // 是否允许章节检测自动重做
}

func (o Options) normalized() Options {
	if o.Speed <= 0 {
		o.Speed = 1
	}
	if o.Speed > 2 {
		o.Speed = 2
	}
	if o.Jobs <= 0 {
		o.Jobs = 4
	}
	if o.NotOpenAction == "" {
		o.NotOpenAction = "retry"
	}
	if o.RetryInterval <= 0 {
		o.RetryInterval = 1.0
	}
	if o.MaxTries <= 0 {
		o.MaxTries = 5
	}
	if o.TargetCount <= 0 {
		o.TargetCount = 100
	}
	if o.WorkMaxRetries <= 0 {
		o.WorkMaxRetries = 3
	}
	return o
}

// Progress 进度事件
type Progress struct {
	Account string `json:"account"`
	Course  string `json:"course"`
	Chapter string `json:"chapter"`
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Failed  int    `json:"failed"`
	State   string `json:"state"` // pending|running|done|error|stopped
}

// Hooks 回调集合（GUI 用它把进度/日志推给前端）
type Hooks struct {
	OnProgress func(Progress)
	OnNotify   func(msg string)
	AskFunc    func(prompt string) bool
}

// AccountTask 一个账号的完整任务描述
type AccountTask struct {
	Key        string
	Account    api.Account
	UseCookies bool
	CookieFile string
	Courses    []htmlx.Course
	Options    Options
	Tiku       tiku.Provider
	Notify     notify.Notifier
}

// Manager 多账号并发调度器
type Manager struct {
	log   *logx.Logger
	hooks Hooks

	mu      sync.Mutex
	runners []*Runner
	status  map[string]Progress
	running bool
	cancel  chan struct{}
	doneCh  chan struct{}
	wg      sync.WaitGroup
}

// NewManager 构造调度器
func NewManager(log *logx.Logger, hooks Hooks) *Manager {
	if log == nil {
		log = logx.New("")
	}
	return &Manager{log: log, hooks: hooks, status: map[string]Progress{}}
}

// Running 是否有任务在跑
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// Status 返回各账号的最新进度快照
func (m *Manager) Status() []Progress {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Progress, 0, len(m.status))
	for _, p := range m.status {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account < out[j].Account })
	return out
}

// Start 并发启动所有账号的任务，阻塞直到全部结束
func (m *Manager) Start(tasks []AccountTask) error {
	if len(tasks) == 0 {
		return errors.New("没有可执行的任务")
	}
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return errors.New("已有任务在运行")
	}
	m.running = true
	m.runners = nil
	m.status = map[string]Progress{}
	m.cancel = make(chan struct{})
	m.doneCh = make(chan struct{})
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.running = false
		close(m.doneCh)
		m.mu.Unlock()
	}()

	m.log.Info("开始执行任务: %d 个账号并发执行", len(tasks))

	var errMu sync.Mutex
	var errs []string

	for _, t := range tasks {
		t := t
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			if err := m.runAccount(t); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %v", t.Account.Label(), err))
				errMu.Unlock()
			}
		}()
	}
	m.wg.Wait()

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// runAccount 执行单个账号的全部课程
func (m *Manager) runAccount(t AccountTask) error {
	label := t.Account.Label()
	log := m.log.With(label)
	opt := t.Options.normalized()

	apiOpt := api.Options{
		QueryDelay:      opt.QueryDelay,
		WorkMaxRetries:  opt.WorkMaxRetries,
		WorkRedoEnabled: opt.WorkRedoEnabled,
		Speed:           opt.Speed,
		NotOpenAction:   opt.NotOpenAction,
		CookieFile:      t.CookieFile,
		AskFunc:         m.hooks.AskFunc,
	}

	cli := api.NewClient(t.Account, t.Tiku, log, apiOpt)
	if t.CookieFile != "" {
		if raw, err := os.ReadFile(t.CookieFile); err == nil && len(bytesTrim(raw)) > 0 {
			_ = cli.LoadCookies(string(raw))
		}
	}

	res, err := cli.Login(t.UseCookies)
	if err != nil || !res.Status {
		msg := res.Msg
		if err != nil {
			msg = err.Error()
		}
		log.Error("登录失败: %s", msg)
		m.setStatus(label, Progress{Account: label, State: "error"})
		return fmt.Errorf("登录失败: %s", msg)
	}
	log.Success("登录成功: %s", res.RealName)

	r := NewRunner(cli, opt, log, Hooks{
		OnProgress: func(p Progress) {
			p.Account = label
			m.setStatus(label, p)
			if m.hooks.OnProgress != nil {
				m.hooks.OnProgress(p)
			}
		},
		OnNotify: m.hooks.OnNotify,
		AskFunc:  m.hooks.AskFunc,
	})

	m.mu.Lock()
	m.runners = append(m.runners, r)
	m.mu.Unlock()

	// 汇总所有课程的章节（对齐 Python main(): 全局并发执行所有课程的任务点）
	type coursePoints struct {
		course htmlx.Course
		points []htmlx.Point
	}
	var cps []coursePoints
	total := 0
	for _, c := range t.Courses {
		pl, err := cli.CoursePoints(c.CourseID, c.ClazzID, c.CPI)
		if err != nil {
			log.Error("读取课程 [%s] 章节失败: %v", c.Title, err)
			continue
		}
		cps = append(cps, coursePoints{course: c, points: pl.Points})
		total += len(pl.Points)
	}
	log.Info("课程章节读取完毕, 共 %d 个章节待处理", total)

	m.setStatus(label, Progress{Account: label, Total: total, State: "running"})

	var tasks []*ChapterTask
	idx := 0
	for _, cp := range cps {
		for _, p := range cp.points {
			tasks = append(tasks, &ChapterTask{index: idx, point: p, course: cp.course})
			idx++
		}
	}

	r.total = total
	if err := r.RunTasks(tasks); err != nil {
		log.Error("任务执行出错: %v", err)
		m.setStatus(label, Progress{Account: label, Total: total, Done: r.doneCount(), Failed: r.failedCount(), State: "error"})
		if t.Notify != nil {
			_ = t.Notify.Send(fmt.Sprintf("chaoxing : 账号 %s 出现错误 %v", label, err))
		}
		return err
	}

	log.Success("账号 %s 所有课程学习任务已完成", label)
	m.setStatus(label, Progress{Account: label, Total: total, Done: r.doneCount(), Failed: r.failedCount(), State: "done"})
	if t.Notify != nil {
		_ = t.Notify.Send(fmt.Sprintf("chaoxing : 账号 %s 所有课程学习任务已完成", label))
	}

	// 刷课完成后追加章节学习次数（对齐 Python main()）
	if opt.AddLearningCount {
		log.Info("刷课完成, 开始增加章节学习次数...")
		for _, cp := range cps {
			if r.Stopped() {
				break
			}
			if res := cli.IncreaseChapterLearningCount(cp.course, cp.points, opt.TargetCount, func(n, target int) {
				log.Info("章节学习次数进度: %d/%d", n, target)
			}); res.IsFailure() {
				log.Error("课程 [%s] 章节学习次数增加失败", cp.course.Title)
			}
		}
		log.Info("所有课程章节学习次数增加完成")
		if t.Notify != nil {
			_ = t.Notify.Send(fmt.Sprintf("chaoxing : 账号 %s 所有课程章节学习次数增加完成", label))
		}
	}
	return nil
}

func (m *Manager) setStatus(label string, p Progress) {
	m.mu.Lock()
	if old, ok := m.status[label]; ok {
		// 保留更完整的字段
		if p.Total == 0 {
			p.Total = old.Total
		}
		if p.Course == "" {
			p.Course = old.Course
		}
	}
	m.status[label] = p
	m.mu.Unlock()
}

// Stop 停止所有账号的任务（不阻塞）
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	runners := append([]*Runner(nil), m.runners...)
	running := m.running
	m.mu.Unlock()

	if !running {
		return
	}
	if cancel != nil {
		select {
		case <-cancel:
		default:
			close(cancel)
		}
	}
	for _, r := range runners {
		r.Stop()
	}
	m.log.Warn("已请求停止所有任务, 等待当前请求返回...")
}

// Wait 等待全部任务结束（可带超时，timeout<=0 表示一直等）
func (m *Manager) Wait(timeout time.Duration) bool {
	m.mu.Lock()
	done := m.doneCh
	running := m.running
	m.mu.Unlock()
	if !running || done == nil {
		return true
	}
	if timeout <= 0 {
		<-done
		return true
	}
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// DefaultCookiePath 返回某账号的默认 cookie 文件路径
func DefaultCookiePath(dataDir, accountID string) string {
	return filepath.Join(dataDir, "cookies_"+accountID+".txt")
}

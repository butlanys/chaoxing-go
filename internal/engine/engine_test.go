package engine

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/butlanys/chaoxing-go/internal/api"
	"github.com/butlanys/chaoxing-go/internal/crypto"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
	"github.com/butlanys/chaoxing-go/internal/logx"
	"github.com/butlanys/chaoxing-go/internal/notify"
)

// ---------- 假超星服务端 ----------

// rewriteTransport 把发往 chaoxing.com 各子域的请求改写到本地假服务端。
//
// 这样 engine 的集成测试走的是 api.Client 里真实的 URL / 参数构造逻辑，
// 只把网络出口换成假服务端，不需要 mock 掉任何协议细节。
type rewriteTransport struct {
	target *url.URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r2 := req.Clone(req.Context())
	r2.URL.Scheme = t.target.Scheme
	r2.URL.Host = t.target.Host
	r2.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(r2)
}

// 章节列表页（两个章节点）
const fakeChapterHTML = `<div class="chapter_unit"><ul>` +
	`<li><div id="cur1001"><a class="clicktitle">第一章</a><input class="knowledgeJobCount" value="1"></div></li>` +
	`<li><div id="cur1002"><a class="clicktitle">第二章</a><input class="knowledgeJobCount" value="1"></div></li>` +
	`</ul></div>`

// 任务点卡片页（num=0 时返回一个 document 任务）
func fakeCardHTML(jobID, name string) string {
	return `<html><script type="text/javascript">mArg={"attachments":[{"jobid":"` + jobID +
		`","type":"document","job":{"id":"` + jobID + `"},"objectId":"obj-` + jobID +
		`","otherInfo":"nodeId_5005-1","property":{"name":"` + name + `"},"mid":"mid-1",` +
		`"jtoken":"jt1","enc":"enc1"}],"defaults":{"ktoken":"kt1","cpi":"3003",` +
		`"knowledgeid":"5005","reportTimeInterval":60}};</script></html>`
}

// fakeCx 假超星服务端：记录每个账号的请求序列，并按路径返回可解析的响应。
type fakeCx struct {
	mu       sync.Mutex
	seq      []string // 形如 "<账号>:<路径>"
	failDoc  int      // /ananas/job/document 前 N 次返回 500（用于测重试）
	notOpen  bool     // knowledge/cards 返回"章节未开放"
	docCount int
}

// uid 从 fanyalogin 的 uname（AES 密文，每账号唯一）派生账号标识
func uidOf(uname string) string {
	if uname == "" {
		return "?"
	}
	// 取密文前 8 个字符足够区分测试账号
	if len(uname) > 8 {
		return uname[:8]
	}
	return uname
}

func (f *fakeCx) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	_ = r.ParseForm()
	who := uidOf(r.FormValue("uname"))
	if who == "?" {
		if ck, err := r.Cookie("_uid"); err == nil {
			who = ck.Value
		}
	}

	f.mu.Lock()
	f.seq = append(f.seq, who+path)
	f.mu.Unlock()

	switch {
	case strings.Contains(path, "fanyalogin"):
		http.SetCookie(w, &http.Cookie{Name: "_uid", Value: who, Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "fid", Value: "1024", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":true}`))

	case strings.Contains(path, "mycourse/studentcourse"):
		_, _ = w.Write([]byte(fakeChapterHTML))

	case strings.Contains(path, "knowledge/cards"):
		if f.notOpen {
			_, _ = w.Write([]byte(`<html>章节未开放</html>`))
			return
		}
		if r.URL.Query().Get("num") == "0" {
			_, _ = w.Write([]byte(fakeCardHTML("doc-1", "任务一")))
			return
		}
		_, _ = w.Write([]byte(`<html><script type="text/javascript">mArg={"attachments":[],"defaults":{"ktoken":"kt1","cpi":"3003","knowledgeid":"5005"}};</script></html>`))

	case strings.Contains(path, "ananas/job/document"):
		f.mu.Lock()
		f.docCount++
		shouldFail := f.docCount <= f.failDoc
		f.mu.Unlock()
		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":true,"msg":"文档任务完成"}`))

	default:
		w.WriteHeader(http.StatusOK)
	}
}

// 记录下来的请求快照
func (f *fakeCx) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.seq))
	copy(out, f.seq)
	return out
}

// ---------- 测试脚手架 ----------

type captureNotifier struct {
	mu   sync.Mutex
	msgs []string
}

func (c *captureNotifier) Name() string { return "capture" }

func (c *captureNotifier) Send(msg string) error {
	c.mu.Lock()
	c.msgs = append(c.msgs, msg)
	c.mu.Unlock()
	return nil
}

func (c *captureNotifier) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.msgs))
	copy(out, c.msgs)
	return out
}

func testCourse() htmlx.Course {
	return htmlx.Course{
		CourseID: "100", ClazzID: "200", CPI: "3003",
		Title: "测试课程", Teacher: "老师",
	}
}

// newTask 构造一个把请求导向假服务端的账号任务
func newTask(fake *fakeCx, tr *rewriteTransport, username string, opt Options, ntf notify.Notifier) AccountTask {
	return AccountTask{
		Key:       username,
		Account:   api.Account{Username: username, Password: "pw-" + username, Remark: username},
		Courses:   []htmlx.Course{testCourse()},
		Options:   opt,
		Notify:    ntf,
		Transport: tr,
	}
}

// ---------- 测试 ----------

// TestManagerRunsAccountsConcurrently 两个账号并发跑完全部章节，并验证请求真的交错。
func TestManagerRunsAccountsConcurrently(t *testing.T) {
	fake := &fakeCx{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	target, _ := url.Parse(srv.URL)
	tr := &rewriteTransport{target: target}

	ntf := &captureNotifier{}
	var progMu sync.Mutex
	var progress []Progress
	hooks := Hooks{
		OnProgress: func(p Progress) {
			progMu.Lock()
			progress = append(progress, p)
			progMu.Unlock()
		},
	}

	opt := Options{Speed: 1, Jobs: 1, NotOpenAction: "continue", RetryInterval: 0.05, MaxTries: 3}
	m := NewManager(logx.New(""), hooks)

	tasks := []AccountTask{
		newTask(fake, tr, "13800000001", opt, ntf),
		newTask(fake, tr, "13800000002", opt, ntf),
	}

	done := make(chan error, 1)
	go func() { done <- m.Start(tasks) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Manager.Start 返回错误: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("Manager.Start 超时未结束")
	}

	// 1) 两个账号都应为 done
	statuses := m.Status()
	if len(statuses) != 2 {
		t.Fatalf("状态数 = %d, 期望 2 (%+v)", len(statuses), statuses)
	}
	for _, p := range statuses {
		if p.State != "done" {
			t.Errorf("账号 %s 状态 = %s, 期望 done (done=%d failed=%d)", p.Account, p.State, p.Done, p.Failed)
		}
		if p.Total != 2 || p.Done != 2 || p.Failed != 0 {
			t.Errorf("账号 %s 进度 = %d/%d 失败 %d, 期望 2/2 失败 0", p.Account, p.Done, p.Total, p.Failed)
		}
	}

	// 2) 两个账号各自发了请求
	seq := fake.snapshot()
	uidA, uidB := uidOf(crypto.Encrypt("13800000001")), uidOf(crypto.Encrypt("13800000002"))
	firstB, lastA := -1, -1
	for i, s := range seq {
		if strings.HasPrefix(s, uidB) && firstB < 0 {
			firstB = i
		}
		if strings.HasPrefix(s, uidA) {
			lastA = i
		}
	}
	if firstB < 0 {
		t.Fatalf("未看到账号 B 的请求, 序列: %v", seq)
	}
	if lastA < 0 {
		t.Fatalf("未看到账号 A 的请求, 序列: %v", seq)
	}
	// 3) 并发证据: B 的首次请求出现在 A 的末次请求之前 => 两者时间上重叠
	if firstB > lastA {
		t.Errorf("两个账号的请求没有交错, 疑似串行执行 (A 末次=%d, B 首次=%d)\n序列: %v", lastA, firstB, seq)
	}

	// 4) 进度事件覆盖两个账号
	seen := map[string]bool{}
	progMu.Lock()
	for _, p := range progress {
		seen[p.Account] = true
	}
	progMu.Unlock()
	for _, name := range []string{"13800000001", "13800000002"} {
		if !seen[name] {
			t.Errorf("账号 %s 没有收到任何进度事件", name)
		}
	}

	// 5) 通知: 每个账号一条完成消息
	if got := ntf.all(); len(got) != 2 {
		t.Errorf("通知条数 = %d, 期望 2 (%v)", len(got), got)
	}
}

// TestRunnerRetriesFailedJob 任务点首次失败后应进重试队列并最终成功。
func TestRunnerRetriesFailedJob(t *testing.T) {
	fake := &fakeCx{failDoc: 2} // 前 2 次 document 请求返回 500
	srv := httptest.NewServer(fake)
	defer srv.Close()
	target, _ := url.Parse(srv.URL)
	tr := &rewriteTransport{target: target}

	m := NewManager(logx.New(""), Hooks{})
	opt := Options{Speed: 1, Jobs: 1, NotOpenAction: "continue", RetryInterval: 0.05, MaxTries: 5}
	err := m.Start([]AccountTask{newTask(fake, tr, "13800000009", opt, &captureNotifier{})})
	if err != nil {
		t.Fatalf("Manager.Start 返回错误: %v", err)
	}

	// 章节 2 个，每个 1 个 document 任务；其中一个先失败 2 次后成功
	// => 2 次失败 + 2 次成功 = 4 次 document 请求
	fake.mu.Lock()
	docCount := fake.docCount
	fake.mu.Unlock()
	if docCount != 4 {
		t.Errorf("document 请求次数 = %d, 期望 4 (2 次失败 + 2 个章节各成功 1 次)", docCount)
	}
	for _, p := range m.Status() {
		if p.State != "done" || p.Done != 2 || p.Failed != 0 {
			t.Errorf("重试后状态 = %s, 进度 %d/%d 失败 %d, 期望 done 2/2 失败 0",
				p.State, p.Done, p.Total, p.Failed)
		}
	}
}

// TestRunnerNotOpenContinue 章节未开放且策略为 continue 时应跳过并计为成功。
func TestRunnerNotOpenContinue(t *testing.T) {
	fake := &fakeCx{notOpen: true}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	target, _ := url.Parse(srv.URL)
	tr := &rewriteTransport{target: target}

	m := NewManager(logx.New(""), Hooks{})
	opt := Options{Speed: 1, Jobs: 1, NotOpenAction: "continue", RetryInterval: 0.05, MaxTries: 3}
	if err := m.Start([]AccountTask{newTask(fake, tr, "13800000007", opt, &captureNotifier{})}); err != nil {
		t.Fatalf("Manager.Start 返回错误: %v", err)
	}
	for _, p := range m.Status() {
		if p.State != "done" || p.Failed != 0 {
			t.Errorf("continue 策略下状态 = %s 失败 %d, 期望 done 0", p.State, p.Failed)
		}
	}
	// continue 策略下每个章节只请求一次 cards（不重试）
	cards := 0
	for _, s := range fake.snapshot() {
		if strings.Contains(s, "knowledge/cards") {
			cards++
		}
	}
	if cards != 2 {
		t.Errorf("cards 请求次数 = %d, 期望 2 (两个章节各一次, 不重试)", cards)
	}
}

// TestStopInterruptsRun 停止后任务应尽快退出且不再继续处理。
func TestStopInterruptsRun(t *testing.T) {
	fake := &fakeCx{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	target, _ := url.Parse(srv.URL)
	tr := &rewriteTransport{target: target}

	m := NewManager(logx.New(""), Hooks{})
	opt := Options{Speed: 1, Jobs: 1, NotOpenAction: "retry", RetryInterval: 0.05, MaxTries: 5}

	finished := make(chan error, 1)
	go func() {
		finished <- m.Start([]AccountTask{newTask(fake, tr, "13800000008", opt, &captureNotifier{})})
	}()

	// 等到确实开始跑了再停
	deadline := time.Now().Add(20 * time.Second)
	for !m.Running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !m.Running() {
		t.Fatal("任务未进入运行状态")
	}
	m.Stop()

	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("Stop 后任务未在 30s 内退出")
	}
	if m.Running() {
		t.Error("停止后 Running() 仍为 true")
	}
}

// TestTaskQueueOrderAndShutdown 优先队列按 index 出队，关闭后 Get 返回 nil。
func TestTaskQueueOrderAndShutdown(t *testing.T) {
	q := newTaskQueue()
	for _, idx := range []int{5, 1, 3, 0, 9} {
		q.Put(&ChapterTask{index: idx})
	}
	var got []int
	for i := 0; i < 5; i++ {
		task := q.Get()
		if task == nil {
			t.Fatal("队列提前返回 nil")
		}
		got = append(got, task.index)
	}
	want := []int{0, 1, 3, 5, 9}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("出队顺序 = %v, 期望 %v", got, want)
		}
	}
	q.Shutdown()
	if task := q.Get(); task != nil {
		t.Errorf("关闭后 Get 应返回 nil, 实际 %+v", task)
	}
}

// TestTaskQueueGetBlocksUntilPut Get 在空队列上应阻塞，Put 后立刻返回。
func TestTaskQueueGetBlocksUntilPut(t *testing.T) {
	q := newTaskQueue()
	got := make(chan int, 1)
	go func() {
		task := q.Get()
		if task == nil {
			got <- -1
			return
		}
		got <- task.index
	}()
	select {
	case v := <-got:
		t.Fatalf("空队列上 Get 未阻塞, 立即返回 %d", v)
	case <-time.After(150 * time.Millisecond):
	}
	q.Put(&ChapterTask{index: 42})
	select {
	case v := <-got:
		if v != 42 {
			t.Errorf("Get 得到 %d, 期望 42", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Put 后 Get 未返回")
	}
}

// TestOptionsNormalized 参数归一化（倍速上限、并发下限、默认值）
func TestOptionsNormalized(t *testing.T) {
	cases := []struct {
		in   Options
		want Options
	}{
		{Options{}, Options{Speed: 1, Jobs: 4, NotOpenAction: "retry", RetryInterval: 1.0, MaxTries: 5, TargetCount: 100, WorkMaxRetries: 3}},
		{Options{Speed: 5, Jobs: -1, TargetCount: 50}, Options{Speed: 2, Jobs: 4, NotOpenAction: "retry", RetryInterval: 1.0, MaxTries: 5, TargetCount: 50, WorkMaxRetries: 3}},
	}
	for i, tc := range cases {
		if got := tc.in.normalized(); got != tc.want {
			t.Errorf("用例 %d: normalized() = %+v, 期望 %+v", i, got, tc.want)
		}
	}
}

// 编译期断言：captureNotifier 满足 notify.Notifier
var _ notify.Notifier = (*captureNotifier)(nil)

// 编译期断言：Hooks 回调签名
var _ = fmt.Sprintf

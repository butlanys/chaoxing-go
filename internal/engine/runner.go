package engine

import (
	"container/heap"
	"sync"
	"time"

	"github.com/butlanys/chaoxing-go/internal/api"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
	"github.com/butlanys/chaoxing-go/internal/logx"
)

// ChapterTask 一个章节任务（对应 Python main.py 的 ChapterTask）
type ChapterTask struct {
	index  int
	point  htmlx.Point
	course htmlx.Course
	result ChapterResult
	tries  int
}

// taskHeap 按 index 排序的最小堆（对齐 Python PriorityQueue 的 ChapterTask.__lt__）
type taskHeap []*ChapterTask

func (h taskHeap) Len() int           { return len(h) }
func (h taskHeap) Less(i, j int) bool { return h[i].index < h[j].index }
func (h taskHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *taskHeap) Push(x any)        { *h = append(*h, x.(*ChapterTask)) }
func (h *taskHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return item
}

// taskQueue 阻塞式优先队列，行为对齐 Python queue.PriorityQueue
type taskQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	items  taskHeap
	closed bool
}

func newTaskQueue() *taskQueue {
	q := &taskQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Put 入队并唤醒一个等待中的 worker
func (q *taskQueue) Put(t *ChapterTask) {
	q.mu.Lock()
	if !q.closed {
		heap.Push(&q.items, t)
		q.cond.Broadcast()
	}
	q.mu.Unlock()
}

// Get 阻塞取任务；队列已关闭且为空时返回 nil
func (q *taskQueue) Get() *ChapterTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.items) == 0 {
		return nil
	}
	return heap.Pop(&q.items).(*ChapterTask)
}

// Len 当前排队长度
func (q *taskQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Shutdown 关闭队列并唤醒全部等待者
func (q *taskQueue) Shutdown() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

// Runner 单账号任务执行器
type Runner struct {
	client *api.Client
	opt    Options
	log    *logx.Logger
	hooks  Hooks

	mu          sync.Mutex
	cond        *sync.Cond
	stopped     bool
	outstanding int
	doneN       int
	failedN     int
	total       int
	stopCh      chan struct{}
	stopOnce    sync.Once
}

// NewRunner 构造单账号执行器
func NewRunner(c *api.Client, opt Options, log *logx.Logger, hooks Hooks) *Runner {
	if log == nil {
		log = logx.New("")
	}
	r := &Runner{
		client: c,
		opt:    opt.normalized(),
		log:    log,
		hooks:  hooks,
		stopCh: make(chan struct{}),
	}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// Stopped 是否已请求停止
func (r *Runner) Stopped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped
}

func (r *Runner) doneCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.doneN
}

func (r *Runner) failedCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failedN
}

// Stop 请求停止（幂等）
func (r *Runner) Stop() {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		r.stopped = true
		r.mu.Unlock()
		close(r.stopCh)
	})
}

// RunTasks 并发执行全部章节任务，阻塞直到全部结束
func (r *Runner) RunTasks(tasks []*ChapterTask) error {
	if len(tasks) == 0 {
		return nil
	}
	q := newTaskQueue()
	retryQ := newTaskQueue()

	r.mu.Lock()
	r.outstanding = len(tasks)
	r.total = len(tasks)
	r.mu.Unlock()

	for _, t := range tasks {
		q.Put(t)
	}

	var wg sync.WaitGroup
	for i := 0; i < r.opt.Jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.worker(q, retryQ)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.retryLoop(q, retryQ)
	}()

	r.waitOutstanding()

	// 对齐 Python: join 之后稍作等待再关闭队列
	time.Sleep(500 * time.Millisecond)
	q.Shutdown()
	retryQ.Shutdown()
	wg.Wait()

	if r.Stopped() {
		r.log.Warn("任务已停止")
	}
	return nil
}

// waitOutstanding 等待所有任务到达终态
func (r *Runner) waitOutstanding() {
	r.mu.Lock()
	for r.outstanding > 0 && !r.stopped {
		r.cond.Wait()
	}
	r.mu.Unlock()
}

func (r *Runner) finishOne(success bool) {
	r.mu.Lock()
	r.outstanding--
	if success {
		r.doneN++
	} else {
		r.failedN++
	}
	r.cond.Broadcast()
	r.mu.Unlock()
}

func (r *Runner) emit(p Progress) {
	if r.hooks.OnProgress != nil {
		r.hooks.OnProgress(p)
	}
}

func (r *Runner) snapshot(course, chapter, state string) Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Progress{
		Course:  course,
		Chapter: chapter,
		Total:   r.total,
		Done:    r.doneN,
		Failed:  r.failedN,
		State:   state,
	}
}

// worker 章节工作协程
func (r *Runner) worker(q, retryQ *taskQueue) {
	for {
		task := q.Get()
		if task == nil {
			return
		}
		if r.Stopped() {
			r.finishOne(false)
			continue
		}

		r.emit(r.snapshot(task.course.Title, task.point.Title, "running"))
		task.result = r.processChapter(task.course, task.point)

		switch task.result {
		case ChapterSuccess:
			r.log.Debug("任务完成: %s - %s", task.course.Title, task.point.Title)
			r.finishOne(true)
			r.emit(r.snapshot(task.course.Title, task.point.Title, "running"))

		case ChapterNotOpen:
			if r.opt.NotOpenAction == "continue" {
				r.log.Warn("章节未开启: %s - %s, 正在跳过", task.course.Title, task.point.Title)
				r.finishOne(true)
				continue
			}
			if r.opt.NotOpenAction == "ask" && r.hooks.AskFunc != nil {
				if r.hooks.AskFunc("章节未开启: " + task.point.Title + "\n可能由于上一章节的章节检测未完成。是否跳过继续?") {
					r.log.Warn("用户选择跳过: %s", task.point.Title)
					r.finishOne(true)
					continue
				}
			}
			task.tries++
			if task.tries >= r.opt.MaxTries {
				r.log.Error("章节未开启: %s - %s 可能由于上一章节的章节检测未完成, 也可能由于该章节因为时效已关闭, "+
					"请手动检查完成并提交再重试。或者在配置中配置(自动跳过关闭章节/开启题库并启用提交)",
					task.course.Title, task.point.Title)
				r.finishOne(false)
				continue
			}
			retryQ.Put(task)

		case ChapterError:
			task.tries++
			r.log.Warn("重试任务 %s - %s (%d/%d 次尝试)",
				task.course.Title, task.point.Title, task.tries, r.opt.MaxTries)
			if task.tries >= r.opt.MaxTries {
				r.log.Error("任务重试次数达到上限: %s - %s", task.course.Title, task.point.Title)
				r.finishOne(false)
				continue
			}
			retryQ.Put(task)

		default:
			r.log.Error("任务 %s 的状态无效: %s", task.result, task.point.Title)
			r.finishOne(false)
		}
	}
}

// retryLoop 把重试队列中的任务按间隔重新投递回主队列
func (r *Runner) retryLoop(q, retryQ *taskQueue) {
	for {
		task := retryQ.Get()
		if task == nil {
			return
		}
		if r.Stopped() {
			r.finishOne(false)
			continue
		}
		select {
		case <-r.stopCh:
			r.finishOne(false)
			continue
		case <-time.After(time.Duration(r.opt.RetryInterval * float64(time.Second))):
		}
		q.Put(task)
	}
}

// processChapter 处理单个章节（对齐 Python process_chapter）
func (r *Runner) processChapter(course htmlx.Course, point htmlx.Point) ChapterResult {
	r.log.Info("当前章节: %s", point.Title)

	if point.HasFinished {
		r.log.Info("章节：%s 已完成所有任务点", point.Title)
		return ChapterSuccess
	}

	// 随机等待，避免请求过快
	r.client.RateLimitRandom(0, 0.2)

	jobs, info := r.client.JobList(course, point)
	if info.NotOpen {
		return ChapterNotOpen
	}

	for _, job := range jobs {
		if r.Stopped() {
			return ChapterError
		}
		res := r.processJob(course, job, info)
		if res.IsFailure() {
			return ChapterError
		}
	}
	return ChapterSuccess
}

// processJob 处理单个任务点（对齐 Python process_job）
func (r *Runner) processJob(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo) api.StudyResult {
	switch job.Type {
	case "video":
		r.log.Trace("识别到视频任务, 任务章节: %s 任务ID: %s", course.Title, job.JobID)
		res := r.client.StudyVideo(course, job, info, "Video")
		if res.IsFailure() {
			r.log.Warn("当前任务非视频任务, 正在尝试音频任务解码")
			res = r.client.StudyVideo(course, job, info, "Audio")
		}
		if res.IsFailure() {
			r.log.Warn("出现异常任务 -> 任务章节: %s 任务ID: %s, 已跳过", course.Title, job.JobID)
		}
		return res

	case "document":
		r.log.Trace("识别到文档任务, 任务章节: %s 任务ID: %s", course.Title, job.JobID)
		return r.client.StudyDocument(course, job)

	case "workid":
		r.log.Trace("识别到章节检测任务, 任务章节: %s", course.Title)
		return r.client.StudyWork(course, job, info)

	case "read":
		r.log.Trace("识别到阅读任务, 任务章节: %s", course.Title)
		return r.client.StudyRead(course, job, info)

	case "live":
		r.log.Trace("识别到直播任务, 任务章节: %s 任务ID: %s", course.Title, job.JobID)
		return r.client.RunLive(course, job, info.KnowledgeID, r.opt.Speed)
	}

	r.log.Error("未知任务类型: %s", job.Type)
	return api.ResultError
}

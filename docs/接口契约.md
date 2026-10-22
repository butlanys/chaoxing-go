# chaoxing-go 接口契约 (CONTRACT)

本项目是 `https://github.com/Samueli924/chaoxing` (Python, GPL-3.0) 的 Go 重写二创。
Python 参考实现已 clone 到 `/root/chaoxing-ref/`，**协议细节以它为准，逐行对齐**。

- 模块路径: `github.com/butlanys/chaoxing-go`
- Go 版本: 1.25 (go.mod 已就绪，依赖已下载)
- 目录: `/root/chaoxing-go`
- 依赖: `github.com/PuerkitoBio/goquery`, `gopkg.in/ini.v1`, `github.com/wailsapp/wails/v2`
- **禁止**引入新的第三方依赖（除上述三个 + 标准库 + `golang.org/x/net/html`）；需要别的功能用标准库自己写。

## 硬性规则

1. 所有中文注释、日志文案与 Python 版保持一致的语义。
2. 每个包必须 `go vet ./...` 干净、`go build ./...` 通过。
3. 每个包自带 `_test.go` 单元测试（用真实样例片段，不要 mock 掉解析逻辑）。
4. 不允许 panic 传播到调用方；网络错误返回 error。
5. **只修改自己负责的目录**，不要动别人的包。
6. 常量、User-Agent、AES Key、URL、请求参数必须与 Python 版逐字一致。

## 全局常量 (internal/consts)

```go
package consts

const (
	AESKey          = "u2oh6Vu^HWe4_AES"
	Threshold       = 1 // 秒, 视频进度轮询间隔
	RateInterval    = 0.5
	VideoLogInterval = 2
)

var Headers = map[string]string{
	"User-Agent":        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36",
	"sec-ch-ua":         `"Chromium";v="118", "Google Chrome";v="118", "Not=A?Brand";v="99"`,
	"sec-ch-ua-mobile":  "?0",
	"sec-ch-ua-platform": `"Windows"`,
}

var VideoHeaders = map[string]string{"Referer": "https://mooc1.chaoxing.com/ananas/modules/video/index.html?v=2025-0725-1842"}
var AudioHeaders = map[string]string{"Referer": "https://mooc1.chaoxing.com/ananas/modules/audio/index_new.html?v=2025-0725-1842"}
```

## internal/logx — 日志

```go
package logx

type Level int
const (LevelTrace Level = iota; LevelDebug; LevelInfo; LevelWarn; LevelError; LevelSuccess)

func ParseLevel(s string) Level

type Entry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`   // "trace"/"debug"/"info"/"warn"/"error"/"success"
	Account string    `json:"account"` // 账号标识(手机号或备注), "" = 全局
	Msg     string    `json:"msg"`
}

// Sink 是日志出口。GUI 用它把日志通过 Wails event 推给前端；CLI 打印到 stderr。
type Sink func(Entry)

// SetSink 设置全局出口(可多个)
func AddSink(s Sink)
func SetLevel(l Level)
func GetLevel() Level

type Logger struct{ /* 内部持有 account 标签 */ }
func New(account string) *Logger
func (l *Logger) With(account string) *Logger
func (l *Logger) Trace(format string, a ...any)
func (l *Logger) Debug(format string, a ...any)
func (l *Logger) Info(format string, a ...any)
func (l *Logger) Warn(format string, a ...any)
func (l *Logger) Error(format string, a ...any)
func (l *Logger) Success(format string, a ...any)
```

## internal/crypto

```go
package crypto

// Encrypt AES-128-CBC + PKCS7 + base64, key/iv 均为 consts.AESKey
func Encrypt(plaintext string) string

// VideoEnc md5(f"[{clazzID}][{userID}][{jobID}][{objectID}][{playingTime*1000}][d_yHJ!$pdA~5][{duration*1000}][0_{duration}]")
func VideoEnc(clazzID, userID, jobID, objectID string, playingTime, duration int) string

// Timestamp 毫秒时间戳字符串
func Timestamp() string
```

## internal/font — 超星加密字体解码

```go
package font

// Decoder 供 htmlx 调用；可为 nil
type Decoder interface{ Decode(string) string }

type Glyph struct {
	NumberOfContours int
	EndPtsOfContours []int
	Coords           [][2]int // 绝对坐标(已累加 delta)
	Flags            []byte   // 原始 flags, hash 时取 &0x01
}

// HashGlyph 复刻 Python hash_glyph: numberOfContours<=0 返回 ""
// pos_data[i] = fmt.Sprintf("%d%d%d", x, y, flag&1)，拼接后 md5 hex
func HashGlyph(g *Glyph) string

// ParseTTFGlyphs 解析 TTF 的 glyf/loca/maxp/head 表, 返回 {"uni4E00": hash, ...}
// 只处理 numberOfContours > 0 的简单字形；复合字形跳过
func ParseTTFGlyphs(ttf []byte) (map[string]string, error)

// Table 是 resource/font_map_table.json 的加载结果 ({"uni4E00": "hash", ...})
type Table struct{ /* char->hash, hash->char 双向 */ }
func LoadTable(path string) (*Table, error)
func (t *Table) FindChar(hash string) (string, bool) // hash -> "uni4E00"
func (t *Table) FindHash(char string) (string, bool)

// NewDecoder 从含 <style id="cxSecretStyle"> 的 HTML 构造解码器; 失败返回 error
func NewDecoder(htmlText string, table *Table) (*HTMLDecoder, error)

type HTMLDecoder struct{ /* 持有 fontmap: uniXXXX -> hash */ }
func (d *HTMLDecoder) Decode(s string) string

// 康熙部首替换表(与 Python KX_RADICALS_TAB 完全一致)
var KXRadicals *strings.Replacer // 或 func KXFix(string) string
```

- 字体 base64 提取正则: `base64,([\w\W]+?)'`，前缀 `data:application/font-ttf;charset=utf-8;base64,`（前缀长度 47）。
- `Decode` 逐字符: `char_code = "uni" + upper(hex(ord(c)))`；命中 fontmap 得到 hash；用 hash 反查 Table 得原始 `uniXXXX`；转字符。查不到保留原字符。最后做康熙部首替换。
- `resource/font_map_table.json` 必须从 `/root/chaoxing-ref/resource/font_map_table.json` 复制到 `/root/chaoxing-go/resource/`（1.5MB）。**加载路径解析**：优先 `<exe目录>/resource/font_map_table.json`，其次工作目录，最后内嵌兜底（用 `//go:embed` 内嵌一份也可以）。

## internal/htmlx — HTML 解析（纯函数，无网络）

```go
package htmlx

import "github.com/butlanys/chaoxing-go/internal/font"

type Course struct {
	ID       string `json:"id"`
	Info     string `json:"info"`
	RoleID   string `json:"roleid"`
	ClazzID  string `json:"clazzId"`
	CourseID string `json:"courseId"`
	CPI      string `json:"cpi"`
	Title    string `json:"title"`
	Desc     string `json:"desc"`
	Teacher  string `json:"teacher"`
}

type Folder struct{ ID, Rename string }

type Point struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	JobCount    string `json:"jobCount"`
	HasFinished bool   `json:"has_finished"`
	NeedUnlock  bool   `json:"need_unlock"`
}

type PointList struct {
	HasLocked bool    `json:"hasLocked"`
	Points    []Point `json:"points"`
}

type Job struct {
	Type                 string `json:"type"` // video|document|workid|read|live
	JobID                string `json:"jobid"`
	Name                 string `json:"name"`
	OtherInfo            string `json:"otherinfo"`
	Mid                  string `json:"mid"`
	ObjectID             string `json:"objectid"`
	Aid                  string `json:"aid"`
	PlayTime             int    `json:"playTime"` // 毫秒
	Rt                   string `json:"rt"`
	AttDuration          string `json:"attDuration"`
	AttDurationEnc       string `json:"attDurationEnc"`
	VideoFaceCaptureEnc  string `json:"videoFaceCaptureEnc"`
	JToken               string `json:"jtoken"`
	Enc                  string `json:"enc"`
	Title                string `json:"title"` // read/live 用
	Property             map[string]any `json:"property"`
}

type JobInfo struct {
	KToken             string `json:"ktoken"`
	MtEnc              string `json:"mtEnc"`
	ReportTimeInterval int    `json:"reportTimeInterval"`
	DefEnc             string `json:"defenc"`
	CardID             string `json:"cardid"`
	CPI                string `json:"cpi"`
	QnEnc              string `json:"qnenc"`
	KnowledgeID        string `json:"knowledgeid"`
	NotOpen            bool   `json:"notOpen"`
}

type Question struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Options      string `json:"options"`
	Type         string `json:"type"`     // single|multiple|completion|judgement|shortanswer|unknown
	TypeCode     string `json:"typeCode"` // 原始 data 属性
	Answer       string `json:"answer"`   // 最终提交的答案
	AnswerSource string `json:"answerSource"` // cover|random|""
}

type WorkForm struct {
	Fields    map[string]string `json:"fields"` // form 里除含 "answer" 外的 input name->value
	Questions []Question        `json:"questions"`
	// AnswerWQBID = strings.Join(ids, ",") + ","
	AnswerWQBID string `json:"answerwqbid"`
}

type RecordQuestion struct {
	ID            string
	Title         string
	TypeLabel     string
	MyAnswer      string
	CorrectAnswer string
	ParseOK       bool
}

func DecodeCourseList(htmlText string) []Course
func DecodeCourseFolder(htmlText string) []Folder
func DecodeCoursePoint(htmlText string) PointList
func DecodeCourseCard(htmlText string) ([]Job, JobInfo) // "章节未开放" -> JobInfo{NotOpen:true}
func DecodeQuestions(htmlText string, fd font.Decoder) WorkForm
func DecodeWorkRecordList(htmlText string) []struct{ Times int; Score float64 }
func DecodeWorkRecordDetail(htmlText string) []RecordQuestion
```

必须逐条对齐 `/root/chaoxing-ref/api/decode.py` 的 selector / 正则 / 字段名 / 排序（`q_options.sort()` 后 `\n` join；选项取 `aria-label` 优先，去掉尾部 "选择"；题目 title 收集 NavigableString + `<img src="...">` 拼接、去掉 `\r\t\n`；题型码 0/1/2/3/4 → single/multiple/completion/judgement/shortanswer）。

`_process_attachment_cards` 的直播识别条件、`otherInfo` 取 `split("&")[0]`、跳过 `isPassed`、`job==nil` 时只尝试 read 任务等细节都要保留。
`mArg={(.*?)};` 提取时先 `strings.ReplaceAll(html, " ", "")`。

## internal/config

```go
package config

type Common struct {
	UseCookies       bool
	Username         string
	Password         string
	CourseList       []string
	Speed            float64
	Jobs             int
	NotOpenAction    string  // retry|ask|continue
	RetryInterval    float64
	WorkRedoEnabled  bool
	WorkMaxRetries   int
	AddLearningCount bool
	TargetCount      int
}

type Config struct {
	Common       Common
	Tiku         map[string]string // [tiku] 节原始键值(键小写)
	Notification map[string]string // [notification] 节
	Path         string            // 来源 ini 路径
}

func Load(path string) (*Config, error)
func Default() *Config
func (c *Config) Save(path string) error // 写回 ini(保持节名与键)
func (c *Config) TikuGet(key, def string) string
func (c *Config) TikuFloat(key string, def float64) float64
func (c *Config) TikuBool(key string, def bool) bool
func StrToBool(s string) bool
```

INI 默认值必须与 `/root/chaoxing-ref/config_template.ini` + `main.py:load_config_from_file` 一致（speed 默认 1、jobs 默认 4、notopen_action 默认 retry、retry_interval 默认 1.0、work_max_retries 默认 3、target_count 默认 100、cover_rate 默认 0.9、delay 默认 1.0 等）。同时生成一份 `config_template.ini`（复制 Python 版）。

## internal/tiku — 题库

```go
package tiku

type Question struct { // 与 htmlx.Question 解耦, engine 负责转换
	ID, Title, Options, Type string
}

type Provider interface {
	Name() string
	// QueryAll 返回与输入等长的切片, 未搜到填 ""; delay 为题目间隔秒数
	QueryAll(qs []Question, delay float64) []string
	// Query 单题
	Query(q Question) (string, bool)
	// SubmitMode "" = 提交, "1" = 仅保存
	SubmitMode() string
	CoverRate() float64
	// JudgementSelect 判断题答案 -> bool (true 表示选"正确")
	JudgementSelect(ans string) bool
	SetWorkFeedback(feedback []string)
	Disabled() bool
	IsManual() bool
	TrueList() []string
	FalseList() []string
}

// New 根据 [tiku] 配置构造题库; provider 支持逗号分隔多题库回退
func New(conf map[string]string, log *logx.Logger) (Provider, error)
```

实现的题库（对齐 `/root/chaoxing-ref/api/answer.py`）：
- `TikuYanxi` (https://tk.enncy.cn/query, token 轮换)
- `TikuGo` (https://q.icodef.com/api/v1/answer, go_min_interval/go_retry_times/go_retry_backoff)
- `TikuLike` (datam.site, likeapi_* 配置, 多 token 随机)
- `TikuAdapter` (自建 url)
- `AI` (OpenAI 兼容 endpoint/key/model/min_interval_seconds/http_proxy, 支持 work feedback 重答)
- `SiliconFlow` (siliconflow_key/model/endpoint)
- `TikuManual` (GUI/CLI 交互, 用回调注入)
- `TikuFallback` (多 provider 顺序回退)
- `DummyTiku` (未配置题库时的空实现, Disabled()==true)

`internal/tiku/cache.go`: `cache.json` 题目缓存 (`CacheDAO` 等价), 线程安全 + 原子写。
`internal/tiku/check.go`: 移植 `answer_check.py` 的 `cut`/`check_single`/`check_multiple`/`check_judgement`/`check_completion`/`check_answer`。

手动模式在 GUI 下需要回调，定义：
```go
// ManualHandler 由 GUI/CLI 注入; 返回答案列表(与题目等长)
type ManualHandler func(qs []Question) []string
var ManualHook ManualHandler
```

## internal/notify

```go
package notify

type Notifier interface { Name() string; Send(msg string) error }
func New(conf map[string]string, log *logx.Logger) Notifier // 空配置 -> noop
```
ServerChan / Qmsg / Bark / Telegram(需 tg_chat_id), 对齐 `/root/chaoxing-ref/api/notification.py`。

## internal/captcha

```go
package captcha

type Solver interface { Solve(img []byte) (string, error) }

// New 依据 [common]/[tiku] 配置构造; 无 OCR 可用时返回 DisabledSolver(ErrUnavailable)
func New(conf map[string]string, log *logx.Logger) Solver

// CxCaptcha 复刻 api/captcha.py 的验证码通关流程
type CxCaptcha struct{ /* http client, cookies, ua, solver */ }
func NewCxCaptcha(client *http.Client, ua, cookies string, s Solver, log *logx.Logger) *CxCaptcha
func (c *CxCaptcha) TryPass() bool
```
`/processVerifyPng.ac?t=<rand>` 取图 (Content-Type 必须是 image/png)，`/html/processVerify.ac?ucode=&app=0` 提交，302 视为成功。
OCR 实现：优先 `docr` HTTP 服务(配置键 `ocr_url`)，其次返回 DisabledSolver。

## internal/api — 超星客户端

```go
package api

type Account struct{ Username, Password, Remark string }

type StudyResult int
const (ResultSuccess StudyResult = iota; ResultForbidden; ResultError; ResultTimeout)
func (r StudyResult) IsSuccess() bool
func (r StudyResult) IsFailure() bool

type LoginResult struct{ Status bool; Msg string; RealName string }

type Options struct {
	QueryDelay      float64
	WorkMaxRetries  int
	WorkRedoEnabled bool
	Speed           float64
	CookieFile      string        // 每账号独立, 默认 cookies_<username>.txt
	Timeout         time.Duration // 默认 5s
	Proxy           string
	NotOpenAction   string
	AskFunc         func(prompt string) bool // "ask" 模式回调; nil 时按 retry 处理
}

type Client struct{ /* http.Client(带 cookie jar), account, tiku, log, 两个限速器, uid/fid 缓存 */ }

func NewClient(acc Account, t tiku.Provider, log *logx.Logger, opt Options) *Client

func (c *Client) Login(useCookies bool) (LoginResult, error)
func (c *Client) ValidateCookieSession() bool
func (c *Client) Name() string
func (c *Client) UID() string
func (c *Client) FID() string
// CookieString 导出当前会话 cookie 为 "k=v; k=v" 形式（GUI 保存账号 cookie 用）
func (c *Client) CookieString() string
// LoadCookies 从 "k=v; k=v" 字符串导入 cookie
func (c *Client) LoadCookies(cookieStr string) error
func (c *Client) CourseList() ([]htmlx.Course, error)
func (c *Client) CoursePoints(courseID, clazzID, cpi string) (htmlx.PointList, error)
func (c *Client) JobList(course htmlx.Course, point htmlx.Point) ([]htmlx.Job, htmlx.JobInfo, error)
func (c *Client) StudyVideo(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo, typ string) StudyResult // typ: "Video"|"Audio"
func (c *Client) StudyDocument(course htmlx.Course, job htmlx.Job) StudyResult
func (c *Client) StudyWork(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo) StudyResult
func (c *Client) StudyRead(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo) StudyResult
// RunLive 处理直播任务（对应 api/live.py + api/live_process.py），
// 循环提交观看时长直到达到直播总时长；knowledgeID 取 job_info.knowledgeid。
func (c *Client) RunLive(course htmlx.Course, job htmlx.Job, knowledgeID string, speed float64) StudyResult
// LiveStatus 查询直播状态（含总时长），对应 api/live.py 的 get_status
func (c *Client) LiveStatus(course htmlx.Course, job htmlx.Job, knowledgeID string) (map[string]any, error)
func (c *Client) StudyEmptyPage(course htmlx.Course, point htmlx.Point) StudyResult
func (c *Client) IncreaseChapterLearningCount(course htmlx.Course, points []htmlx.Point, target int, onProgress func(n, target int)) StudyResult
func (c *Client) ActivityList(course htmlx.Course) ([]Activity, error)
func (c *Client) SignIn(course htmlx.Course, activityID string) (string, error)

// 供 engine 使用的答案后处理(对齐 base.py 的 multi_cut/clean_res/is_subsequence/best_option_by_similarity/random_answer/normalize_text)
func MultiCut(answer string) []string
func CleanRes(res []string) []string
func IsSubsequence(a, o string) bool
func BestOptionBySimilarity(target string, options []string, threshold float64) string
func RandomAnswer(options, qType string) string
func NormalizeAnswerText(s string) string
func NormalizeText(s string) string
```

**每个 HTTP 请求的 URL / method / params / data / headers 必须与 Python 版逐字一致**，包括：
- `passport2.chaoxing.com/fanyalogin` (fid=-1, uname/password 用 AES, refer=https%3A%2F%2Fi.chaoxing.com, t=true, forbidotherlogin=0, validate="", doubleFactorLogin=0, independentId=0)
- `mooc2-ans.chaoxing.com/mooc2-ans/visit/courselistdata` (courseType=1, courseFolderId=0, query="", superstarClass=0, Referer 见 base.py:502)
- `mooc2-ans.chaoxing.com/mooc2-ans/visit/interaction` → 二级文件夹
- `mooc2-ans.chaoxing.com/mooc2-ans/mycourse/studentcourse?courseid=&clazzid=&cpi=&ut=s`
- `mooc1.chaoxing.com/mooc-ans/knowledge/cards` (clazzid, courseid, knowledgeid, ut=s, cpi, v=2025-0424-1038-3, mooc2=1, num=0..6 循环)
- `mooc1.chaoxing.com/ananas/status/{objectid}?k={fid}&flag=normal`
- `mooc1.chaoxing.com/mooc-ans/multimedia/log/a/{cpi}/{dtoken}` (全部参数 + rt 重试逻辑 + 403 验证码 + _recover_after_forbidden)
- `mooc1.chaoxing.com/ananas/job/document`, `/ananas/job/readv2`
- `mooc1.chaoxing.com/mooc-ans/api/work` + `mooc-ans/work/addStudentWorkNew` + `mooc-ans/work/record-list` + `mooc-ans/work/record-detail`
- `mooc1.chaoxing.com/mooc-ans/mycourse/studentstudyAjax`, `detect.chaoxing.com/api/monitor`, `fystat-ans.chaoxing.com/log/setlog...`
- `mobilelearn.chaoxing.com/v2/apis/active/student/activelist`, `/newsign/preSign`, `/pptSign/stuSignajax`
- `zhibo.chaoxing.com/saveTimePc?...`, `mooc1.chaoxing.com/ananas/live/liveinfo?...`

`StudyWork` 必须完整移植：重做轮次(work_redo_enabled/work_max_retries)、feedback_history 传给题库、覆盖率判断、pyFlag 提交/保存分支、`_check_work_result` 的 record-list/record-detail/兜底页面判断、满分兜底(>=99.95)、unknown 计数。
`StudyVideo` 必须完整移植：瞬间完成检测(isdrag=4)、30~90s 随机上报间隔、403 恢复(刷新 status + 重登)、rt 从 otherinfo `-rt_([1d])` 推导、速度倍率、`_speed` 上限 2.0。

## internal/engine — 任务调度 & 多账号并发

```go
package engine

type ChapterResult int
const (ChapterSuccess ChapterResult = iota; ChapterError; ChapterNotOpen; ChapterPending)

type Options struct {
	Speed         float64
	Jobs          int // 单账号并发章节数
	NotOpenAction string
	RetryInterval float64
	MaxTries      int
	AddLearningCount bool
	TargetCount   int
}

type Progress struct {
	Account   string `json:"account"`
	Course    string `json:"course"`
	Chapter   string `json:"chapter"`
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	Failed    int    `json:"failed"`
	State     string `json:"state"` // pending|running|done|error|stopped
}

type Hooks struct {
	OnProgress func(Progress)
	OnNotify   func(msg string) // 需要外部通知/事件时
	AskFunc    func(prompt string) bool
}

// Runner 单账号
type Runner struct{}
func NewRunner(c *api.Client, opt Options, log *logx.Logger, hooks Hooks) *Runner
func (r *Runner) RunCourses(courses []htmlx.Course) error
func (r *Runner) Stop()

// Manager 多账号并发调度
type Manager struct{}
func NewManager(log *logx.Logger, hooks Hooks) *Manager
// Start 为每个账号起一个 goroutine, 账号之间完全并发; 内部每个账号的章节并发由 Options.Jobs 控制
func (m *Manager) Start(tasks []AccountTask) error
func (m *Manager) Stop()
func (m *Manager) Status() []Progress
func (m *Manager) Running() bool

type AccountTask struct {
	Key      string // 账号唯一标识
	Account  api.Account
	UseCookies bool
	CookieFile string
	Courses  []htmlx.Course
	Options  Options
	Tiku     tiku.Provider
	Notify   notify.Notifier
}
```

对齐 `main.py` 的 `ChapterTask`/`JobProcessor`/`process_chapter`/`process_job` 语义：
- 优先级队列按 index 排序，成功即 task_done，ERROR/NOT_OPEN 进 retry 队列（tries>=5 放弃）
- `process_job` 的类型分发 + video 失败后自动重试 Audio
- 已完成任务点直接 SUCCESS
- `notopen_action`: retry / continue / ask(回调)

## internal/store — 账号持久化

```go
package store

type Account struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Password   string    `json:"password"` // 落盘前用本机密钥加密
	Remark     string    `json:"remark"`
	Enabled    bool      `json:"enabled"`
	UseCookies bool      `json:"use_cookies"`
	Cookies    string    `json:"cookies"`
	CourseIDs  []string  `json:"course_ids"`
	LastLogin  time.Time `json:"last_login"`
	LastStatus string    `json:"last_status"`
}

type Store struct{}
func Open(path string) (*Store, error) // 默认 ~/.chaoxing-go/accounts.json, 权限 0600
func (s *Store) List() []Account
func (s *Store) Get(id string) (Account, bool)
func (s *Store) Add(a Account) (Account, error) // 自动生成 ID
func (s *Store) Update(a Account) error
func (s *Store) Delete(id string) error
func (s *Store) Save() error
```
密码加密：`~/.chaoxing-go/key.bin` (0600) 存 32 字节随机密钥，AES-GCM 加密后 base64。

## GUI (Wails) — 由 app.go + frontend/ 实现

`main.go` (package main) + `app.go` 暴露给前端的绑定方法（返回 JSON 友好类型）：
- `ListAccounts() []store.Account`
- `AddAccount(a store.Account) (store.Account, error)` / `UpdateAccount` / `DeleteAccount(id)`
- `LoginAccount(id) (api.LoginResult, error)`
- `FetchCourses(id) ([]htmlx.Course, error)`（含缓存到账号）
- `StartRun(req RunRequest) error` / `StopRun()`
- `GetStatus() []engine.Progress` / `GetAccountsStatus()`
- `GetLogs(limit int) []logx.Entry`
- `LoadConfig() *config.Config` / `SaveConfig(*config.Config) error`
- `ExportResults() (string, error)`
Wails events: `log`, `progress`, `done`。

前端: `frontend/` 用 Vite + React + TypeScript + Ant Design v5，页面：账号管理 / 课程选择 / 任务与配置 / 实时日志。构建产物 `frontend/dist`。

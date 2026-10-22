// Package htmlx 负责解析超星返回的 HTML/JSON 片段，全部为纯函数，不做任何网络请求。
//
// 本文件只定义数据结构与函数签名（跨包契约）；具体解析逻辑见 parsers.go，
// 与 Python 原版 /root/chaoxing-ref/api/decode.py 逐条对齐。
package htmlx

// FontDecoder 超星加密字体解码器。由 internal/font 实现（结构化接口，避免包循环依赖）。
type FontDecoder interface {
	Decode(string) string
}

// Course 课程信息（对应 decode_course_list）
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

// Key 返回用于去重的 (courseId, clazzId) 组合
func (c Course) Key() string { return c.CourseID + "/" + c.ClazzID }

// Folder 课程文件夹（对应 decode_course_folder）
type Folder struct {
	ID     string `json:"id"`
	Rename string `json:"rename"`
}

// Point 章节点（对应 decode_course_point）
type Point struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	JobCount    string `json:"jobCount"`
	HasFinished bool   `json:"has_finished"`
	NeedUnlock  bool   `json:"need_unlock"`
}

// PointList 章节列表
type PointList struct {
	HasLocked bool    `json:"hasLocked"`
	Points    []Point `json:"points"`
}

// Job 任务点（对应 decode_course_card 的 attachments 解析结果）
type Job struct {
	Type                string         `json:"type"` // video|document|workid|read|live
	JobID               string         `json:"jobid"`
	Name                string         `json:"name"`
	OtherInfo           string         `json:"otherinfo"`
	Mid                 string         `json:"mid"`
	ObjectID            string         `json:"objectid"`
	Aid                 string         `json:"aid"`
	PlayTime            int            `json:"playTime"` // 毫秒
	Rt                  string         `json:"rt"`
	AttDuration         string         `json:"attDuration"`
	AttDurationEnc      string         `json:"attDurationEnc"`
	VideoFaceCaptureEnc string         `json:"videoFaceCaptureEnc"`
	JToken              string         `json:"jtoken"`
	Enc                 string         `json:"enc"`
	Title               string         `json:"title"` // read/live 任务使用
	Property            map[string]any `json:"property"`
}

// JobInfo 章节级别的元信息（对应 _extract_job_info）
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

// Question 章节检测题目
type Question struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Options      string `json:"options"`
	Type         string `json:"type"`     // single|multiple|completion|judgement|shortanswer|unknown
	TypeCode     string `json:"typeCode"` // 原始 data 属性（0/1/2/3/4）
	Answer       string `json:"answer"`
	AnswerSource string `json:"answerSource"` // cover|random|""
}

// WorkForm 章节检测提交表单
type WorkForm struct {
	Fields      map[string]string `json:"fields"` // form 中不含 "answer" 的 input name->value
	Questions   []Question        `json:"questions"`
	AnswerWQBID string            `json:"answerwqbid"`
}

// RecordQuestion 作答详情中的单题记录
type RecordQuestion struct {
	ID            string
	Title         string
	TypeLabel     string
	MyAnswer      string
	CorrectAnswer string
	ParseOK       bool
}

// RecordItem 作答记录列表项
type RecordItem struct {
	Times int
	Score float64
}

// 解析函数（DecodeCourseList / DecodeCourseFolder / DecodeCoursePoint /
// DecodeCourseCard / DecodeQuestions / DecodeWorkRecordList / DecodeWorkRecordDetail）
// 的实现见 parsers.go，与 Python 原版 /root/chaoxing-ref/api/decode.py 逐条对齐：
//
//	DecodeCourseList(htmlText string) []Course
//	DecodeCourseFolder(htmlText string) []Folder
//	DecodeCoursePoint(htmlText string) PointList
//	DecodeCourseCard(htmlText string) ([]Job, JobInfo)
//	DecodeQuestions(htmlText string, fd FontDecoder) WorkForm
//	DecodeWorkRecordList(htmlText string) []RecordItem
//	DecodeWorkRecordDetail(htmlText string) []RecordQuestion

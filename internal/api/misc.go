package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/butlanys/chaoxing-go/internal/consts"
	"github.com/butlanys/chaoxing-go/internal/crypto"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
)

// 本文件对应 Python 原版 api/base.py 的
// study_document / study_read / increase_chapter_learning_count / _access_chapter_for_count /
// _extract_and_send_setlog / _send_monitor_heartbeat / study_emptypage 相关、
// get_activity_list / pre_sign / sign_in_normal，
// 以及 api/live.py + api/live_process.py 的直播任务处理。

var (
	// 对齐 base.py study_document: r'nodeId_(.*?)-'
	reNodeID = regexp.MustCompile(`nodeId_(.*?)-`)

	// 对齐 base.py _extract_and_send_setlog:
	// r'<script[^>]+src="(https://fystat-ans\.chaoxing\.com/log/setlog[^"]+)"'
	reSetlogURL = regexp.MustCompile(`<script[^>]+src="(https://fystat-ans\.chaoxing\.com/log/setlog[^"]+)"`)
)

// buildURL 按顺序拼接查询参数（值做 URL 编码，参数顺序与 Python f-string 一致）。
func buildURL(base string, pairs ...[2]string) string {
	var sb strings.Builder
	sb.WriteString(base)
	for i, p := range pairs {
		if i == 0 {
			sb.WriteByte('?')
		} else {
			sb.WriteByte('&')
		}
		sb.WriteString(p[0])
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(p[1]))
	}
	return sb.String()
}

// jobProperty 读取任务点 property 中的字符串值。
func jobProperty(job htmlx.Job, key string) string {
	if job.Property == nil {
		return ""
	}
	v, ok := job.Property[key]
	if !ok || v == nil {
		return ""
	}
	return anyString(v)
}

// StudyDocument 学习文档任务（对齐 base.py study_document）。
func (c *Client) StudyDocument(course htmlx.Course, job htmlx.Job) StudyResult {
	knowledgeID := ""
	if m := reNodeID.FindStringSubmatch(job.OtherInfo); m != nil {
		knowledgeID = m[1]
	}

	u := buildURL(urlJobDocument,
		[2]string{"jobid", job.JobID},
		[2]string{"knowledgeid", knowledgeID},
		[2]string{"courseid", course.CourseID},
		[2]string{"clazzid", course.ClazzID},
		[2]string{"jtoken", job.JToken},
		[2]string{"_dc", crypto.Timestamp()},
	)

	res, err := c.doText(requestOpts{method: http.MethodGet, rawURL: u})
	if err != nil {
		c.log.Error("文档任务学习失败 -> %v", err)
		return ResultError
	}
	if res.statusCode != http.StatusOK {
		c.log.Error("文档任务学习失败 -> [%d]%s", res.statusCode, res.body)
		return ResultError
	}
	return ResultSuccess
}

// StudyRead 学习阅读任务（对齐 base.py study_read），仅完成任务点，不增长时长。
func (c *Client) StudyRead(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo) StudyResult {
	params := url.Values{}
	params.Set("jobid", job.JobID)
	params.Set("knowledgeid", info.KnowledgeID)
	params.Set("jtoken", job.JToken)
	params.Set("courseid", course.CourseID)
	params.Set("clazzid", course.ClazzID)

	res, err := c.doText(requestOpts{method: http.MethodGet, rawURL: urlJobReadV2, params: params})
	if err != nil {
		c.log.Error("阅读任务学习失败 -> %v", err)
		return ResultError
	}
	if res.statusCode != http.StatusOK {
		c.log.Error("阅读任务学习失败 -> [%d]%s", res.statusCode, res.body)
		return ResultError
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(res.body), &payload); err != nil {
		c.log.Error("阅读任务学习失败 -> %s", res.body)
		return ResultError
	}
	c.log.Info("阅读任务学习 -> %v", payload["msg"])
	return ResultSuccess
}

// accessChapterForCount 访问章节页面用于累计学习次数（对齐 base.py _access_chapter_for_count）。
func (c *Client) accessChapterForCount(course htmlx.Course, point htmlx.Point) (string, bool) {
	res, err := c.doText(requestOpts{
		method:  http.MethodGet,
		rawURL:  urlStudentStudyAjax,
		params:  studyAjaxParams(course, point),
		timeout: 8 * time.Second,
	})
	if err != nil {
		c.log.Error("章节访问失败 -> [%v]%s", err, point.Title)
		return "", false
	}
	if res.statusCode != http.StatusOK {
		c.log.Error("章节访问失败 -> [%d]%s", res.statusCode, point.Title)
		return "", false
	}
	c.log.Info("章节访问成功 -> %s", point.Title)
	return res.body, true
}

// extractAndSendSetlog 从 studentstudyAjax 返回的 HTML 中提取 setlog URL 并执行
// （对齐 base.py _extract_and_send_setlog，该 URL 是记录章节学习次数的关键 API）。
func (c *Client) extractAndSendSetlog(htmlText string) {
	m := reSetlogURL.FindStringSubmatch(htmlText)
	if m == nil {
		c.log.Trace("未在响应中找到 setlog URL")
		return
	}

	res, err := c.doText(requestOpts{method: http.MethodGet, rawURL: m[1], timeout: 5 * time.Second})
	if err != nil {
		c.log.Trace("Setlog failed (non-critical): %v", err)
		return
	}
	c.log.Trace("Setlog sent -> %d", res.statusCode)
}

// randomJSONPCallback 对齐 Python f"jsonp{secrets.randbelow(10**21 - 10**20) + 10**20}"
// 即 21 位、首位非零的随机数字。
func randomJSONPCallback() string {
	var sb strings.Builder
	sb.WriteString("jsonp")
	sb.WriteByte(byte('1' + rand.Intn(9))) // 首位 1-9
	for i := 0; i < 20; i++ {
		sb.WriteByte(byte('0' + rand.Intn(10)))
	}
	return sb.String()
}

// chapterDwellTime 访问章节后的停留时长，对齐 Python time.sleep(30)
// （模拟前端 setInterval(fn, 30000) 的间隔）。声明为变量，便于单元测试注入零等待。
var chapterDwellTime = 30 * time.Second

// sendMonitorHeartbeat 发送章节监控心跳包（对齐 base.py _send_monitor_heartbeat）。
func (c *Client) sendMonitorHeartbeat(course htmlx.Course, point htmlx.Point) {
	params := url.Values{}
	params.Set("version", crypto.Timestamp())
	params.Set("refer", "http://i.mooc.chaoxing.com")
	params.Set("from", "")
	params.Set("fid", c.FID())
	params.Set("jsoncallback", randomJSONPCallback())
	params.Set("t", crypto.Timestamp())

	refererURL := fmt.Sprintf("https://mooc1.chaoxing.com/mycourse/studentstudy?chapterId=%s&courseId=%s&clazzid=%s&cpi=%s&mooc2=1",
		point.ID, course.CourseID, course.ClazzID, course.CPI)

	res, err := c.doText(requestOpts{
		method:  http.MethodGet,
		rawURL:  urlMonitor,
		params:  params,
		headers: map[string]string{"Referer": refererURL},
		timeout: 5 * time.Second,
	})
	if err != nil {
		c.log.Trace("Monitor heartbeat failed (non-critical): %v", err)
		return
	}
	c.log.Trace("Monitor heartbeat sent -> %d", res.statusCode)
}

// IncreaseChapterLearningCount 增加课程章节学习次数（对齐 base.py increase_chapter_learning_count）。
// onProgress 可为 nil。
func (c *Client) IncreaseChapterLearningCount(course htmlx.Course, points []htmlx.Point, target int, onProgress func(n, target int)) StudyResult {
	total := 0
	consecutiveFailures := 0
	const maxConsecutiveFailures = 10

	c.log.Info("开始增加章节学习次数, 目标总次数: %d, 章节数: %d", target, len(points))
	if len(points) == 0 {
		c.log.Warn("章节列表为空, 跳过章节学习次数增加")
		return ResultSuccess
	}

	for total < target {
		for _, point := range points {
			if total >= target {
				break
			}
			c.RateLimitRandom(0, 0.2)

			htmlText, ok := c.accessChapterForCount(course, point)
			if !ok {
				c.log.Error("章节学习次数增加失败, 当前章节: %s", point.Title)
				consecutiveFailures++
				if consecutiveFailures >= maxConsecutiveFailures {
					c.log.Error("章节学习次数增加连续失败 %d 次, 终止任务", consecutiveFailures)
					return ResultError
				}
				continue
			}
			consecutiveFailures = 0

			// 第 1 步：从 HTML 中提取 setlog URL 并执行（真正的计次 API）
			c.extractAndSendSetlog(htmlText)
			// 第 2 步：立即发送 monitor 心跳包（模拟 fn()）
			c.sendMonitorHeartbeat(course, point)
			// 第 3 步：停留 30 秒（模拟前端 setInterval 间隔）
			time.Sleep(chapterDwellTime)
			// 第 4 步：再次发送 monitor 心跳包（模拟 setInterval 触发的第二次心跳）
			c.sendMonitorHeartbeat(course, point)

			total++
			if onProgress != nil {
				onProgress(total, target)
			}
			c.log.Info("章节学习次数进度: %d/%d", total, target)
		}
	}

	c.log.Info("章节学习次数增加完成, 共完成: %d 次", total)
	return ResultSuccess
}

// ActivityList 获取签到活动列表（对齐 base.py get_activity_list）。
// 原版使用 allow_redirects=False，这里同样不跟随重定向。
func (c *Client) ActivityList(course htmlx.Course) ([]Activity, error) {
	params := url.Values{}
	params.Set("fid", c.FID())
	params.Set("courseId", course.CourseID)
	params.Set("classId", course.ClazzID)
	params.Set("showNotStartedActive", "0")
	params.Set("_", crypto.Timestamp())

	res, err := c.doText(requestOpts{
		method:     http.MethodGet,
		rawURL:     urlActiveList,
		params:     params,
		noRedirect: true,
	})
	if err != nil {
		return nil, fmt.Errorf("获取活动列表失败: %w", err)
	}
	if res.statusCode != http.StatusOK {
		c.log.Error("Failed to get activity list, return code: %d", res.statusCode)
		return []Activity{}, nil
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(res.body), &payload); err != nil {
		c.log.Error("解析活动列表失败: %v", err)
		return []Activity{}, nil
	}
	if jsonInt(payload["result"]) != 1 {
		c.log.Error("Unknown status: %v %v", payload["result"], payload["errorMsg"])
		return []Activity{}, nil
	}

	data, _ := payload["data"].(map[string]any)
	list, _ := data["activeList"].([]any)
	out := make([]Activity, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Activity{
			ID:        anyString(m["id"]),
			Name:      anyString(m["name"]),
			Status:    jsonInt(m["status"]),
			Type:      jsonInt(m["type"]),
			OtherID:   anyString(m["otherId"]),
			StartTime: anyString(m["startTime"]),
		})
	}
	return out, nil
}

// SignIn 签到（对齐 base.py pre_sign + sign_in_normal 默认参数），返回签到响应文本。
func (c *Client) SignIn(course htmlx.Course, activityID string) (string, error) {
	// 1. pre_sign
	preParams := url.Values{}
	preParams.Set("general", "1")
	preParams.Set("sys", "1")
	preParams.Set("ls", "1")
	preParams.Set("appType", "15")
	preParams.Set("tid", "")
	preParams.Set("ut", "s")
	preParams.Set("uid", c.UID())
	preParams.Set("activePrimaryId", activityID)
	preParams.Set("courseId", course.CourseID)
	preParams.Set("classId", course.ClazzID)

	pre, err := c.doText(requestOpts{method: http.MethodGet, rawURL: urlPreSign, params: preParams})
	if err != nil {
		return "", fmt.Errorf("获取签到页面失败: %w", err)
	}
	if pre.statusCode != http.StatusOK {
		c.log.Error("Failed to get sign in, return code: %d message: %s", pre.statusCode, pre.body)
	}

	// 2. stuSignajax
	params := url.Values{}
	params.Set("activeId", activityID)
	params.Set("uid", c.UID())
	params.Set("fid", c.FID())
	params.Set("courseId", course.CourseID)
	params.Set("classId", course.ClazzID)
	params.Set("clientip", "")
	params.Set("objectId", "aaa")
	params.Set("name", "")
	params.Set("useragent", "")
	params.Set("latitude", "-1")
	params.Set("longitude", "-1")
	params.Set("appType", "15")

	res, err := c.doText(requestOpts{method: http.MethodGet, rawURL: urlStuSignAjax, params: params})
	if err != nil {
		return "", fmt.Errorf("提交签到失败: %w", err)
	}
	if res.statusCode != http.StatusOK {
		c.log.Error("Failed to get sign in, return code: %d message: %s", res.statusCode, res.body)
	}
	return res.body, nil
}

// LiveStatus 查询直播状态（含总时长），对齐 api/live.py 的 Live.get_status。
func (c *Client) LiveStatus(course htmlx.Course, job htmlx.Job, knowledgeID string) (map[string]any, error) {
	liveID := jobProperty(job, "liveId")
	userID := c.UID()
	if liveID == "" || userID == "" || course.ClazzID == "" || knowledgeID == "" {
		c.log.Error("缺少直播状态查询必要参数")
		return nil, errors.New("缺少直播状态查询必要参数")
	}

	params := url.Values{}
	params.Set("liveid", liveID)
	params.Set("userid", userID)
	params.Set("clazzid", course.ClazzID)
	params.Set("knowledgeid", knowledgeID)
	params.Set("courseid", course.CourseID)
	params.Set("jobid", jobProperty(job, "_jobid"))
	params.Set("ut", "s")

	res, err := c.doText(requestOpts{
		method:  http.MethodGet,
		rawURL:  urlLiveInfo,
		params:  params,
		headers: consts.LiveHeaders,
		timeout: 10 * time.Second,
	})
	if err != nil {
		c.log.Error("获取直播状态失败: %v", err)
		return nil, err
	}
	if res.statusCode != http.StatusOK {
		c.log.Error("获取直播状态失败: HTTP %d", res.statusCode)
		return nil, fmt.Errorf("获取直播状态失败: HTTP %d", res.statusCode)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(res.body), &payload); err != nil {
		c.log.Error("获取直播状态失败: %v", err)
		return nil, err
	}
	return payload, nil
}

// liveDoFinish 提交直播观看时长（对齐 api/live.py Live.do_finish）。
func (c *Client) liveDoFinish(course htmlx.Course, streamName, vdoid string) bool {
	u := buildURL(urlSaveTimePc,
		[2]string{"streamName", streamName},
		[2]string{"vdoid", vdoid},
		[2]string{"userId", c.UID()},
		[2]string{"isStart", "0"},
		[2]string{"t", crypto.Timestamp()},
		[2]string{"courseId", course.CourseID},
	)

	res, err := c.doText(requestOpts{
		method:  http.MethodGet,
		rawURL:  u,
		headers: consts.LiveHeaders,
		timeout: 10 * time.Second,
	})
	if err != nil {
		c.log.Error("提交直播时长失败: %v", err)
		return false
	}
	c.log.Debug("直播时长提交响应: %s", res.body)
	// 对齐原版 raise_for_status()：非 2xx 视为失败
	if res.statusCode < 200 || res.statusCode > 299 {
		c.log.Error("提交直播时长失败: HTTP %d", res.statusCode)
		return false
	}
	// 响应为 @success 表示提交成功
	return strings.TrimSpace(res.body) == "@success"
}

// RunLive 处理直播任务（对齐 api/live_process.py LiveProcessor.run_live）：
// 循环提交观看时长直到达到直播总时长。
func (c *Client) RunLive(course htmlx.Course, job htmlx.Job, knowledgeID string, speed float64) StudyResult {
	streamName := jobProperty(job, "streamName")
	vdoid := jobProperty(job, "vdoid")
	if streamName == "" || vdoid == "" {
		c.log.Error("缺少直播必要参数，无法提交时长")
		return ResultError
	}
	if speed <= 0 {
		speed = 1.0
	}

	// 获取直播状态（包含总时长）
	liveStatus, err := c.LiveStatus(course, job, knowledgeID)
	if err != nil || liveStatus == nil {
		c.log.Error("直播状态获取失败，无法继续")
		return ResultError
	}

	// 解析直播总时长（单位：秒）
	duration := 0.0
	if temp, ok := liveStatus["temp"].(map[string]any); ok {
		if data, ok := temp["data"].(map[string]any); ok {
			duration = jsonFloat(data["duration"])
		}
	}
	if duration == 0 {
		c.log.Warn("无法获取直播总时长，默认按30分钟处理")
		duration = 30 * 60 // 默认30分钟
	}

	// 根据播放速度调整所需时间
	adjustedDuration := duration / speed
	totalMinutes := (int(adjustedDuration) + 59) / 60 // 转换为分钟（向上取整）
	c.log.Info("开始刷取直播'%s'，总时长%d分钟（已根据倍速调整）", job.Name, totalMinutes)

	for i := 0; i < totalMinutes; i++ {
		c.log.Info("直播'%s'已观看%d/%d分钟", job.Name, i+1, totalMinutes)
		if !c.liveDoFinish(course, streamName, vdoid) {
			c.log.Warn("第%d分钟时长提交失败，将重试", i+1)
			// 失败重试一次
			time.Sleep(5 * time.Second)
			_ = c.liveDoFinish(course, streamName, vdoid)
		}
		// 根据倍速调整间隔时间
		time.Sleep(time.Duration(59 / speed * float64(time.Second)))
	}

	c.log.Success("直播'%s'时长刷取完成", job.Name)
	return ResultSuccess
}

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/butlanys/chaoxing-go/internal/font"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
	"github.com/butlanys/chaoxing-go/internal/logx"
	"github.com/butlanys/chaoxing-go/internal/tiku"
)

// 本文件对应 Python 原版 api/base.py 的 study_work / _check_work_result，
// 以及 _parse_work_record_list / _parse_work_record_detail（后者由 internal/htmlx 实现）。

// errWorkNotCreated 教师未创建完成该测验（对应 Python 的 PermissionError 分支）
var errWorkNotCreated = errors.New("教师未创建完成该测验")

// reWorkScore 对齐 base.py: r'本次成绩<i>([\d.]+)</i>分'
var reWorkScore = regexp.MustCompile(`本次成绩<i>([\d.]+)</i>分`)

// workSubmitHeaders 提交章节检测时的完整请求头（逐字对齐 base.py 1210-1224 行）。
var workSubmitHeaders = map[string]string{
	"Host":               "mooc1.chaoxing.com",
	"sec-ch-ua-platform": `"Windows"`,
	"X-Requested-With":   "XMLHttpRequest",
	"User-Agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 Edg/129.0.0.0",
	"Accept":             "application/json, text/javascript, */*; q=0.01",
	"sec-ch-ua":          `"Microsoft Edge";v="129", "Not=A?Brand";v="8", "Chromium";v="129"`,
	"Content-Type":       "application/x-www-form-urlencoded; charset=UTF-8",
	"sec-ch-ua-mobile":   "?0",
	"Origin":             "https://mooc1.chaoxing.com",
	"Sec-Fetch-Site":     "same-origin",
	"Sec-Fetch-Mode":     "cors",
	"Sec-Fetch-Dest":     "empty",
	"Accept-Language":    "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6,ja;q=0.5",
}

// 字体映射表懒加载（对齐 base.py 每次构造 FontDecoder 时读取 FontHashDAO）
var (
	fontTableOnce sync.Once
	fontTable     *font.Table
)

// loadFontTable 懒加载字体映射表；优先磁盘 resource/font_map_table.json，
// 找不到时回退到编译期内嵌的映射表（保证单文件可执行程序开箱可用）。失败返回 nil。
func loadFontTable() *font.Table {
	fontTableOnce.Do(func() {
		t, err := font.LoadTable("resource/font_map_table.json")
		if err != nil {
			logx.Default().Debug("加载字体映射表失败(题目将不解密): %v", err)
			return
		}
		fontTable = t
	})
	return fontTable
}

// fontDecoderFor 依据题目页 HTML 构造字体解码器；无加密字体时返回 nil。
func fontDecoderFor(htmlText string) htmlx.FontDecoder {
	table := loadFontTable()
	if table == nil {
		return nil
	}
	decoder, err := font.NewDecoder(htmlText, table)
	if err != nil {
		return nil
	}
	return decoder
}

// workAPIParams 构造 /mooc-ans/api/work 的查询参数（fetch 与兜底判断共用）。
func (c *Client) workAPIParams(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo) url.Values {
	params := url.Values{}
	params.Set("api", "1")
	params.Set("workId", strings.Replace(job.JobID, "work-", "", 1))
	params.Set("jobid", job.JobID)
	params.Set("originJobId", job.JobID)
	params.Set("needRedirect", "true")
	params.Set("skipHeader", "true")
	params.Set("knowledgeid", info.KnowledgeID)
	params.Set("ktoken", info.KToken)
	params.Set("cpi", info.CPI)
	params.Set("ut", "s")
	params.Set("clazzId", course.ClazzID)
	params.Set("type", "")
	params.Set("enc", job.Enc)
	params.Set("mooc2", "1")
	params.Set("courseid", course.CourseID)
	return params
}

// fetchWorkQuestions 获取章节检测题目页（对齐 base.py fetch_response_with_retry）：
// 最多 3 次尝试、间隔 1s；「教师未创建完成该测验」不重试，直接返回 errWorkNotCreated。
func (c *Client) fetchWorkQuestions(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo) (htmlx.WorkForm, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		res, err := c.doText(requestOpts{
			method: http.MethodGet,
			rawURL: urlWorkAPI,
			params: c.workAPIParams(course, job, info),
		})
		if err != nil {
			lastErr = err
			c.log.Warn("无效响应 (%v), 重试中...", err)
		} else {
			// 未创建完成该测验则不进行答题，目前遇到的情况是未创建完成等同于没题目
			if strings.Contains(res.body, "教师未创建完成该测验") {
				return htmlx.WorkForm{}, errWorkNotCreated
			}

			form := htmlx.DecodeQuestions(res.body, fontDecoderFor(res.body))
			if res.statusCode == http.StatusOK && len(form.Questions) > 0 {
				return form, nil
			}
			lastErr = fmt.Errorf("请求返回无效数据 (Code: %d)", res.statusCode)
			c.log.Warn("无效响应 (Code: %d), 重试中...", res.statusCode)
		}
		if attempt < 3 {
			time.Sleep(1 * time.Second)
		}
	}
	if lastErr == nil {
		lastErr = errors.New("获取章节检测题目失败")
	}
	return htmlx.WorkForm{}, lastErr
}

// StudyWork 章节检测（对齐 base.py study_work，完整移植重做轮次/覆盖率/pyFlag/成绩校验）。
func (c *Client) StudyWork(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo) StudyResult {
	// 题库未配置或已停用 -> 直接跳过（对齐 Python tiku.DISABLE）
	if c.tiku == nil || c.tiku.Disabled() {
		return ResultSuccess
	}

	// 章节检测最大重做次数（答错后收集错误反馈并重新提交，直到全对）
	// work_redo_enabled=false 时为单轮模式: 提交+检查+报告成绩, 不自动重做
	maxRetries := 0
	if c.opt.WorkRedoEnabled {
		maxRetries = c.opt.WorkMaxRetries
		if maxRetries < 1 {
			maxRetries = 1
		}
	}
	queryDelay := c.opt.QueryDelay

	var feedbackHistory []string
	lastSubmittedScore := 0.0

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			c.log.Warn("章节检测重做第 %d/%d 轮，携带上一轮错误反馈重新作答", attempt, maxRetries)
			time.Sleep(2 * time.Second)
		}

		// 1. 获取题目
		form, err := c.fetchWorkQuestions(course, job, info)
		if err != nil {
			if errors.Is(err, errWorkNotCreated) {
				c.log.Warn("跳过章节检测: %v", err)
				return ResultSuccess
			}
			if attempt > 0 {
				c.log.Error("重做轮无法获取题目(上一轮提交成绩 %v 分), 该课程可能仅允许作答一次: %v", lastSubmittedScore, err)
			} else {
				c.log.Error("获取章节检测题目失败, 达到最大重试次数: %v", err)
			}
			return ResultError
		}

		// 2. 设置上一轮错误反馈（供 AI 重新作答时参考）
		if len(feedbackHistory) > 0 {
			c.tiku.SetWorkFeedback(feedbackHistory)
			c.log.Debug("已将上一轮错误反馈设置到题库")
		}

		// 3. 搜题
		totalQuestions := len(form.Questions)
		foundAnswers := 0

		queryQuestions := make([]tiku.Question, 0, totalQuestions)
		for _, q := range form.Questions {
			queryQuestions = append(queryQuestions, tiku.Question{
				ID:      q.ID,
				Title:   q.Title,
				Options: q.Options,
				Type:    q.Type,
			})
		}
		answers := c.tiku.QueryAll(queryQuestions, queryDelay)
		if len(answers) != totalQuestions {
			c.log.Error("题库返回的答案数量（%d）与题目数量（%d）不匹配，正在补齐或截断以防错位！", len(answers), totalQuestions)
			if len(answers) < totalQuestions {
				padded := make([]string, totalQuestions)
				copy(padded, answers)
				answers = padded
			} else {
				answers = answers[:totalQuestions]
			}
		}

		answersOut := make([]string, totalQuestions)
		sources := make([]string, totalQuestions)

		for i, q := range form.Questions {
			res := answers[i]
			c.log.Debug("当前题目信息 -> %s", q.Title)

			answer := ""
			if res == "" {
				// 随机答题
				answer = RandomAnswer(q.Options, q.Type)
				sources[i] = "random"
			} else {
				switch q.Type {
				case "multiple":
					// 多选处理
					optionsList := MultiCut(q.Options)
					resList := MultiCut(res)
					if resList != nil && optionsList != nil {
						for _, a := range CleanRes(resList) {
							matched := false
							for _, o := range optionsList {
								// 去掉各种符号和前面 ABCD 的答案应当是选项的子序列
								if IsSubsequence(a, o) {
									answer += firstRune(o)
									matched = true
									break // 找到匹配项后立即停止，防止重复添加
								}
							}
							if !matched {
								if letter := BestOptionBySimilarity(a, optionsList, 0.8); letter != "" {
									answer += letter
								}
							}
						}
						// 对答案进行排序, 否则会提交失败
						answer = sortUniqueRunes(answer)
					}
					// 如果分割失败那么就直接到下面去随机选
				case "single":
					// 单选也进行切割，主要是防止返回的答案有异常字符
					optionsList := MultiCut(q.Options)
					if optionsList != nil {
						tRes := CleanRes([]string{res})
						if len(tRes) > 0 {
							for _, o := range optionsList {
								if IsSubsequence(tRes[0], o) {
									answer = firstRune(o)
									break
								}
							}
							if answer == "" {
								answer = BestOptionBySimilarity(tRes[0], optionsList, 0.8)
							}
						}
					}
				case "judgement":
					if c.tiku.JudgementSelect(res) {
						answer = "true"
					} else {
						answer = "false"
					}
				case "completion":
					answer = res
				default:
					// 其他类型直接使用答案（目前仅知有简答题）
					answer = res
				}

				if answer == "" { // 检查 answer 是否为空
					c.log.Warn("找到答案但答案未能匹配 -> %s\t随机选择答案", res)
					answer = RandomAnswer(q.Options, q.Type)
					sources[i] = "random"
				} else {
					c.log.Info("成功获取到答案：%s", answer)
					sources[i] = "cover"
					foundAnswers++
				}
			}
			answersOut[i] = answer
			c.log.Info("%s 填写答案为 %s", q.Title, answer)
		}

		coverRate := float64(foundAnswers) / float64(totalQuestions) * 100
		c.log.Info("章节检测题库覆盖率： %.0f%%", coverRate)

		// 提交模式: 与题库绑定, 留空直接提交, "1" 保存但不提交
		pyFlag := ""
		if c.tiku.SubmitMode() == "1" {
			pyFlag = "1"
		} else if c.tiku.IsManual() || coverRate >= c.tiku.CoverRate()*100 || c.rollbackTimes() >= 1 {
			pyFlag = ""
		} else {
			pyFlag = "1"
			c.log.Info("章节检测题库覆盖率低于%.0f%%，不予提交", c.tiku.CoverRate()*100)
		}

		// 组建提交表单
		formData := url.Values{}
		for k, v := range form.Fields {
			formData.Set(k, v)
		}
		for i, q := range form.Questions {
			if pyFlag == "1" {
				if sources[i] == "cover" {
					formData.Set("answer"+q.ID, answersOut[i])
				} else {
					formData.Set("answer"+q.ID, "")
				}
			} else {
				formData.Set("answer"+q.ID, answersOut[i])
			}
			formData.Set("answertype"+q.ID, q.TypeCode)
		}
		formData.Set("answerwqbid", form.AnswerWQBID)
		formData.Set("pyFlag", pyFlag)

		// 4. 提交
		action := "提交"
		if pyFlag == "1" {
			action = "保存"
		}
		submitRes, err := c.doText(requestOpts{
			method:  http.MethodPost,
			rawURL:  urlWorkSubmit,
			form:    formData,
			headers: workSubmitHeaders,
		})
		if err != nil {
			c.log.Error("%s答题失败 -> %v", action, err)
			return ResultError
		}
		if submitRes.statusCode != http.StatusOK {
			c.log.Error("%s答题失败 -> %s", action, submitRes.body)
			return ResultError
		}
		var submitJSON map[string]any
		if err := json.Unmarshal([]byte(submitRes.body), &submitJSON); err != nil {
			c.log.Error("%s答题失败 -> %s", action, submitRes.body)
			return ResultError
		}
		if jsonBool(submitJSON["status"]) {
			c.log.Info("%s答题成功 -> %v", action, submitJSON["msg"])
		} else {
			c.log.Error("%s答题失败 -> %v", action, submitJSON["msg"])
			return ResultError
		}

		// 5. 若只是保存未提交，无法判断成绩，保持原有行为
		if pyFlag == "1" {
			return ResultSuccess
		}

		// 6. 提交后检查成绩：若未全部正确，则收集错误反馈并重新作答提交
		resultInfo, err := c.checkWorkResult(course, job, info, formData)
		if err != nil || resultInfo == nil {
			// 无法获取成绩详情（如接口异常），按原行为返回成功，避免误判失败
			return ResultSuccess
		}

		score := jsonFloat(resultInfo["score"])
		unknown := jsonInt(resultInfo["unknown"])
		if jsonBool(resultInfo["all_correct"]) {
			c.log.Info("章节检测全部正确（成绩 %v 分），通过！", score)
			return ResultSuccess
		}
		if score >= 99.95 {
			// 逐题比对存在格式差异或解析失配, 但满分成绩说明实际全对, 以分数为准
			if unknown > 0 {
				c.log.Warn("成绩 %v 分达到满分, 以分数为准通过（%d 题答案字段未能解析, 建议人工复核）", score, unknown)
			} else {
				c.log.Info("逐题比对存在格式差异, 但成绩 %v 分达到满分, 以分数为准, 通过！", score)
			}
			return ResultSuccess
		}
		if unknown > 0 {
			// 解析失配且未达满分: 判定不可信, 不触发重做, 保留已提交成绩
			c.log.Warn("%d 题答案字段解析失败且成绩 %v 分未达满分, 跳过重做判定", unknown, score)
			return ResultSuccess
		}

		// 7. 未全对：收集错误反馈，进入下一轮重做
		feedbackHistory = toStringSlice(resultInfo["feedback"])
		wrongCount := len(feedbackHistory)
		if attempt >= maxRetries {
			// 已是最后一轮(含单轮模式), 保留已提交成绩, 不再触发无意义的重做
			c.log.Warn("章节检测有 %d/%d 题回答错误（成绩 %v 分），已保留本次成绩, 请人工检查处理",
				wrongCount, totalQuestions, score)
			return ResultSuccess
		}
		c.log.Warn("章节检测有 %d/%d 题回答错误（成绩 %v 分），已将错误反馈给AI，准备重新作答提交 (第 %d/%d 轮)",
			wrongCount, totalQuestions, score, attempt+1, maxRetries+1)
		c.incRollback()
		lastSubmittedScore = score
	}

	// 达到最大重试次数仍未全对
	c.log.Error("章节检测重试 %d 次仍未全部正确，请人工检查处理", maxRetries+1)
	return ResultError
}

// toStringSlice 把 resultInfo["feedback"] 转成 []string（保证非 nil）。
func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, anyString(item))
		}
		return out
	}
	return []string{}
}

// checkWorkResult 章节检测提交后查询最新一次作答的成绩与对错详情（对齐 base.py _check_work_result）。
// 返回 nil 表示无法获取成绩详情。
func (c *Client) checkWorkResult(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo, form url.Values) (map[string]any, error) {
	workID := form.Get("workId")
	if workID == "" {
		workID = form.Get("workRelationId")
	}
	if workID == "" {
		workID = strings.Replace(job.JobID, "work-", "", 1)
	}
	workAnswerID := form.Get("workAnswerId")
	courseID := course.CourseID
	classID := course.ClazzID
	cpi := course.CPI
	if cpi == "" {
		cpi = form.Get("cpi")
	}

	// 1. 获取作答记录列表（提交后服务端异步生成记录，需稍作等待并多次重试）
	var records []htmlx.RecordItem
	for attempt := 0; attempt < 5; attempt++ {
		params := url.Values{}
		params.Set("courseId", courseID)
		params.Set("classId", classID)
		params.Set("workId", workID)
		params.Set("workAnswerId", workAnswerID)
		params.Set("cpi", cpi)
		params.Set("api", "1")
		params.Set("mooc2", "1")
		params.Set("ut", "s")

		res, err := c.doText(requestOpts{
			method:  http.MethodGet,
			rawURL:  urlWorkRecordList,
			params:  params,
			timeout: 20 * time.Second,
		})
		if err != nil {
			c.log.Warn("获取章节检测作答记录失败 (第%d次): %v", attempt+1, err)
		} else {
			records = htmlx.DecodeWorkRecordList(res.body)
			if len(records) > 0 {
				break
			}
		}
		if attempt < 4 {
			time.Sleep(1500 * time.Millisecond)
		}
	}

	if len(records) == 0 {
		// 兜底：重新访问题目页判断是否已通过（详情页=已提交有成绩；可编辑页=未通过可重做）
		return c.fallbackWorkResult(course, job, info, cpi)
	}

	latestTimes := records[0].Times
	for _, r := range records {
		if r.Times > latestTimes {
			latestTimes = r.Times
		}
	}
	// 对齐 Python dict(records).get(latest_times, 0.0)：同序号取最后一次
	latestScore := 0.0
	for _, r := range records {
		if r.Times == latestTimes {
			latestScore = r.Score
		}
	}

	// 2. 获取最新一次作答详情（含每道题对错与正确答案）
	params := url.Values{}
	params.Set("courseId", courseID)
	params.Set("classId", classID)
	params.Set("workId", workID)
	params.Set("workAnswerId", workAnswerID)
	params.Set("times", strconv.Itoa(latestTimes))
	params.Set("cpi", cpi)
	params.Set("ut", "s")
	params.Set("isdisplaytable", "0")
	params.Set("firstHeader", "2")
	params.Set("isWork", "false")
	params.Set("workSystem", "0")
	params.Set("api", "1")
	params.Set("archive", "false")
	params.Set("mooc2", "1")

	res, err := c.doText(requestOpts{
		method:  http.MethodGet,
		rawURL:  urlWorkRecordDetail,
		params:  params,
		timeout: 20 * time.Second,
	})
	if err != nil {
		c.log.Warn("获取章节检测作答详情失败: %v", err)
		return nil, err
	}

	detail := htmlx.DecodeWorkRecordDetail(res.body)
	if len(detail) == 0 {
		c.log.Warn("章节检测作答详情解析为空，跳过成绩检查")
		return nil, nil
	}

	allCorrect, unknown, feedback := compareWorkDetail(detail)
	c.log.Debug("章节检测成绩: %v 分, 全部正确: %v, 错题数: %d, 未解析题数: %d",
		latestScore, allCorrect, len(feedback), unknown)

	return map[string]any{
		"all_correct": allCorrect,
		"feedback":    feedback,
		"score":       latestScore,
		"times":       latestTimes,
		"unknown":     unknown,
	}, nil
}

// fallbackWorkResult 无法获取作答记录时的兜底判断（对齐 base.py _check_work_result 的兜底分支）。
func (c *Client) fallbackWorkResult(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo,
	cpi string) (map[string]any, error) {

	c.log.Warn("无法获取章节检测作答记录，尝试通过题目页状态判断")

	params := url.Values{}
	params.Set("api", "1")
	params.Set("workId", strings.Replace(job.JobID, "work-", "", 1))
	params.Set("jobid", job.JobID)
	params.Set("originJobId", job.JobID)
	params.Set("needRedirect", "true")
	params.Set("skipHeader", "true")
	params.Set("knowledgeid", info.KnowledgeID)
	params.Set("ktoken", info.KToken)
	fallbackCPI := info.CPI
	if fallbackCPI == "" {
		fallbackCPI = cpi
	}
	params.Set("cpi", fallbackCPI)
	params.Set("ut", "s")
	params.Set("clazzId", course.ClazzID)
	params.Set("type", "")
	params.Set("enc", job.Enc)
	params.Set("mooc2", "1")
	params.Set("courseid", course.CourseID)

	res, err := c.doText(requestOpts{
		method:  http.MethodGet,
		rawURL:  urlWorkAPI,
		params:  params,
		timeout: 20 * time.Second,
	})
	if err != nil {
		c.log.Warn("兜底判断章节检测状态失败: %v", err)
		return nil, err
	}

	html := res.body
	if strings.Contains(html, "answerwqbid") {
		// 可编辑页面：说明未全部正确，可重新作答（该判定为页面状态推断, 未核对实际成绩）
		c.log.Warn("题目页仍可编辑, 推断章节检测未全部正确(未核对成绩)")
		return map[string]any{
			"all_correct": false,
			"feedback":    []string{},
			"score":       0.0,
			"times":       0,
		}, nil
	}
	if strings.Contains(html, "正确答案") && strings.Contains(html, "我的答案") {
		// 已提交详情页：解析成绩与对错
		detail := htmlx.DecodeWorkRecordDetail(html)
		if len(detail) > 0 {
			allCorrect, unknown, feedback := compareWorkDetail(detail)
			score := 0.0
			if m := reWorkScore.FindStringSubmatch(html); m != nil {
				if v, perr := strconv.ParseFloat(m[1], 64); perr == nil {
					score = v
				}
			}
			return map[string]any{
				"all_correct": allCorrect,
				"feedback":    feedback,
				"score":       score,
				"times":       0,
				"unknown":     unknown,
			}, nil
		}
	}
	return nil, nil
}

// compareWorkDetail 逐题比对作答详情，返回 (是否全对, 未解析题数, 错误反馈)。
// 对齐 base.py：ParseOK 为 false 的题计入 unknown 并判定为未全对；
// 答案归一化后不一致的题追加四行反馈文本。
func compareWorkDetail(detail []htmlx.RecordQuestion) (bool, int, []string) {
	feedback := []string{}
	allCorrect := true
	unknown := 0

	for _, q := range detail {
		if !q.ParseOK {
			unknown++
			allCorrect = false
			continue
		}
		myAns := strings.TrimSpace(q.MyAnswer)
		correctAns := strings.TrimSpace(q.CorrectAnswer)
		if NormalizeAnswerText(myAns) != NormalizeAnswerText(correctAns) {
			allCorrect = false
			feedback = append(feedback, fmt.Sprintf(
				"- 题目：%s\n  题型：%s\n  你的上次答案：%s\n  正确答案：%s",
				q.Title, q.TypeLabel, orDefault(myAns, "(空)"), orDefault(correctAns, "(空)"),
			))
		}
	}
	return allCorrect, unknown, feedback
}

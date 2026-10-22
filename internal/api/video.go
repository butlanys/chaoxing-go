package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/butlanys/chaoxing-go/internal/captcha"
	"github.com/butlanys/chaoxing-go/internal/consts"
	"github.com/butlanys/chaoxing-go/internal/crypto"
	"github.com/butlanys/chaoxing-go/internal/htmlx"
)

// 本文件对应 Python 原版 api/base.py 的
// study_video / video_progress_log / _refresh_video_status / _recover_after_forbidden。

// OCRConfig 验证码 OCR 后端配置（对应 captcha.New 需要的 [tiku] 配置节）。
// 由 app/CLI 启动时注入（对齐 base.py 里按需初始化 ocr 的行为）。
var OCRConfig map[string]string

// forbiddenRetryBackoff 403 后的等待时长，对齐 Python time.sleep(random.uniform(2, 4))。
// 声明为变量，便于单元测试注入零等待。
var forbiddenRetryBackoff = func() time.Duration {
	return time.Duration(randFloat(2, 4) * float64(time.Second))
}

// captchaRetryInterval 验证码通关失败后的重试间隔（对齐 Python time.sleep(2)）。
var captchaRetryInterval = 2 * time.Second

// reRtFromOtherInfo 对齐 base.py: r"-rt_([1d])"
var reRtFromOtherInfo = regexp.MustCompile(`-rt_([1d])`)

// videoHeadersFor 按任务类型返回对应 Referer（对齐 gc.VIDEO_HEADERS / gc.AUDIO_HEADERS）。
func videoHeadersFor(typ string) map[string]string {
	if typ == "Audio" {
		return consts.AudioHeaders
	}
	return consts.VideoHeaders
}

// videoStatusURL 构造视频状态接口地址（对齐 base.py 的 f-string）。
func (c *Client) videoStatusURL(objectID string) string {
	return fmt.Sprintf("%s/%s?k=%s&flag=normal", urlVideoStatus, objectID, c.FID())
}

// StudyVideo 学习视频/音频任务点（对齐 base.py study_video）。
// typ: "Video" | "Audio"；速度倍率取 c.opt.Speed。
func (c *Client) StudyVideo(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo, typ string) StudyResult {
	headers := videoHeadersFor(typ)

	res, err := c.doText(requestOpts{method: http.MethodGet, rawURL: c.videoStatusURL(job.ObjectID), headers: headers})
	if err != nil {
		c.log.Error("获取视频状态失败: %v", err)
		return ResultError
	}
	var videoInfo map[string]any
	if err := json.Unmarshal([]byte(res.body), &videoInfo); err != nil {
		c.log.Error("解析视频状态失败: %v", err)
		return ResultError
	}
	if anyString(videoInfo["status"]) != "success" {
		c.log.Error("Unknown status: %v", videoInfo["status"])
		return ResultError
	}

	dtoken := anyString(videoInfo["dtoken"])
	_ = anyString(videoInfo["crc"])
	_ = anyString(videoInfo["key"])

	// 真实世界的时间: lastIter, gc.THRESHOLD
	// 视频内的时间(可被倍速放大): duration, playTime, lastLogTime, waitTime
	duration := jsonInt(videoInfo["duration"])
	playTime := float64(job.PlayTime / 1000)
	lastLogTime := 0.0
	lastIter := time.Now()
	waitTime := int(randFloat(30, 90))

	c.log.Info("开始任务: %s, 总时长: %ds, 已进行: %ds", job.Name, duration, int(playTime))

	forbiddenRetry := 0
	const maxForbiddenRetry = 2

	// 先探测是否「瞬间完成」
	passed, state := c.videoProgressLog(course, job, info, dtoken, duration, duration, typ, 4, headers)
	if passed {
		c.log.Info("任务瞬间完成: %s", job.Name)
		return ResultSuccess
	}

	for !passed {
		// 有时最后一次请求需要发送多次才能完成任务
		if playTime-lastLogTime >= float64(waitTime) || playTime == float64(duration) {
			passed, state = c.videoProgressLog(course, job, info, dtoken, duration, int(playTime), typ, 3, headers)

			if state == http.StatusForbidden {
				if forbiddenRetry >= maxForbiddenRetry {
					c.log.Warn("403重试失败, 跳过当前任务")
					return ResultForbidden
				}
				forbiddenRetry++
				c.log.Warn("出现403报错, 正在尝试刷新会话状态 (第%d次)", forbiddenRetry)
				time.Sleep(forbiddenRetryBackoff())

				refreshed, rerr := c.recoverAfterForbidden(job, typ)
				if rerr == nil && refreshed != nil {
					rawDtoken, hasDtoken := refreshed["dtoken"]
					rawDuration, hasDuration := refreshed["duration"]
					if hasDtoken && anyString(rawDtoken) != "" && hasDuration && rawDuration != nil {
						dtoken = anyString(rawDtoken)
						duration = jsonInt(rawDuration)
						if rawPlayTime, ok := refreshed["playTime"]; ok && rawPlayTime != nil {
							playTime = float64(jsonInt(rawPlayTime))
						}
						c.log.Debug("刷新后的令牌: %s, 持续时间: %d, 播放时间: %v", dtoken, duration, playTime)
						continue
					}
				}
				c.log.Error("会话恢复失败，刷新后的元数据缺少必要字段 (dtoken, duration)")
				return ResultError
			} else if !passed && state != http.StatusOK {
				return ResultError
			}

			waitTime = int(randFloat(30, 90))
			lastLogTime = playTime
			c.log.Trace("Progress logged")
		}

		// 上报进度本身耗时，认为视频仍在后台播放，手动补算这段时间
		dt := time.Since(lastIter).Seconds() * c.opt.Speed
		lastIter = time.Now()
		playTime = math.Min(float64(duration), playTime+dt)

		time.Sleep(consts.Threshold)
	}

	c.log.Info("任务完成: %s", job.Name)
	return ResultSuccess
}

// videoProgressLog 上报视频播放进度（对齐 base.py video_progress_log）。
// 返回 (是否已完成, HTTP 状态码)。
func (c *Client) videoProgressLog(course htmlx.Course, job htmlx.Job, info htmlx.JobInfo,
	dtoken string, duration, playingTime int, typ string, isdrag int,
	headers map[string]string) (bool, int) {

	if headers == nil {
		c.log.Warn("null headers")
		headers = consts.VideoHeaders
	}

	c.videoRL.Limit(true, 0, 2)

	if strings.Contains(job.OtherInfo, "courseId") {
		// 原版此处 raise RuntimeError("this is not possible")，Go 版返回非 200 状态由调用方判定失败
		c.log.Error("%s", job.OtherInfo)
		return false, 0
	}

	uid := c.UID()
	enc := crypto.VideoEnc(course.ClazzID, uid, job.JobID, job.ObjectID, playingTime, duration)

	params := url.Values{}
	params.Set("clazzId", course.ClazzID)
	params.Set("playingTime", strconv.Itoa(playingTime))
	params.Set("duration", strconv.Itoa(duration))
	params.Set("clipTime", fmt.Sprintf("0_%d", duration))
	params.Set("objectId", job.ObjectID)
	params.Set("otherInfo", job.OtherInfo)
	params.Set("courseId", course.CourseID)
	params.Set("jobid", job.JobID)
	params.Set("userid", uid)
	params.Set("isdrag", strconv.Itoa(isdrag))
	params.Set("view", "pc")
	params.Set("enc", enc)
	params.Set("dtype", typ)

	if job.VideoFaceCaptureEnc != "" {
		params.Set("videoFaceCaptureEnc", job.VideoFaceCaptureEnc)
	}
	if job.AttDuration != "" {
		params.Set("attDuration", job.AttDuration)
	}
	if job.AttDurationEnc != "" {
		params.Set("attDurationEnc", job.AttDurationEnc)
	}

	logURL := fmt.Sprintf("%s/%s/%s", urlMultimediaLog, course.CPI, dtoken)

	// performRequest 单次上报；遇到 403/验证码时先尝试自动通关验证码再重发
	performRequest := func(rtVal string) (*httpResult, error) {
		params.Set("rt", rtVal)
		params.Set("_t", crypto.Timestamp())

		res, err := c.doText(requestOpts{method: http.MethodGet, rawURL: logURL, params: params, headers: headers})
		if err != nil {
			return nil, err
		}
		if res.statusCode == http.StatusForbidden || strings.Contains(res.body, "验证码") || strings.Contains(res.body, "validate") {
			c.log.Warn("检测到验证码拦截，正在尝试自动通过验证码...")
			if c.tryPassCaptcha(headers) {
				params.Set("_t", crypto.Timestamp())
				return c.doText(requestOpts{method: http.MethodGet, rawURL: logURL, params: params, headers: headers})
			}
			c.log.Error("多次验证码通关失败，可能需要手动干预。")
		}
		return res, nil
	}

	rt := job.Rt
	if rt == "" {
		if m := reRtFromOtherInfo.FindStringSubmatch(job.OtherInfo); m != nil {
			if m[1] == "d" {
				rt = "0.9"
			} else {
				rt = "1"
			}
			c.log.Trace("Got rt from otherinfo: %s", rt)
		}
	}

	var res *httpResult
	if rt != "" {
		c.log.Trace("Got rt: %s", rt)
		job.Rt = rt
		res, err = performRequest(rt)
		if err != nil {
			c.log.Error("视频进度上报失败: %v", err)
			return false, 0
		}
	} else {
		c.log.Warn("Failed to get rt")
		for _, rtVal := range []string{"0.9", "1"} {
			res, err = performRequest(rtVal)
			if err != nil {
				c.log.Error("视频进度上报失败: %v", err)
				return false, 0
			}
			if res.statusCode == http.StatusOK {
				c.log.Trace("%s", res.body)
				return jsonIsPassed(res.body), http.StatusOK
			} else if res.statusCode == http.StatusForbidden {
				c.log.Warn("出现403报错, 正常尝试切换rt")
			} else {
				c.log.Warn("未知错误 jobid=%s, status_code=%d, 摘要:\n%s", job.JobID, res.statusCode, truncate(res.body, 200))
				break
			}
		}
	}

	switch {
	case res.statusCode == http.StatusOK:
		c.log.Trace("%s", res.body)
		return jsonIsPassed(res.body), http.StatusOK
	case res.statusCode == http.StatusForbidden:
		c.log.Debug("视频进度上报返回403, jobid=%s, 摘要=%s", job.JobID, truncate(res.body, 200))
		// 若出现两个 rt 参数都返回 403 的情况, 则跳过当前任务
		c.log.Error("出现403报错, 尝试修复无效, 正在跳过当前任务点...")
		return false, http.StatusForbidden
	}

	c.log.Error("未知错误: %d", res.statusCode)
	return false, res.statusCode
}

// jsonIsPassed 解析进度上报响应中的 isPassed（对齐 Python resp.json()["isPassed"]）。
func jsonIsPassed(body string) bool {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return false
	}
	return jsonBool(payload["isPassed"])
}

// tryPassCaptcha 尝试自动通过超星验证码（对齐 base.py video_progress_log 内的验证码分支）。
// 复用 c.http（带 c.jar），因此验证码会话产生的 cookie 会直接落到当前会话。
func (c *Client) tryPassCaptcha(headers map[string]string) bool {
	ua := consts.Headers["User-Agent"]
	if v, ok := headers["User-Agent"]; ok && v != "" {
		ua = v
	}
	solver := captcha.New(OCRConfig, c.log)
	solverClient := captcha.NewCxCaptcha(c.http, ua, c.CookieString(), solver, c.log)

	for attempt := 1; attempt <= 3; attempt++ {
		c.log.Info("第 %d 次尝试通关验证码...", attempt)
		if solverClient.TryPass() {
			c.log.Success("验证码通关成功！")
			return true
		}
		c.log.Warn("验证码验证失败，正在重试...")
		time.Sleep(captchaRetryInterval)
	}
	return false
}

// refreshVideoStatus 刷新视频状态（对齐 base.py _refresh_video_status）。
func (c *Client) refreshVideoStatus(job htmlx.Job, typ string) (map[string]any, error) {
	c.RateLimitRandom(0, 0.2)
	headers := videoHeadersFor(typ)

	res, err := c.doText(requestOpts{
		method:  http.MethodGet,
		rawURL:  c.videoStatusURL(job.ObjectID),
		headers: headers,
		timeout: 8 * time.Second,
	})
	if err != nil {
		c.log.Debug("刷新视频状态失败: %v", err)
		return nil, err
	}
	if res.statusCode != http.StatusOK {
		c.log.Debug("刷新视频状态返回码异常: %d", res.statusCode)
		c.log.Debug("%s", res.body)
		return nil, fmt.Errorf("刷新视频状态返回码异常: %d", res.statusCode)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(res.body), &data); err != nil {
		c.log.Debug("解析视频状态响应失败: %v", err)
		return nil, err
	}
	if anyString(data["status"]) == "success" {
		return data, nil
	}
	return nil, fmt.Errorf("视频状态异常: %v", data["status"])
}

// recoverAfterForbidden 403 后的会话恢复（对齐 base.py _recover_after_forbidden）。
func (c *Client) recoverAfterForbidden(job htmlx.Job, typ string) (map[string]any, error) {
	// 对齐 SessionManager.update_cookies()：先从 cookie 文件刷新会话
	c.reloadCookiesFromFile()

	if refreshed, err := c.refreshVideoStatus(job, typ); err == nil && refreshed != nil {
		return refreshed, nil
	}
	if c.reloginIfNeeded() {
		return c.refreshVideoStatus(job, typ)
	}
	return nil, fmt.Errorf("会话恢复失败")
}

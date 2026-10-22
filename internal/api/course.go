package api

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/butlanys/chaoxing-go/internal/htmlx"
)

// 本文件对应 Python 原版 api/base.py 的
// get_course_list / get_course_point / get_job_list / study_emptypage。

// courseListForm 构造 courselistdata 接口的表单数据。
func courseListForm(folderID string) url.Values {
	form := url.Values{}
	form.Set("courseType", "1")
	form.Set("courseFolderId", folderID)
	form.Set("query", "")
	form.Set("superstarClass", "0")
	return form
}

// CourseList 读取所有课程列表（对齐 base.py get_course_list）：
// 先取一级课程列表，再取课程文件夹（二级）并逐个追加。
func (c *Client) CourseList() ([]htmlx.Course, error) {
	// 接口突然抽风, 增加 headers；有可能只是 referer 的问题
	headers := map[string]string{
		"Referer": "https://mooc2-ans.chaoxing.com/mooc2-ans/visit/interaction?moocDomain=https://mooc1-1.chaoxing.com/mooc-ans",
	}

	c.log.Trace("正在读取所有的课程列表...")
	resp, err := c.postForm(urlCourseListData, courseListForm("0"), headers, 0)
	if err != nil {
		return nil, fmt.Errorf("读取课程列表失败: %w", err)
	}
	body := readBody(resp)
	_ = resp.Body.Close()
	c.log.Info("课程列表读取完毕...")
	courseList := htmlx.DecodeCourseList(body)

	// 二级文件夹
	interactionResp, err := c.get(urlInteraction, nil, nil)
	if err != nil {
		return courseList, fmt.Errorf("读取课程文件夹失败: %w", err)
	}
	interactionBody := readBody(interactionResp)
	_ = interactionResp.Body.Close()

	for _, folder := range htmlx.DecodeCourseFolder(interactionBody) {
		folderResp, err := c.postForm(urlCourseListData, courseListForm(folder.ID), nil, 0)
		if err != nil {
			return courseList, fmt.Errorf("读取文件夹 %s 课程列表失败: %w", folder.ID, err)
		}
		folderBody := readBody(folderResp)
		_ = folderResp.Body.Close()
		courseList = append(courseList, htmlx.DecodeCourseList(folderBody)...)
	}

	return courseList, nil
}

// CoursePoints 读取课程的所有章节（对齐 base.py get_course_point）。
func (c *Client) CoursePoints(courseID, clazzID, cpi string) (htmlx.PointList, error) {
	u := fmt.Sprintf("%s?courseid=%s&clazzid=%s&cpi=%s&ut=s", urlStudentCourse, courseID, clazzID, cpi)
	c.log.Trace("URL: %s", u)
	c.log.Trace("开始读取课程所有章节...")

	resp, err := c.get(u, nil, nil)
	if err != nil {
		return htmlx.PointList{}, fmt.Errorf("读取课程章节失败: %w", err)
	}
	defer resp.Body.Close()

	body := readBody(resp)
	c.log.Trace("原始章节列表内容:\n%s", body)
	c.log.Info("课程章节读取成功...")
	return htmlx.DecodeCoursePoint(body), nil
}

// mergeJobInfo 对齐 Python 的 job_info.update(_job_info)：
// 原版 _extract_job_info 在 defaults 为空时返回 {}（update 不改变任何字段），
// 而 Go 侧 htmlx.extractJobInfo 此时返回全零结构体，故以「全零即空」近似该语义。
func mergeJobInfo(dst *htmlx.JobInfo, src htmlx.JobInfo) {
	if src == (htmlx.JobInfo{}) {
		return
	}
	*dst = src
}

// JobList 读取章节下的全部任务点（对齐 base.py get_job_list）。
// num 依次取 "0".."6" 共 7 次请求；任一次非 200 返回错误；
// 若某次解析出 notOpen 则立即返回（省一次请求）。
func (c *Client) JobList(course htmlx.Course, point htmlx.Point) ([]htmlx.Job, htmlx.JobInfo, error) {
	c.RateLimit()

	jobList := []htmlx.Job{}
	jobInfo := htmlx.JobInfo{}

	cardsParams := url.Values{}
	cardsParams.Set("clazzid", course.ClazzID)
	cardsParams.Set("courseid", course.CourseID)
	cardsParams.Set("knowledgeid", point.ID)
	cardsParams.Set("ut", "s")
	cardsParams.Set("cpi", course.CPI)
	cardsParams.Set("v", "2025-0424-1038-3")
	cardsParams.Set("mooc2", "1")

	// 学习界面任务卡片数, 很少有3个的, 但是对于章节解锁任务点少一个都不行
	var lastBody string
	for _, possibleNum := range []string{"0", "1", "2", "3", "4", "5", "6"} {
		c.log.Trace("开始读取章节所有任务点...")

		cardsParams.Set("num", possibleNum)
		resp, err := c.get(urlKnowledgeCards, cardsParams, nil)
		if err != nil {
			return nil, htmlx.JobInfo{}, fmt.Errorf("读取章节任务点失败: %w", err)
		}
		body := readBody(resp)
		statusCode := resp.StatusCode
		_ = resp.Body.Close()
		lastBody = body

		if statusCode != http.StatusOK {
			c.log.Error("未知错误: %d 正在跳过", statusCode)
			c.log.Error("%s", body)
			return nil, htmlx.JobInfo{}, fmt.Errorf("读取章节任务点失败: HTTP %d", statusCode)
		}

		jobs, info := htmlx.DecodeCourseCard(body)
		if info.NotOpen {
			// 直接返回, 节省一次请求
			c.log.Info("该章节未开放")
			return nil, info, nil
		}
		jobList = append(jobList, jobs...)
		mergeJobInfo(&jobInfo, info)
	}

	if len(jobList) == 0 {
		_ = c.StudyEmptyPage(course, point)
	}

	c.log.Trace("原始任务点列表内容:\n%s", lastBody)
	c.log.Info("章节任务点读取成功...")
	return jobList, jobInfo, nil
}

// studyAjaxParams 构造 studentstudyAjax 接口参数（study_emptypage / _access_chapter_for_count 共用）。
func studyAjaxParams(course htmlx.Course, point htmlx.Point) url.Values {
	params := url.Values{}
	params.Set("courseId", course.CourseID)
	params.Set("clazzid", course.ClazzID)
	params.Set("chapterId", point.ID)
	params.Set("cpi", course.CPI)
	params.Set("verificationcode", "")
	params.Set("mooc2", "1")
	params.Set("microTopicId", "0")
	params.Set("editorPreview", "0")
	return params
}

// StudyEmptyPage 访问空页面任务（对齐 base.py study_emptypage）。
func (c *Client) StudyEmptyPage(course htmlx.Course, point htmlx.Point) StudyResult {
	resp, err := c.getTimeout(urlStudentStudyAjax, studyAjaxParams(course, point), nil, 8*time.Second)
	if err != nil {
		c.log.Error("空页面任务失败 -> [%v]%s", err, point.Title)
		return ResultError
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.log.Error("空页面任务失败 -> [%d]%s", resp.StatusCode, point.Title)
		return ResultError
	}
	c.log.Info("空页面任务完成 -> %s", point.Title)
	return ResultSuccess
}

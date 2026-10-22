package htmlx

import (
	"strings"
)

// 本文件对应 Python decode.py 的 decode_course_card / _extract_job_info /
// _process_attachment_cards / _process_video_task / _process_document_task /
// _process_work_task / _process_read_task / _process_live_task。

// DecodeCourseCard 解析任务点列表页面，对应 Python 的 decode_course_card。
// 章节未开放时返回 (nil, JobInfo{NotOpen:true})。
func DecodeCourseCard(htmlText string) ([]Job, JobInfo) {
	log().Trace("开始解码任务点列表...")

	// 检查章节是否未开放
	if strings.Contains(htmlText, "章节未开放") {
		return nil, JobInfo{NotOpen: true}
	}

	// 提取 mArg 参数：先把所有空格删掉，再用 mArg={(.*?)}; 取第一个
	temp := reMArg.FindAllStringSubmatch(strings.ReplaceAll(htmlText, " ", ""), -1)
	if len(temp) == 0 {
		return []Job{}, JobInfo{}
	}

	// 解析 JS 对象字面量（超星返回的并非严格 JSON）
	cardsData := parseMArgJSON("{" + temp[0][1] + "}")
	if len(cardsData) == 0 {
		return []Job{}, JobInfo{}
	}

	jobInfo := extractJobInfo(cardsData)

	cards, _ := cardsData["attachments"].([]any)
	jobs := processAttachmentCards(cards)

	return jobs, jobInfo
}

// extractJobInfo 对应 Python 的 _extract_job_info。
func extractJobInfo(cardsData map[string]any) JobInfo {
	defaults := getMap(cardsData, "defaults")
	if len(defaults) == 0 {
		return JobInfo{}
	}

	return JobInfo{
		KToken:             getString(defaults, "ktoken"),
		MtEnc:              getString(defaults, "mtEnc"),
		ReportTimeInterval: getIntDefault(defaults, "reportTimeInterval", 60),
		DefEnc:             getString(defaults, "defenc"),
		CardID:             getString(defaults, "cardid"),
		CPI:                getString(defaults, "cpi"),
		QnEnc:              getString(defaults, "qnenc"),
		KnowledgeID:        getString(defaults, "knowledgeid"),
	}
}

// processAttachmentCards 对应 Python 的 _process_attachment_cards。
func processAttachmentCards(cards []any) []Job {
	jobs := []Job{}

	for _, item := range cards {
		card, ok := item.(map[string]any)
		if !ok || card == nil {
			continue
		}

		// 跳过已通过的任务
		if truthy(card["isPassed"]) {
			continue
		}

		// 处理无 job 字段的特殊任务
		jobVal, hasJob := card["job"]
		if !hasJob || jobVal == nil {
			// 只尝试识别阅读任务
			if rj := processReadTask(card); rj != nil {
				jobs = append(jobs, *rj)
			}
			continue
		}

		// 超星 api 的 otherInfo 会带一串用 & 拼接的参数，只保留第一段
		if _, ok := card["otherInfo"]; ok {
			log().Trace("Fixing other info...")
			card["otherInfo"] = strings.Split(anyString(card["otherInfo"]), "&")[0]
			log().Trace("New info: %s", anyString(card["otherInfo"]))
		}

		// 多维度判断是否为直播任务
		cardType := strings.ToLower(getString(card, "type"))
		property := getMap(card, "property")
		propType := strings.ToLower(getString(property, "type"))
		resourceType := strings.ToLower(getString(property, "resourceType"))

		isLive := strings.Contains(cardType, "live") ||
			strings.Contains(propType, "live") ||
			strings.Contains(resourceType, "live") ||
			strings.Contains(cardType, "livestream") ||
			hasNonNull(property, "liveId") ||
			hasNonNull(property, "streamName") ||
			hasNonNull(property, "vdoid")

		switch {
		case isLive:
			if lj := processLiveTask(card); lj != nil {
				jobs = append(jobs, *lj)
			}
		case cardType == "video":
			if vj := processVideoTask(card); vj != nil {
				jobs = append(jobs, *vj)
			}
		case cardType == "document":
			jobs = append(jobs, processDocumentTask(card))
		case cardType == "workid":
			jobs = append(jobs, processWorkTask(card))
		default:
			log().Warn("Unknown card type: %s", cardType)
			log().Warn("%v", card)
		}
	}

	return jobs
}

// hasNonNull 等价 Python 的 m.get(key) is not None（键存在且值非 null）
func hasNonNull(m map[string]any, key string) bool {
	v, ok := m[key]
	return ok && v != nil
}

// processVideoTask 对应 Python 的 _process_video_task：mid 为必须字段，缺失则跳过该任务。
func processVideoTask(card map[string]any) *Job {
	midVal, ok := card["mid"]
	if !ok {
		// Python 里 card["mid"] 抛 KeyError -> 返回 None
		log().Warn("出现转码失败视频，已跳过...")
		return nil
	}

	property := getMap(card, "property")
	return &Job{
		Type:                "video",
		JobID:               getString(card, "jobid"),
		Name:                getString(property, "name"),
		OtherInfo:           getString(card, "otherInfo"),
		Mid:                 anyString(midVal),
		ObjectID:            getString(card, "objectId"),
		Aid:                 getString(card, "aid"),
		PlayTime:            getIntDefault(card, "playTime", 0),
		Rt:                  getString(property, "rt"),
		AttDuration:         getString(card, "attDuration"),
		AttDurationEnc:      getString(card, "attDurationEnc"),
		VideoFaceCaptureEnc: getString(card, "videoFaceCaptureEnc"),
	}
}

// processDocumentTask 对应 Python 的 _process_document_task。
func processDocumentTask(card map[string]any) Job {
	property := getMap(card, "property")
	return Job{
		Type:      "document",
		JobID:     getString(card, "jobid"),
		OtherInfo: getString(card, "otherInfo"),
		JToken:    getString(card, "jtoken"),
		Mid:       getString(card, "mid"),
		Enc:       getString(card, "enc"),
		Aid:       getString(card, "aid"),
		ObjectID:  getString(property, "objectid"),
	}
}

// processWorkTask 对应 Python 的 _process_work_task。
func processWorkTask(card map[string]any) Job {
	return Job{
		Type:      "workid",
		JobID:     getString(card, "jobid"),
		OtherInfo: getString(card, "otherInfo"),
		Mid:       getString(card, "mid"),
		Enc:       getString(card, "enc"),
		Aid:       getString(card, "aid"),
	}
}

// processReadTask 对应 Python 的 _process_read_task：仅 type=="read" 且 property.read 不为真时有效。
func processReadTask(card map[string]any) *Job {
	if t, ok := card["type"].(string); !ok || t != "read" {
		return nil
	}
	property := getMap(card, "property")
	if truthy(property["read"]) {
		return nil
	}

	return &Job{
		Type:      "read",
		JobID:     getString(card, "jobid"),
		Title:     getString(property, "title"),
		JToken:    getString(card, "jtoken"),
		Mid:       getString(card, "mid"),
		OtherInfo: getString(card, "otherInfo"),
		Enc:       getString(card, "enc"),
		Aid:       getString(card, "aid"),
		// Python 字典里的 "id"（阅读资源 id）在 Go 结构里落到 ObjectID
		ObjectID: getString(property, "id"),
	}
}

// processLiveTask 对应 Python 的 _process_live_task：保留完整 property 供后续使用。
func processLiveTask(card map[string]any) *Job {
	property := getMap(card, "property")

	// Python: card.get("jobid", str(card.get("id", "")))
	jobID := ""
	if v, ok := card["jobid"]; ok {
		jobID = anyString(v)
	} else {
		jobID = anyString(card["id"])
	}

	// Python: property.get("title", property.get("name", "未知直播"))
	name := "未知直播"
	if v, ok := property["title"]; ok {
		name = anyString(v)
	} else if v, ok := property["name"]; ok {
		name = anyString(v)
	}

	return &Job{
		Type:      "live",
		JobID:     jobID,
		Name:      name,
		OtherInfo: getString(card, "otherInfo"),
		Property:  property, // liveId / streamName / vdoid 等直播特有字段都在这里面
		Mid:       getString(card, "mid"),
		ObjectID:  getString(card, "objectId"),
		Aid:       getString(card, "aid"),
	}
}

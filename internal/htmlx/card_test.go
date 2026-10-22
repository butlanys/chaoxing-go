package htmlx

import "testing"

// 任务点页面片段：mArg 为超星返回的 JS 对象字面量（标准 JSON，含多余空格用于验证空格剔除）
const courseCardHTML = `<html><body>
<div id="cardContent"></div>
<script type="text/javascript">
var mArg={"defaults": {"ktoken":"KT","mtEnc":"MT","reportTimeInterval": 30,"defenc":"DEF","cardid":"CARD1","cpi":"999","qnenc":"QN","knowledgeid":"KID1"},"attachments": [{"type":"video","job":{"name":"视频"},"jobid":"job_v1","mid":"mid_1","objectId":"obj_1","aid":"100","playTime":1500,"attDuration":600,"attDurationEnc":"ENC1","videoFaceCaptureEnc":"FACE1","otherInfo":"courseId=1&clazzid=2&extra=3","property":{"name":"视频1","rt":"0.9"}},{"type":"video","job":{"name":"视频"},"jobid":"job_v2","objectId":"obj_2","property":{"name":"转码失败视频"}},{"type":"document","job":{"name":"文档"},"jobid":"job_d1","jtoken":"JT","mid":"mid_d","enc":"ENC_D","aid":"101","otherInfo":"courseId=1","property":{"objectid":"objdoc"}},{"type":"workid","job":{"name":"作业"},"jobid":"job_w1","mid":"mid_w","enc":"ENC_W","aid":"102","otherInfo":"courseId=1"},{"type":"read","jobid":"job_r1","jtoken":"JT_R","mid":"mid_r","enc":"ENC_R","aid":"103","otherInfo":"courseId=1","property":{"id":"read_obj","title":"阅读材料"}},{"type":"read","jobid":"job_r2","property":{"id":"read_obj2","title":"已读材料","read":true}},{"type":"live","job":{"name":"直播"},"jobid":"job_l1","mid":"mid_l","objectId":"obj_l","aid":"104","otherInfo":"courseId=1","property":{"type":"live","liveId":"LIVE1","streamName":"STREAM1","title":"直播课堂"}},{"type":"video","job":{"name":"直播"},"jobid":"job_l2","mid":"mid_l2","property":{"liveId":"LIVE2","name":"直播2"}},{"type":"video","job":{"name":"视频"},"jobid":"job_pass","mid":"mid_p","isPassed":true,"property":{"name":"已通过的视频"}},{"type":"ppt","job":{"name":"课件"},"jobid":"job_x","mid":"mid_x","property":{}}]};
</script>
</body></html>`

func TestDecodeCourseCard(t *testing.T) {
	jobs, info := DecodeCourseCard(courseCardHTML)

	// JobInfo
	wantInfo := JobInfo{
		KToken: "KT", MtEnc: "MT", ReportTimeInterval: 30, DefEnc: "DEF",
		CardID: "CARD1", CPI: "999", QnEnc: "QN", KnowledgeID: "KID1",
	}
	if info != wantInfo {
		t.Errorf("JobInfo = %+v, 期望 %+v", info, wantInfo)
	}

	want := []Job{
		{
			Type: "video", JobID: "job_v1", Name: "视频1", OtherInfo: "courseId=1",
			Mid: "mid_1", ObjectID: "obj_1", Aid: "100", PlayTime: 1500, Rt: "0.9",
			AttDuration: "600", AttDurationEnc: "ENC1", VideoFaceCaptureEnc: "FACE1",
		},
		{
			Type: "document", JobID: "job_d1", OtherInfo: "courseId=1", JToken: "JT",
			Mid: "mid_d", Enc: "ENC_D", Aid: "101", ObjectID: "objdoc",
		},
		{
			Type: "workid", JobID: "job_w1", OtherInfo: "courseId=1",
			Mid: "mid_w", Enc: "ENC_W", Aid: "102",
		},
		{
			Type: "read", JobID: "job_r1", Title: "阅读材料", OtherInfo: "courseId=1",
			JToken: "JT_R", Mid: "mid_r", Enc: "ENC_R", Aid: "103", ObjectID: "read_obj",
		},
		{
			// property.type 含 live -> 直播
			Type: "live", JobID: "job_l1", Name: "直播课堂", OtherInfo: "courseId=1",
			Mid: "mid_l", ObjectID: "obj_l", Aid: "104",
			Property: map[string]any{"type": "live", "liveId": "LIVE1", "streamName": "STREAM1", "title": "直播课堂"},
		},
		{
			// card.type 是 video，但 property.liveId 非空 -> 仍按直播处理
			Type: "live", JobID: "job_l2", Name: "直播2",
			Mid:      "mid_l2",
			Property: map[string]any{"liveId": "LIVE2", "name": "直播2"},
		},
	}

	if len(jobs) != len(want) {
		t.Fatalf("任务数量 = %d, 期望 %d\n实际: %+v", len(jobs), len(want), jobs)
	}
	for i := range want {
		if jobs[i].Type != want[i].Type || jobs[i].JobID != want[i].JobID {
			t.Errorf("第 %d 个任务 = %s/%s, 期望 %s/%s", i, jobs[i].Type, jobs[i].JobID, want[i].Type, want[i].JobID)
		}
		if jobs[i].PlayTime != want[i].PlayTime || jobs[i].Title != want[i].Title ||
			jobs[i].Name != want[i].Name || jobs[i].Rt != want[i].Rt {
			t.Errorf("第 %d 个任务字段不符 = %+v, 期望 %+v", i, jobs[i], want[i])
		}
	}

	// 直播任务保留完整 property（liveId/streamName）
	if jobs[4].Property["liveId"] != "LIVE1" || jobs[4].Property["streamName"] != "STREAM1" {
		t.Errorf("直播任务未保留 property: %+v", jobs[4].Property)
	}
}

func TestDecodeCourseCardBranches(t *testing.T) {
	t.Run("章节未开放", func(t *testing.T) {
		jobs, info := DecodeCourseCard(`<html><body><div>章节未开放</div></body></html>`)
		if jobs != nil {
			t.Errorf("未开放章节任务列表应为 nil, 实际 %+v", jobs)
		}
		if !info.NotOpen {
			t.Errorf("NotOpen 应为 true, 实际 %+v", info)
		}
	})

	t.Run("无 mArg", func(t *testing.T) {
		jobs, info := DecodeCourseCard(`<html><body>没有任务数据</body></html>`)
		if len(jobs) != 0 {
			t.Errorf("任务列表应为空, 实际 %+v", jobs)
		}
		if info != (JobInfo{}) {
			t.Errorf("JobInfo 应为零值, 实际 %+v", info)
		}
	})

	t.Run("mArg 为空对象", func(t *testing.T) {
		jobs, info := DecodeCourseCard(`<script>var mArg={};</script>`)
		if len(jobs) != 0 || info != (JobInfo{}) {
			t.Errorf("空 mArg 应返回空结果, 实际 %+v / %+v", jobs, info)
		}
	})

	t.Run("无 defaults 时 reportTimeInterval 默认 60", func(t *testing.T) {
		html := `<script>var mArg={"attachments":[{"type":"video","job":{"name":"视频"},"jobid":"j1","mid":"m1"}]};</script>`
		jobs, info := DecodeCourseCard(html)
		if info != (JobInfo{}) {
			t.Errorf("无 defaults 时 JobInfo 应为零值, 实际 %+v", info)
		}
		if len(jobs) != 1 || jobs[0].JobID != "j1" || jobs[0].Type != "video" {
			t.Fatalf("任务解析错误: %+v", jobs)
		}

		html2 := `<script>var mArg={"defaults":{"ktoken":"K2"},"attachments":[]};</script>`
		_, info2 := DecodeCourseCard(html2)
		if info2.ReportTimeInterval != 60 || info2.KToken != "K2" {
			t.Errorf("reportTimeInterval 默认值错误: %+v", info2)
		}
	})

	t.Run("JS 对象字面量容错(无引号 key/单引号)", func(t *testing.T) {
		html := `<script>var mArg={defaults:{ktoken:'KT2',reportTimeInterval:45},attachments:[{type:'video',job:{name:'直播'},jobid:'job_js',mid:'mid_js',property:{name:'JS视频'}}]};</script>`
		jobs, info := DecodeCourseCard(html)
		if info.KToken != "KT2" || info.ReportTimeInterval != 45 {
			t.Fatalf("非严格 JSON 的 JobInfo 解析错误: %+v", info)
		}
		if len(jobs) != 1 || jobs[0].JobID != "job_js" || jobs[0].Name != "JS视频" || jobs[0].Mid != "mid_js" {
			t.Fatalf("非严格 JSON 的任务解析错误: %+v", jobs)
		}
	})
}

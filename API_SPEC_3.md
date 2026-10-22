## work.go —— 章节检测（最复杂，必须完整移植）

`StudyWork(course, job, info) StudyResult`：
1. `c.tiku == nil || c.tiku.Disabled()` → 直接 ResultSuccess（对齐 Python `tiku.DISABLE`）。
2. `maxRetries`：`opt.WorkRedoEnabled` 为 true 时取 `max(1, opt.WorkMaxRetries)`，否则 0。
3. `fetch()`：GET `https://mooc1.chaoxing.com/mooc-ans/api/work`，params：`api=1`、`workId=job.JobID 去掉 "work-" 前缀`、`jobid=job.JobID`、`originJobId=job.JobID`、`needRedirect=true`、`skipHeader=true`、`knowledgeid=info.KnowledgeID`、`ktoken=info.KToken`、`cpi=info.CPI`、`ut=s`、`clazzId=course.ClazzID`、`type=`、`enc=job.Enc`、`mooc2=1`、`courseid=course.CourseID`。
   - 响应含 `教师未创建完成该测验` → 视为「跳过」，StudyWork 返回 ResultSuccess 并 Warn。
   - 否则 `htmlx.DecodeQuestions(html, fontDecoder)`，其中 fontDecoder 由 `font.NewDecoder(html, table)` 构造（`table` 通过 `font.LoadTable("resource/font_map_table.json")` 懒加载一次，包级 sync.Once 缓存；失败则为 nil）。
   - `resp.StatusCode==200 && len(questions.Questions)>0` 才算有效；否则重试，最多 3 次、每次间隔 1s；耗尽后返回 error。
4. 对 `attempt := 0..maxRetries`：
   - attempt>0 时先 `time.Sleep(2s)`，并调用 `c.tiku.SetWorkFeedback(feedbackHistory)`。
   - `answers := c.tiku.QueryAll(qs, c.opt.QueryDelay)`（长度不对时补齐/截断到题目数）。
   - 逐题决定答案（**严格对齐 base.py 1105-1167 行**）：
     - 空答案 → `RandomAnswer(q.Options, q.Type)`，AnswerSource="random"。
     - multiple：`MultiCut(q.Options)` 与 `MultiCut(res)`；对 `CleanRes(resList)` 每个元素，遍历 options 找 `IsSubsequence(a, o)` 命中的取首字母；未命中用 `BestOptionBySimilarity(a, options, 0.8)`；最后 `sort + 去重` 拼成字符串。
     - single：`MultiCut(q.Options)` 后对 `CleanRes(res)[0]` 找 `IsSubsequence` 命中项取首字母；否则 `BestOptionBySimilarity(..., 0.8)`。
     - judgement：`c.tiku.JudgementSelect(res)` → "true"/"false"。
     - completion：list 则 join，string 则原样。
     - 其他（shortanswer/unknown）：直接用 res。
     - 结果为空 → RandomAnswer + "random"；否则 "cover" 且 found++。
   - `coverRate = found/total*100` 并 Info 输出。
   - pyFlag 判定：`SubmitMode()=="1"` → pyFlag="1"；否则若（题库 IsManual）或 `coverRate >= c.tiku.CoverRate()*100` 或 `c.rollback >= 1` → pyFlag=""（提交）；否则 pyFlag="1"（只保存）并 Info 输出覆盖率不足。
   - 组装提交表单：复制 `questions.Fields`；当 pyFlag=="1" 时 `answer{id}` 只填 AnswerSource=="cover" 的答案，否则填全部；同时写入 `answertype{id}`；写入 `answerwqbid`。**提交前删除 questions 字段（不要提交解析结构）**。
   - POST `https://mooc1.chaoxing.com/mooc-ans/work/addStudentWorkNew`，form 编码，headers 用 base.py 1210-1224 行那一整组（Host/sec-ch-ua*/X-Requested-With/UA/Accept/Content-Type/Origin/Sec-Fetch-*/Accept-Language）。非 200 → Error + ResultError；JSON `status` 为 false → Error + ResultError。
   - pyFlag=="1" → 直接 ResultSuccess（只保存无法判分）。
   - 调 `checkWorkResult(...)`：返回 nil → ResultSuccess；`all_correct` → Success；`score >= 99.95` → Success（unknown>0 时 Warn）；`unknown>0` → Warn + Success；否则若 `attempt >= maxRetries` → Warn + Success（保留成绩）；否则记录 feedback、`c.rollback++`、继续下一轮。
5. 循环耗尽 → Error + ResultError。

`checkWorkResult(course, job, info, form, workID) (map[string]any, error)`：
- `workID` 取 `form["workId"]`，空则 `form["workRelationId"]`，空则 `job.JobID` 去 `work-` 前缀。`workAnswerID=form["workAnswerId"]`。`cpi` 取 course.CPI 或 form["cpi"]。
- 先 GET `https://mooc1.chaoxing.com/mooc-ans/work/record-list`（params：courseId、classId、workId、workAnswerId、cpi、api=1、mooc2=1、ut=s，超时 20s），最多 5 次、间隔 1.5s，直到 `htmlx.DecodeWorkRecordList` 非空。
- 记录为空时走兜底：再 GET `/mooc-ans/api/work`（同 fetch 的参数），若 HTML 含 `answerwqbid` → 返回 `{all_correct:false, feedback:[], score:0, times:0}`；若同时含 `正确答案` 与 `我的答案` → 用 `DecodeWorkRecordDetail` 逐题比对（`NormalizeAnswerText`），并正则 `本次成绩<i>([\d.]+)</i>分` 取分；都不满足返回 nil。
- 有记录时：`latestTimes = max(times)`、`latestScore`；GET `https://mooc1.chaoxing.com/mooc-ans/work/record-detail`（params：courseId、classId、workId、workAnswerId、times、cpi、ut=s、isdisplaytable=0、firstHeader=2、isWork=false、workSystem=0、api=1、archive=false、mooc2=1，超时 20s）→ `DecodeWorkRecordDetail`。
- 逐题：`ParseOK==false` → `unknown++` 且 `all_correct=false`；否则 `NormalizeAnswerText(my) != NormalizeAnswerText(correct)` → `all_correct=false` 且 feedback 追加四行文本（题目/题型/你的上次答案/正确答案，格式与 base.py 1452-1457 行一致）。
- 返回 `{all_correct, feedback []string, score, times, unknown}`。

## misc.go

- `StudyDocument(course, job)`：GET `https://mooc1.chaoxing.com/ananas/job/document?jobid={job.JobID}&knowledgeid={正则 nodeId_(.*?)- 从 job.OtherInfo 提取}&courseid=&clazzid=&jtoken=&_dc={Timestamp()}`；非 200 → ResultError。
- `StudyRead(course, job, info)`：GET `https://mooc1.chaoxing.com/ananas/job/readv2`，params `jobid`/`knowledgeid=info.KnowledgeID`/`jtoken`/`courseid`/`clazzid`；非 200 → Error+ResultError；否则 Info 输出 JSON 的 `msg`。
- `IncreaseChapterLearningCount(course, points, target, onProgress)`：循环 `total<target`，逐章节：
  - `RateLimitRandom(0,0.2)`；GET `studentstudyAjax`（同 StudyEmptyPage 参数，超时 8s）；失败则 `consecutiveFailures++`，连续 10 次 → Error + ResultError。
  - 成功则清零连续失败计数；从 HTML 用正则 `<script[^>]+src="(https://fystat-ans\.chaoxing\.com/log/setlog[^"]+)"` 提取 setlog URL 并 GET（超时 5s，失败仅 Trace）。
  - 发 monitor 心跳：GET `https://detect.chaoxing.com/api/monitor`，params：`version=Timestamp()`、`refer=http://i.mooc.chaoxing.com`、`from=`、`fid=FID()`、`jsoncallback=jsonp<随机21位数字>`、`t=Timestamp()`；Header `Referer: https://mooc1.chaoxing.com/mycourse/studentstudy?chapterId={point.ID}&courseId={course.CourseID}&clazzid={course.ClazzID}&cpi={course.CPI}&mooc2=1`；超时 5s。
  - `time.Sleep(30s)` 后再发一次心跳；`total++`；`onProgress(total, target)`；Info 进度。
- `ActivityList(course)`：GET `https://mobilelearn.chaoxing.com/v2/apis/active/student/activelist`，params `fid`/`courseId`/`classId=course.ClazzID`/`showNotStartedActive=0`/`_=Timestamp()`，`allow_redirects=false` 等价（CheckRedirect 返回 ErrUseLastResponse 或直接不跟随）；`result != 1` → 返回空列表 + error。
- `SignIn(course, activityID)`：先 GET `https://mobilelearn.chaoxing.com/newsign/preSign`（params：general=1、sys=1、ls=1、appType=15、tid=、ut=s、uid、activePrimaryId、courseId、classId），再 GET `https://mobilelearn.chaoxing.com/pptSign/stuSignajax`（params：activeId、uid、fid、courseId、classId、clientip=、objectId=aaa、name=、useragent=、latitude=-1、longitude=-1、appType=15），返回响应文本。
- `LiveStatus(course, job, knowledgeID)`：GET `https://mooc1.chaoxing.com/ananas/live/liveinfo?liveid={property.liveId}&userid={UID()}&clazzid={course.ClazzID}&knowledgeid={knowledgeID}&courseid={course.CourseID}&jobid={property._jobid}&ut=s`，header 用 consts.LiveHeaders，返回解析后的 map。
- `RunLive(course, job, knowledgeID, speed)`：取 `property.streamName`/`property.vdoid`，任一为空 → Error + ResultError。调 `LiveStatus` 拿 `temp.data.duration`（缺失按 30*60 处理）；`totalMinutes = ceil(duration/speed/60)`；循环 totalMinutes 次：GET `https://zhibo.chaoxing.com/saveTimePc?streamName=&vdoid=&userId=&isStart=0&t={毫秒}&courseId=`（header consts.LiveHeaders，超时 10s），响应文本 trim 后等于 `@success` 即成功，失败则 sleep 5s 重试一次；每次后 `time.Sleep(59/speed 秒)`，Info 输出「已观看 i/N 分钟」。

## helpers.go

逐字移植 base.py 的：`multi_cut`(调用 Cut)、`clean_res`（长度>1 时去掉开头字母编号与标点）、`normalize_text`（异体字映射 ⻛⻔⻋⻢ + 去字母编号 + 去空白 + 去标点 + 小写）、`get_option_text`、`best_option_by_similarity`（用 `SequenceMatcher` 等价的相似度——**用 difflib 的 Ratios 算法自己实现，不要用 Levenshtein**；简单做法：计算 2*M/(len(a)+len(b))，M 为最长匹配块总长，可用递归或动态规划实现 SequenceMatcher.ratio 的近似）、`is_subsequence`（大小写不敏感的子序列判断）、`random_answer`（权重表与 base.py 完全一致）、`normalize_answer_text`（真/假同义词集合 `_JUDGE_TRUE_SET`/`_JUDGE_FALSE_SET` 必须逐字照搬）。

## 测试要求（必须真跑）

- `session_test.go`：httptest 断言 fanyalogin 的 form 字段名与 AES 密文（用 crypto.Encrypt 复算比对）、cookie 落盘格式、ValidateCookieSession 的三条分支。
- `course_test.go`：断言 knowledge/cards 的 num 从 0 到 6 共 7 次请求、NotOpen 时提前返回、job 合并结果。
- `video_test.go`：断言 status 请求、enc 参数与 crypto.VideoEnc 一致、rt 从 otherInfo 推导、403 分支返回 ResultForbidden。
- `work_test.go`：用 httptest 构造题目页（含 5 种题型）→ 断言提交表单字段（answer{id}/answertype{id}/answerwqbid/pyFlag 分支）、覆盖率不足时 pyFlag=1、满分兜底、feedback 生成。
- `misc_test.go`：document/read/learningCount/live 的 URL 与参数断言。
- `helpers_test.go`：NormalizeText/RandomAnswer（固定随机种子后断言取值分布合法）/IsSubsequence/BestOptionBySimilarity 的表驱动用例。

最终必须实际执行并粘贴输出：
`cd /root/chaoxing-go && go build ./internal/api/... && go vet ./internal/api/... && go test -race -count=1 ./internal/api/...`

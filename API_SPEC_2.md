## course.go

- `CourseList() ([]htmlx.Course, error)`
  - POST `https://mooc2-ans.chaoxing.com/mooc2-ans/visit/courselistdata`，data 同上，额外 header `Referer: https://mooc2-ans.chaoxing.com/mooc2-ans/visit/interaction?moocDomain=https://mooc1-1.chaoxing.com/mooc-ans`；`htmlx.DecodeCourseList(resp.Body)`。
  - 再 GET `https://mooc2-ans.chaoxing.com/mooc2-ans/visit/interaction` → `htmlx.DecodeCourseFolder`；对每个 folder 再 POST courselistdata（`courseFolderId=folder.ID`）并追加解析结果。
- `CoursePoints(courseID, clazzID, cpi string) (htmlx.PointList, error)`
  - GET `https://mooc2-ans.chaoxing.com/mooc2-ans/mycourse/studentcourse?courseid={courseID}&clazzid={clazzID}&cpi={cpi}&ut=s` → `htmlx.DecodeCoursePoint`。
- `JobList(course htmlx.Course, point htmlx.Point) ([]htmlx.Job, htmlx.JobInfo, error)`
  - `c.RateLimit()`；GET `https://mooc1.chaoxing.com/mooc-ans/knowledge/cards`，params：`clazzid`、`courseid`、`knowledgeid=point.ID`、`ut=s`、`cpi=course.CPI`、`v=2025-0424-1038-3`、`mooc2=1`、`num`。
  - **num 依次取 "0".."6" 共 7 次请求**，累加 job 列表、合并 jobInfo；任一次非 200 → 返回 `nil, JobInfo{}, error`。若某次 `jobInfo.NotOpen` 为 true → **立即返回** `nil, jobInfo, nil`（省一次请求）。
  - 全部 num 跑完后若 job 列表为空 → 调用 `StudyEmptyPage(course, point)`。
- `StudyEmptyPage(course, point) StudyResult`
  - GET `https://mooc1.chaoxing.com/mooc-ans/mycourse/studentstudyAjax`，params：`courseId`、`clazzid`、`chapterId=point.ID`、`cpi`、`verificationcode=`、`mooc2=1`、`microTopicId=0`、`editorPreview=0`，超时 8s；非 200 → ResultError，否则 ResultSuccess。

## video.go

- `StudyVideo(course, job, info, typ string) StudyResult`（typ: "Video"|"Audio"，headers 用 consts.VideoHeaders / consts.AudioHeaders）
  1. GET `https://mooc1.chaoxing.com/ananas/status/{job.ObjectID}?k={FID()}&flag=normal`；JSON 的 `status != "success"` → `Error("Unknown status: %v")` 并返回 ResultError。取 `dtoken`、`crc`、`key`、`duration`。
  2. `playTime = job.PlayTime / 1000`（毫秒转秒）；`lastLogTime = 0`；`lastIter = time.Now()`；`waitTime = random(30,90)`。
  3. 先调用 `videoProgressLog(..., playingTime=duration, duration=duration, isdrag=4)` 探测是否「瞬间完成」，`isPassed` 为 true → 直接 ResultSuccess。
  4. 循环直到 passed：
     - 若 `playTime-lastLogTime >= waitTime || playTime == duration` → 调用 `videoProgressLog(..., playingTime=int(playTime), isdrag=3)`。
       - state==403：`forbiddenRetry >= 2` → 返回 ResultForbidden；否则 forbiddenRetry++、`time.Sleep(random(2,4)s)`、`recoverAfterForbidden`；恢复成功则更新 dtoken/duration/playTime 并 continue，失败 → ResultError。
       - `!passed && state != 200` → ResultError。
       - 重置 waitTime=random(30,90)、lastLogTime=playTime。
     - `dt = time.Since(lastIter).Seconds() * speed`；`lastIter = now`；`playTime = min(duration, playTime+dt)`。
     - `time.Sleep(consts.Threshold)`（1s）。
  5. 结束 `Info("任务完成: %s", job.Name)` → ResultSuccess。
- `videoProgressLog(course, job, info, dtoken, duration, playingTime, typ, isdrag, headers) (passed bool, state int)`
  - 先 `c.videoRL.Limit(true, 0, 2)`。
  - `enc = crypto.VideoEnc(course.ClazzID, UID(), job.JobID, job.ObjectID, playingTime, duration)`。
  - params：`clazzId`、`playingTime`、`duration`、`clipTime=f"0_{duration}"`、`objectId`、`otherInfo=job.OtherInfo`、`courseId`、`jobid`、`userid=UID()`、`isdrag`、`view=pc`、`enc`、`dtype=typ`；非空时追加 `videoFaceCaptureEnc`、`attDuration`、`attDurationEnc`。
  - URL：`https://mooc1.chaoxing.com/mooc-ans/multimedia/log/a/{course.CPI}/{dtoken}`。
  - `rt` 解析：优先 `job.Rt`；为空时用正则 `-rt_([1d])` 匹配 `job.OtherInfo`，命中 `d` → `"0.9"`，`1` → `"1"`。
    - 有 rt：单次请求（params 加 `rt`、`_t=crypto.Timestamp()`）。
    - 无 rt：依次试 rt=0.9、1；200 直接返回 `json.isPassed, 200`；403 继续换 rt；其他状态码 break。
  - 403 或响应体含 `验证码`/`validate` → 走验证码流程：用 `internal/captcha` 的 `NewCxCaptcha(c.http, UA, cookieString, solver, c.log)`，最多 3 次 `TryPass()`，成功则把 captcha 会话 cookie 合并回 c.jar 并重发请求。`captcha.New(conf, log)` 需要一个 `map[string]string` 配置——用 `c.opt.OCRConfig`（见下）或空 map。**为此请在 `types.go` 之外新增字段不可行**，改用包级变量：在 `video.go` 里定义 `var OCRConfig map[string]string`，由 app/CLI 启动时赋值。
  - 403 且重试无效 → `Error("出现403报错, 尝试修复无效, 正在跳过当前任务点...")` 返回 `false, 403`。
- `refreshVideoStatus(job, typ) (map[string]any, error)`：GET `https://mooc1.chaoxing.com/ananas/status/{job.ObjectID}?k={FID()}&flag=normal`（header 同 typ），`status=="success"` 返回 map。
- `recoverAfterForbidden(job, typ) (map[string]any, error)`：先 `refreshVideoStatus`，失败则重新 `Login(false)` 后再试一次。

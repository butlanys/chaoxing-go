# internal/api 实现说明（给实现 agent）

目标：把 `/root/chaoxing-ref/api/base.py`(1661 行)、`api/captcha.py`、`api/live.py`、`api/live_process.py` 完整移植成 Go。
契约见 `/root/chaoxing-go/CONTRACT.md`；`internal/api/types.go` 与 `internal/api/http.go` **已写好，不要修改**（types.go 里的 Client 结构体/Options/限速器/StudyResult 都在，http.go 里有 flatJar、newHTTPClient、randFloat、CookieString、LoadCookies、RateLimit、RateLimitRandom）。

## 你要新增的文件（不要动 types.go / http.go）

- `session.go`  — 请求底座：doGET/doPOST、cookie 持久化、uid/fid、Name()、ValidateCookieSession()、Login()
- `course.go`   — CourseList()、CoursePoints()、JobList()、StudyEmptyPage()
- `video.go`    — StudyVideo()、videoProgressLog()、refreshVideoStatus()、recoverAfterForbidden()
- `work.go`     — StudyWork()、checkWorkResult()
- `misc.go`     — StudyDocument()、StudyRead()、IncreaseChapterLearningCount()、ActivityList()、SignIn()、RunLive()、LiveStatus()
- `helpers.go`  — MultiCut/CleanRes/IsSubsequence/BestOptionBySimilarity/RandomAnswer/NormalizeText/NormalizeAnswerText 及内部小工具
- 每个文件配 `_test.go`（httptest.Server 断言请求构造与响应解析）

## 通用规则

- 所有 URL / method / 参数名 / 参数值 / Header 必须与 base.py **逐字一致**。
- 通用 Header 用 `consts.Headers`（**不要**在 Client 上覆盖它，每次请求显式传）。
- 每处需要等待的地方用 Client 自带限速器：普通接口 `c.RateLimit()`，随机等待 `c.RateLimitRandom(0, 0.2)`；视频进度上报前用 `c.videoRL.Limit(true, 0, 2)`。
- 超时统一走 `c.http`（已带 timeout）。
- 所有方法不得 panic，网络/解析错误返回 error 或 StudyResult。
- 日志用 `c.log`（已带账号标签），中文文案与 base.py 一致。

## session.go

- `get(url string, params url.Values, headers map[string]string) (*http.Response, error)`：GET，自动带上 consts.Headers + 传入 headers。
- `post(url string, form url.Values, headers map[string]string) (*http.Response, error)`：POST form-urlencoded。
- `getJSON` / `postJSON` 辅助：返回 http.Response 与解析后的 `map[string]any`。
- `UID()`：读 cookie `_uid`，其次 `UID`；都没有返回 "" 并 `Error("Cannot get uid !")`。结果缓存到 `c.uid`。
- `FID()`：读 cookie `fid`，缺失返回 "1024"（对齐 Python 默认值 1024）。缓存到 `c.fid`。
- `Name()`：GET `https://passport2.chaoxing.com/mooc/accountManage`，正则 `id="messageName"\s+value="([^"]*)"` 取真实姓名；失败返回 ""。
- `ValidateCookieSession()`：先看 cookie 里有没有 `_uid`；没有返回 false。然后 POST `https://mooc2-ans.chaoxing.com/mooc2-ans/visit/courselistdata`，data=`courseType=1&courseFolderId=0&query=&superstarClass=0`，超时 8s；非 200 返回 false；响应体含 `passport2.chaoxing.com` 或（小写后）含 `login` 返回 false；否则 true。
- `Login(useCookies bool) (LoginResult, error)`：
  - useCookies=true：若 `c.opt.CookieFile` 非空则从文件读 cookie 并 `LoadCookies`；校验 `ValidateCookieSession()`；失败时若账号密码非空则退回密码登录，否则返回 `{Status:false, Msg:"cookies 已失效，请更新 cookies 或提供账号密码"}`；成功则 `Name()` 填 RealName，并把 cookie 写回 CookieFile。
  - 密码登录：POST `https://passport2.chaoxing.com/fanyalogin`，data 字段：`fid=-1`、`uname=crypto.Encrypt(username)`、`password=crypto.Encrypt(password)`、`refer=https%3A%2F%2Fi.chaoxing.com`、`t=true`、`forbidotherlogin=0`、`validate=`、`doubleFactorLogin=0`、`independentId=0`。响应 JSON `status==true` 即成功（msg2 为失败原因）。成功后把 cookie 落盘到 CookieFile（`k=v;` 拼接，去尾分号，权限 0600），并调用 `Name()`。
  - 落盘格式与 `api/cookies.py` 一致：`k=v;k=v`（**无空格**，最后不带分号）。读取时兼容带空格与不带空格。

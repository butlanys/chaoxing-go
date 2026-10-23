# chaoxing-go · 超星学习通自动化助手（Go 重写版）

超星学习通 / 超星尔雅 / 泛雅超星 **全自动完成任务点** 工具。

本项目是 [Samueli924/chaoxing](https://github.com/Samueli924/chaoxing)（Python 原版）的 **Go 语言重写二创**，
在保持协议行为逐字对齐的前提下，补齐了原版没有的两件事：

- 🧑‍🤝‍🧑 **多账号并发**：多个账号各自独立会话、独立限速、独立题库查询，真正并行刷课
- 🖥️ **Wails GUI**：账号管理 / 课程选择 / 任务配置 / 实时进度 / 实时日志，全程鼠标操作

原版命令行用法也完整保留（`cmd/chaoxing`）。

> ⚠️ 本项目遵循 **GPL-3.0**，仅用于**学习讨论**，禁止用于盈利。他人使用本代码进行的任何违法行为与作者无关。

---

## 功能

| 能力 | 说明 |
| --- | --- |
| 任务点类型 | 视频、音频、文档、阅读、章节检测（作业）、直播、空页面 |
| 视频进度 | 支持倍速（1~2x）、随机上报间隔（30~90s）、`rt` 参数推导、403 自动恢复（刷新状态 / 重登 / 验证码通关） |
| 章节检测 | 题库搜题 + 覆盖率阈值 + 提交/仅保存两种模式 + 答错后带错题反馈自动重做 |
| 字体加密 | 自实现 TTF `glyf` 解析 + 字形哈希，破解超星 `cxSecretStyle` 加密题目（与原版算法逐字节一致） |
| 题库 | 言溪 / GO题 / LIKE知识库 / TikuAdapter / 任意 OpenAI 兼容大模型 / 硅基流动 / 手动输入 / 多题库顺序回退 |
| 通知 | Server酱 / Qmsg酱 / Bark / Telegram |
| 章节学习次数 | 可选在刷课后轮询章节累加学习次数（含 setlog 与 monitor 心跳） |
| 其他 | 自动签到、cookies 登录、配置文件导入导出、题目缓存（`cache.json`）、结果导出 |

## 架构

```
chaoxing-go/
├── main.go / app.go        Wails 应用入口与前端绑定（账号/课程/任务/日志/配置）
├── cmd/chaoxing/           无 GUI 命令行版
├── internal/
│   ├── consts/             全局常量（AES Key、UA、Referer、阈值）
│   ├── logx/               带账号标签的日志 + 多出口 Sink（GUI 事件推送 / CLI stderr）
│   ├── crypto/             AES-128-CBC 登录加密、视频 enc 的 md5、时间戳
│   ├── font/               TTF glyf 解析、字形哈希、超星字体解密、康熙部首替换
│   ├── htmlx/              课程/章节/任务卡片/题目/作答记录解析（纯函数）
│   ├── tiku/               题库 provider、答案校验、题目缓存
│   ├── captcha/            验证码取图/识别/提交流程
│   ├── config/             config.ini 读写（保留注释的就地替换）
│   ├── notify/             外部通知服务
│   ├── store/              多账号持久化（密码本机密钥加密 + 原子写）
│   ├── api/                超星 HTTP 客户端（协议方法）
│   └── engine/             章节优先队列 + 重试 + 多账号并发调度
└── frontend/               Vite + React + TypeScript + Ant Design v5
```

**并发模型**：`engine.Manager` 为每个账号起一个 goroutine（账号之间完全并发）；账号内部由 `Runner`
起 `jobs` 个 worker 处理章节优先队列，失败/未开启章节进入重试队列（默认最多 5 次），
重试用「未完成计数 + 条件变量」保证不会提前退出。

## 构建

### 依赖

```bash
# Go 1.25+、Node 18+、Wails v2
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

> 字体映射表（`internal/font/data/font_map_table.json`，1.5MB）已通过 `//go:embed` 编进二进制，
> 发布出去的单文件即可解密超星加密字体。若想自行更新映射表，把新的
> `resource/font_map_table.json` 放到可执行文件旁边即可覆盖内嵌版本。

### Linux

Debian 13 (trixie) 起只提供 webkit2gtk-4.1，**必须**带 `webkit2_41` 构建标签：

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
wails build -tags webkit2_41
# 产物: build/bin/chaoxing-go
```

### Windows（交叉编译）

```bash
sudo apt install gcc-mingw-w64-x86-64 nsis
wails build -platform windows/amd64 -nsis
```

### 命令行版

```bash
go build -o chaoxing ./cmd/chaoxing
```

### 测试

```bash
go test ./...
python3 scripts/gen_font_fixture.py   # 生成字体交叉验证参考数据（可选）
```

### 构建产物

| 文件 | 说明 |
| --- | --- |
| `build/bin/chaoxing-go` | Linux GUI（本机 arm64；amd64 用 `-platform linux/amd64`） |
| `build/bin/chaoxing-go.exe` | Windows x86-64 GUI（含 WebView2 依赖，Win10+ 自带） |
| `build/bin/chaoxing.exe` | Windows x86-64 命令行版 |

### 开发文档

`docs/` 目录下是移植过程中使用的接口契约与逐模块实现说明：
`接口契约.md`、`api实现说明-1-会话与登录.md`、`api实现说明-2-课程与视频.md`、`api实现说明-3-作业与杂项.md`。

## 使用

### GUI

启动后在左侧依次操作：**账号管理 → 课程选择 → 任务与配置 → 开始刷课 → 实时日志**。

- 账号：支持密码登录与 cookies 登录，可备注、可单独「登录测试」
- 课程：每个账号单独拉取并勾选（同一 `courseId` 多班级会分别完成）
- 配置：倍速、并发章节数、未开启章节处理策略、题库、通知，可导入/导出 `config.ini`
- 日志：按账号/级别筛选、关键词搜索、导出运行结果

配置与账号数据存放在 `~/.chaoxing-go/`：

```
~/.chaoxing-go/
├── accounts.json     # 账号（密码 AES-GCM 加密，权限 0600）
├── key.bin           # 本机加密密钥（权限 0600）
├── config.ini        # 配置
└── cookies_<id>.txt  # 各账号 cookies
```

### 命令行

```bash
# 配置文件方式
./chaoxing -c config.ini

# 命令行参数方式
./chaoxing -u 手机号 -p 密码 -l 课程ID1,课程ID2 -s 1.5 -j 4 -a retry

# 使用 cookies 登录
./chaoxing -c config.ini --use-cookies
```

参数说明：

| 参数 | 说明 |
| --- | --- |
| `-c` | 配置文件路径 |
| `-u` / `-p` | 手机号 / 密码 |
| `-l` | 要学习的课程ID，逗号分隔（留空则列出课程并交互选择） |
| `-s` | 视频倍速，默认 1，上限 2 |
| `-j` | 同时进行的章节数，默认 4 |
| `-a` | 遇到关闭任务点的行为：`retry` / `ask` / `continue` |
| `--use-cookies` | 使用 `cookies.txt` 登录 |
| `-lc` / `-tc` | 开启章节学习次数增加 / 目标次数（默认 100） |
| `-v` | 输出 TRACE 级日志 |

### 配置文件

见仓库根目录的 `config_template.ini`，与原版格式完全兼容（可直接复用原版配置）。
题库 `[tiku]` 节的 `provider` 支持逗号分隔多个实现顺序回退，例如：

```ini
[tiku]
provider = TikuGo,TikuYanxi,AI
submit = false
cover_rate = 0.9
```

## 与原版 Python 的差异

**刻意保持一致**：所有接口 URL、请求方法、参数名与取值、请求头、AES 密钥与加密方式、
视频 `enc` 的 md5 公式、字体字形哈希算法、题库覆盖率与提交判定、章节检测重做流程、
日志文案（中文）。

**Go 版新增/调整**：

- 多账号并发调度（原版单账号）
- Wails GUI 与账号持久化（原版无）
- 命令行版的交互式课程选择保留，但手动答题改为回调注入（GUI 弹窗 / CLI 提示）
- 验证码 OCR 从本地 `ddddocr`（Python 库）改为可配置的 HTTP OCR 服务；未配置时自动降级为跳过，不影响主流程
- `notopen_action=ask` 在 GUI 下通过对话框询问（原版该分支实际等同 `retry`）
- 配置读取对非法数值容错（回退默认值而非崩溃）
- 通知发送失败会返回 error 给调用方（原版仅打日志）

## 免责声明

- 本代码遵循 [GPL-3.0](LICENSE) 协议，允许开源/免费使用、引用、修改、衍生，**不允许**修改或衍生的代码作为闭源商业软件发布和销售，**禁止**使用本代码盈利。
- 本代码仅用于学习讨论，禁止用于盈利。他人或组织使用本代码进行的任何违法行为与本人无关。

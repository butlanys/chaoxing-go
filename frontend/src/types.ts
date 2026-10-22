// 与 Go 侧 JSON tag 一一对应的数据结构。
// 注意: Config 的 Go 字段名没有 json tag, 因此必须使用大写键名 Common / Tiku / Notification / Path。

/** AppInfo() 的返回值 */
export interface AppInfo {
  name: string
  version: string
  dataDir: string
  config: string
  upstream: string
  license: string
}

/** 提交给 AddAccount / UpdateAccount 的表单; UpdateAccount 时 password 为空表示不修改原密码 */
export interface AccountInput {
  id: string
  username: string
  password: string
  remark: string
  enabled: boolean
  use_cookies: boolean
  cookies: string
  course_ids: string[]
}

/** 账号视图 (不含明文密码) */
export interface AccountView {
  id: string
  username: string
  remark: string
  enabled: boolean
  use_cookies: boolean
  has_password: boolean
  has_cookies: boolean
  course_ids: string[]
  /** ISO 时间字符串, Go 的零值时间为 "0001-01-01T00:00:00Z" */
  last_login: string
  last_status: string
  label: string
}

/** 课程; 唯一键是 courseId + "/" + clazzId */
export interface Course {
  id: string
  info: string
  roleid: string
  clazzId: string
  courseId: string
  cpi: string
  title: string
  desc: string
  teacher: string
}

/** 课程唯一键 */
export function courseKey(c: Course): string {
  return `${c.courseId}/${c.clazzId}`
}

/** StartRun 的请求体 */
export interface RunRequest {
  account_ids: string[]
  /** 账号ID -> 课程 key(courseId/clazzId) 列表 */
  course_ids: Record<string, string[]>
  speed: number
  jobs: number
  notopen_action: string
  retry_interval: number
  add_learning_count: boolean
  target_count: number
}

export type ProgressState = 'pending' | 'running' | 'done' | 'error' | 'stopped'

/** 单账号当前进度 */
export interface Progress {
  account: string
  course: string
  chapter: string
  total: number
  done: number
  failed: number
  state: ProgressState | string
}

export type LogLevel = 'trace' | 'debug' | 'info' | 'success' | 'warn' | 'error'

/** 一条日志 */
export interface LogEntry {
  time: string
  level: LogLevel
  account: string
  msg: string
}

/** 登录结果 */
export interface LoginResult {
  status: boolean
  msg: string
  realName: string
}

/** [common] 节 */
export interface CommonConfig {
  UseCookies: boolean
  Username: string
  Password: string
  CourseList: string[]
  Speed: number
  Jobs: number
  NotOpenAction: string
  RetryInterval: number
  WorkRedoEnabled: boolean
  WorkMaxRetries: number
  AddLearningCount: boolean
  TargetCount: number
}

/** 完整配置; 键名与 Go 结构体字段名一致 (无 json tag) */
export interface Config {
  Common: CommonConfig
  Tiku: Record<string, string>
  Notification: Record<string, string>
  Path: string
}

/** 运行参数默认值, 与 Python 版 config_template.ini 对齐 */
export function defaultCommon(): CommonConfig {
  return {
    UseCookies: false,
    Username: '',
    Password: '',
    CourseList: [],
    Speed: 1,
    Jobs: 4,
    NotOpenAction: 'retry',
    RetryInterval: 1.0,
    WorkRedoEnabled: false,
    WorkMaxRetries: 3,
    AddLearningCount: false,
    TargetCount: 100,
  }
}

/** 构造一份默认配置 */
export function defaultConfig(): Config {
  return {
    Common: defaultCommon(),
    Tiku: {
      provider: 'TikuYanxi',
      check_llm_connection: 'true',
      submit: 'false',
      cover_rate: '0.9',
      delay: '1.0',
      tokens: '',
      url: '',
      endpoint: '',
      key: '',
      model: '',
      min_interval_seconds: '3',
      http_proxy: '',
      siliconflow_key: '',
      siliconflow_model: 'deepseek-ai/DeepSeek-R1',
      siliconflow_endpoint: 'https://api.siliconflow.cn/v1/chat/completions',
      manual_mode_default: 'batch',
      manual_mode_separator: ';',
    },
    Notification: { provider: '', url: '', tg_chat_id: '' },
    Path: '',
  }
}

/** 状态标签的展示文案与颜色 */
export const STATE_META: Record<string, { text: string; color: string }> = {
  pending: { text: '等待中', color: 'default' },
  running: { text: '进行中', color: 'processing' },
  done: { text: '已完成', color: 'success' },
  error: { text: '出错', color: 'error' },
  stopped: { text: '已停止', color: 'warning' },
}

// 与 Go 侧 JSON tag 一一对应的数据结构。
// 注意: Go 的 config.Config / config.Common 都带 json tag, 序列化后键名是
// common / tiku / notification / path 以及 use_cookies / speed / ... 这样的 snake_case,
// 因此这里必须用小写键名, 否则读到的全是 undefined (页面会因渲染期报错而全白)。
// 详见 frontend/wailsjs/go/models.ts (Wails 生成的同一份结构)。

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

/** [common] 节; 键名与 Go struct 的 json tag 完全一致 (snake_case) */
export interface CommonConfig {
  use_cookies: boolean
  username: string
  password: string
  course_list: string[]
  speed: number
  jobs: number
  notopen_action: string
  retry_interval: number
  work_redo_enabled: boolean
  work_max_retries: number
  add_learning_count: boolean
  target_count: number
}

/** 完整配置; 键名与 Go struct 的 json tag 一致 (common / tiku / notification / path) */
export interface Config {
  common: CommonConfig
  tiku: Record<string, string>
  notification: Record<string, string>
  path: string
}

/** 运行参数默认值, 与 Python 版 config_template.ini 对齐 */
export function defaultCommon(): CommonConfig {
  return {
    use_cookies: false,
    username: '',
    password: '',
    course_list: [],
    speed: 1,
    jobs: 4,
    notopen_action: 'retry',
    retry_interval: 1.0,
    work_redo_enabled: false,
    work_max_retries: 3,
    add_learning_count: false,
    target_count: 100,
  }
}

/** 构造一份默认配置 */
export function defaultConfig(): Config {
  return {
    common: defaultCommon(),
    tiku: {
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
    notification: { provider: '', url: '', tg_chat_id: '' },
    path: '',
  }
}

function numOr(v: unknown, def: number): number {
  const n = typeof v === 'number' ? v : Number(v)
  return Number.isFinite(n) ? n : def
}

function strOr(v: unknown, def: string): string {
  return typeof v === 'string' ? v : v == null ? def : String(v)
}

function boolOr(v: unknown, def: boolean): boolean {
  if (typeof v === 'boolean') return v
  if (typeof v === 'string') {
    const s = v.trim().toLowerCase()
    if (s === 'true' || s === '1' || s === 'yes' || s === 'y') return true
    if (s === 'false' || s === '0' || s === 'no' || s === 'n') return false
  }
  if (typeof v === 'number') return v !== 0
  return def
}

/** 把任意 ini 映射归一化成 Record<string, string>, null/undefined 安全 */
function strMap(v: unknown): Record<string, string> {
  if (!v || typeof v !== 'object') return {}
  const out: Record<string, string> = {}
  for (const [k, val] of Object.entries(v as Record<string, unknown>)) {
    if (val == null) continue
    out[k] = String(val)
  }
  return out
}

/**
 * 把后端返回的配置归一化成完整的 Config。
 *
 * 为什么必须做这一步: Go 的 config.Config 里 common 是值类型, tiku/notification 是 map,
 * 当 ini 缺节时 map 会是 nil 并被序列化成 JSON null; 而前端多处直接解引用
 * (common.speed / Object.keys(tiku)), 一旦拿到 null 就会在渲染期抛错,
 * 整棵 React 树被卸载 → 页面全白。所有后端入口统一过这个函数。
 */
export function normalizeConfig(raw: unknown): Config {
  const r = (raw ?? {}) as Record<string, unknown>
  const c = (r.common ?? {}) as Record<string, unknown>
  const def = defaultCommon()
  return {
    common: {
      use_cookies: boolOr(c.use_cookies, def.use_cookies),
      username: strOr(c.username, ''),
      password: strOr(c.password, ''),
      course_list: Array.isArray(c.course_list) ? c.course_list.map((x) => String(x)) : [],
      speed: numOr(c.speed, def.speed),
      jobs: numOr(c.jobs, def.jobs),
      notopen_action: strOr(c.notopen_action, def.notopen_action),
      retry_interval: numOr(c.retry_interval, def.retry_interval),
      work_redo_enabled: boolOr(c.work_redo_enabled, def.work_redo_enabled),
      work_max_retries: numOr(c.work_max_retries, def.work_max_retries),
      add_learning_count: boolOr(c.add_learning_count, def.add_learning_count),
      target_count: numOr(c.target_count, def.target_count),
    },
    tiku: strMap(r.tiku),
    notification: strMap(r.notification),
    path: strOr(r.path, ''),
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

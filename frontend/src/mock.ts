// 纯前端 mock: 当 (window as any).go?.main?.App 不存在时 (即 `npm run dev` 在浏览器里预览,
// 或 Wails 绑定尚未就绪) 由 bindings.ts 回退到这里, 保证界面能完整渲染与交互。
//
// 这里刻意模拟了真实的异步语义: 所有方法返回 Promise, 失败时 reject。

import {
  AccountInput,
  AccountView,
  AppInfo,
  Config,
  Course,
  defaultConfig,
  LogEntry,
  LogLevel,
  LoginResult,
  normalizeConfig,
  Progress,
  ProgressState,
  RunRequest,
} from './types'

type EventName = 'log' | 'progress' | 'done'
type Handler = (payload: unknown) => void

const listeners: Record<EventName, Set<Handler>> = {
  log: new Set(),
  progress: new Set(),
  done: new Set(),
}

function emit(name: EventName, payload: unknown) {
  listeners[name].forEach((h) => {
    try {
      h(payload)
    } catch (e) {
      // eslint-disable-next-line no-console
      console.error('[mock] 事件回调异常', e)
    }
  })
}

/** mock 的事件总线; bindings 用它模拟 Wails runtime.EventsOn */
export function onMockEvent(name: EventName, handler: Handler): () => void {
  listeners[name].add(handler)
  return () => listeners[name].delete(handler)
}

// ---------- 内存数据 ----------

let seq = 1
function nextId(prefix: string) {
  seq += 1
  return `${prefix}${seq}`
}

const accounts: AccountView[] = [
  {
    id: 'acc_1001',
    username: '13800000001',
    remark: '主账号',
    enabled: true,
    use_cookies: true,
    has_password: true,
    has_cookies: true,
    course_ids: [],
    last_login: new Date(Date.now() - 3600_000).toISOString(),
    last_status: '登录成功',
    label: '主账号',
  },
  {
    id: 'acc_1002',
    username: '13800000002',
    remark: '备用账号',
    enabled: false,
    use_cookies: false,
    has_password: true,
    has_cookies: false,
    course_ids: [],
    last_login: '0001-01-01T00:00:00Z',
    last_status: '',
    label: '备用账号',
  },
]

const secrets: Record<string, { password: string; cookies: string }> = {
  acc_1001: { password: 'demo-pass-1', cookies: 'UID=demo; _uid=demo;' },
  acc_1002: { password: 'demo-pass-2', cookies: '' },
}

const courseTable: Record<string, Course[]> = {
  acc_1001: [
    {
      id: 'c1',
      info: '2025-2026 第一学期',
      roleid: '3',
      clazzId: '100001',
      courseId: '230000001',
      cpi: '111111',
      title: '马克思主义基本原理',
      desc: '马克思主义基本原理概论 (示范课程)',
      teacher: '张老师',
    },
    {
      id: 'c2',
      info: '2025-2026 第一学期',
      roleid: '3',
      clazzId: '100002',
      courseId: '230000002',
      cpi: '222222',
      title: '大学英语 (三)',
      desc: '综合英语与视听说',
      teacher: '李老师',
    },
    {
      id: 'c3',
      info: '2025-2026 第一学期',
      roleid: '3',
      clazzId: '100003',
      courseId: '230000003',
      cpi: '333333',
      title: '高等数学 A',
      desc: '微积分与线性代数基础',
      teacher: '王老师',
    },
  ],
  acc_1002: [
    {
      id: 'c4',
      info: '2025-2026 第一学期',
      roleid: '3',
      clazzId: '200001',
      courseId: '240000001',
      cpi: '444444',
      title: '大学物理',
      desc: '力学与电磁学',
      teacher: '赵老师',
    },
  ],
}

let config: Config = defaultConfig()
config.common.username = '13800000001'
config.common.password = 'demo-pass-1'
config.path = '~/.chaoxing-go/config.ini'

const logs: LogEntry[] = []
const statuses = new Map<string, Progress>()
let running = false
let timer: number | null = null
let runSeq = 0

function nowISO() {
  return new Date().toISOString()
}

function pushLog(level: LogLevel, account: string, msg: string) {
  const entry: LogEntry = { time: nowISO(), level, account, msg }
  logs.push(entry)
  if (logs.length > 2000) logs.splice(0, logs.length - 2000)
  emit('log', entry)
}

function delay<T>(value: T, ms = 120): Promise<T> {
  return new Promise((resolve) => setTimeout(() => resolve(value), ms))
}

function fail(msg: string): Promise<never> {
  return new Promise((_, reject) => setTimeout(() => reject(new Error(msg)), 120))
}

// ---------- mock 方法 ----------

export const mock = {
  AppInfo(): Promise<AppInfo> {
    return delay({
      name: 'chaoxing-go',
      version: '1.0.0 (mock)',
      dataDir: '~/.chaoxing-go',
      config: '~/.chaoxing-go/config.ini',
      upstream: 'https://github.com/Samueli924/chaoxing',
      license: 'GPL-3.0',
    })
  },

  ListAccounts(): Promise<AccountView[]> {
    return delay(accounts.map((a) => ({ ...a, course_ids: [...a.course_ids] })))
  },

  AddAccount(input: AccountInput): Promise<AccountView> {
    if (!input.username.trim() && !input.use_cookies) {
      return fail('手机号不能为空')
    }
    const id = nextId('acc_')
    const view: AccountView = {
      id,
      username: input.username.trim(),
      remark: input.remark,
      enabled: input.enabled,
      use_cookies: input.use_cookies,
      has_password: input.password !== '',
      has_cookies: input.cookies.trim() !== '',
      course_ids: [...input.course_ids],
      last_login: '0001-01-01T00:00:00Z',
      last_status: '',
      label: input.remark || input.username.trim() || id,
    }
    accounts.push(view)
    secrets[id] = { password: input.password, cookies: input.cookies }
    pushLog('info', '', `已新增账号: ${view.label}`)
    return delay(view)
  },

  UpdateAccount(input: AccountInput): Promise<void> {
    const idx = accounts.findIndex((a) => a.id === input.id)
    if (idx < 0) return fail(`账号不存在: ${input.id}`)
    const cur = accounts[idx]
    const sec = secrets[input.id] ?? { password: '', cookies: '' }
    if (input.password !== '') sec.password = input.password
    sec.cookies = input.cookies
    secrets[input.id] = sec
    accounts[idx] = {
      ...cur,
      username: input.username.trim(),
      remark: input.remark,
      enabled: input.enabled,
      use_cookies: input.use_cookies,
      has_password: sec.password !== '',
      has_cookies: sec.cookies.trim() !== '',
      course_ids: [...input.course_ids],
      label: input.remark || input.username.trim() || input.id,
    }
    pushLog('info', '', `已更新账号: ${accounts[idx].label}`)
    return delay(undefined)
  },

  DeleteAccount(id: string): Promise<void> {
    const idx = accounts.findIndex((a) => a.id === id)
    if (idx < 0) return fail(`账号不存在: ${id}`)
    accounts.splice(idx, 1)
    delete courseTable[id]
    delete secrets[id]
    statuses.delete(id)
    pushLog('info', '', `已删除账号: ${id}`)
    return delay(undefined)
  },

  LoginAccount(id: string): Promise<LoginResult> {
    const acc = accounts.find((a) => a.id === id)
    if (!acc) return fail(`账号不存在: ${id}`)
    acc.last_status = '登录成功'
    acc.last_login = nowISO()
    acc.has_cookies = true
    pushLog('success', acc.label, '登录成功 (mock)')
    return delay({ status: true, msg: '登录成功', realName: `模拟用户-${acc.label}` }, 700)
  },

  FetchCourses(id: string): Promise<Course[]> {
    const acc = accounts.find((a) => a.id === id)
    if (!acc) return fail(`账号不存在: ${id}`)
    const list = courseTable[id] ?? []
    pushLog('info', acc.label, `已获取课程列表, 共 ${list.length} 门 (mock)`)
    return delay(list.map((c) => ({ ...c })), 600)
  },

  CachedCourses(id: string): Promise<Course[]> {
    return delay((courseTable[id] ?? []).map((c) => ({ ...c })))
  },

  StartRun(req: RunRequest): Promise<void> {
    if (running) return fail('已有任务在运行，请先停止')
    if (!req.account_ids.length) return fail('请至少选择一个账号')
    const picked = req.account_ids
      .map((id) => accounts.find((a) => a.id === id))
      .filter((a): a is AccountView => !!a)
    if (!picked.length) return fail('没有可执行的账号任务（请先拉取课程列表并勾选课程）')

    running = true
    runSeq += 1
    const myRun = runSeq
    statuses.clear()

    const tasks: Progress[] = []
    picked.forEach((acc) => {
      const keys = req.course_ids[acc.id] ?? []
      const courses = (courseTable[acc.id] ?? []).filter(
        (c) => keys.length === 0 || keys.includes(`${c.courseId}/${c.clazzId}`),
      )
      const list = courses.length ? courses : courseTable[acc.id] ?? []
      list.forEach((c) => {
        const p: Progress = {
          account: acc.label,
          course: c.title,
          chapter: '准备中',
          total: 12,
          done: 0,
          failed: 0,
          state: 'pending' as ProgressState,
        }
        statuses.set(`${acc.label}/${c.title}`, p)
        tasks.push(p)
      })
    })

    pushLog('info', '', `开始执行任务: ${picked.length} 个账号并发 (mock)`)

    let cursor = 0
    timer = window.setInterval(() => {
      if (myRun !== runSeq || !running) return
      const p = tasks[cursor % tasks.length]
      if (!p) return
      if (p.state === 'pending') {
        p.state = 'running'
        pushLog('info', p.account, `开始学习课程: ${p.course}`)
      } else if (p.done < p.total) {
        p.done += 1
        p.chapter = `第 ${p.done} 节`
        if (Math.random() < 0.06) {
          p.failed += 1
          pushLog('warn', p.account, `章节处理失败, 将重试: ${p.chapter}`)
        } else {
          pushLog('success', p.account, `章节完成: ${p.chapter} (${p.done}/${p.total})`)
        }
        if (p.done >= p.total) {
          p.state = 'done'
          p.chapter = '全部完成'
          pushLog('success', p.account, `课程已完成: ${p.course}`)
        }
      }
      statuses.set(`${p.account}/${p.course}`, { ...p })
      emit('progress', { ...p })
      cursor += 1

      if (tasks.every((t) => t.state === 'done' || t.state === 'error' || t.state === 'stopped')) {
        finishRun(myRun)
      }
    }, 900)

    return delay(undefined, 80)
  },

  StopRun(): Promise<void> {
    if (running) {
      running = false
      runSeq += 1
      if (timer !== null) {
        window.clearInterval(timer)
        timer = null
      }
      statuses.forEach((p) => {
        if (p.state === 'running' || p.state === 'pending') {
          p.state = 'stopped'
          statuses.set(`${p.account}/${p.course}`, { ...p })
          emit('progress', { ...p })
        }
      })
      pushLog('warn', '', '已请求停止所有任务 (mock)')
      emit('done', Array.from(statuses.values()))
    }
    return delay(undefined)
  },

  GetStatuses(): Promise<Progress[]> {
    return delay(Array.from(statuses.values()))
  },

  IsRunning(): Promise<boolean> {
    return delay(running)
  },

  GetLogs(limit: number): Promise<LogEntry[]> {
    const all = logs.slice()
    const out = limit > 0 && all.length > limit ? all.slice(all.length - limit) : all
    return delay(out)
  },

  SetLogLevel(level: string): Promise<void> {
    pushLog('info', '', `日志级别已设置为 ${level} (mock)`)
    return delay(undefined)
  },

  LoadConfig(): Promise<Config> {
    return delay(cloneConfig(config))
  },

  SaveConfig(cfg: Config): Promise<void> {
    if (!cfg) return fail('配置为空')
    config = cloneConfig(cfg)
    pushLog('info', '', '配置已保存 (mock)')
    return delay(undefined)
  },

  LoadConfigFile(path: string): Promise<Config> {
    if (!path) return fail('路径为空')
    config = cloneConfig(config)
    config.path = path
    pushLog('info', '', `已载入配置文件: ${path} (mock)`)
    return delay(cloneConfig(config))
  },

  SaveConfigAs(path: string, cfg: Config): Promise<void> {
    if (!path) return fail('路径为空')
    if (cfg) config = cloneConfig(cfg)
    config.path = path
    pushLog('info', '', `配置已另存为: ${path} (mock)`)
    return delay(undefined)
  },

  PickConfigFile(): Promise<string> {
    // 浏览器里没有文件选择框, 返回一个演示路径
    const p = '~/.chaoxing-go/config.ini'
    pushLog('info', '', `已选择配置文件: ${p} (mock)`)
    return delay(p, 60)
  },

  ExportResults(): Promise<string> {
    const p = `~/.chaoxing-go/result_${Date.now()}.txt`
    pushLog('success', '', `运行结果已导出: ${p} (mock)`)
    return delay(p, 300)
  },
}

function finishRun(myRun: number) {
  if (myRun !== runSeq) return
  running = false
  if (timer !== null) {
    window.clearInterval(timer)
    timer = null
  }
  pushLog('success', '', '全部任务执行完毕 (mock)')
  emit('done', Array.from(statuses.values()))
}

function cloneConfig(c: Config): Config {
  const n = normalizeConfig(c)
  return {
    common: { ...n.common, course_list: [...n.common.course_list] },
    tiku: { ...n.tiku },
    notification: { ...n.notification },
    path: n.path,
  }
}

/** mock 模式下预置几条日志, 便于直接看到日志页效果 */
export function seedMockLogs() {
  if (logs.length) return
  const seed: Array<[LogLevel, string, string]> = [
    ['info', '', 'chaoxing-go 已启动 (浏览器 mock 模式)'],
    ['debug', '主账号', '加载本地缓存 cookie 成功'],
    ['trace', '主账号', 'GET https://mooc1.chaoxing.com/visit/courses/list'],
    ['success', '主账号', '登录成功, 真实姓名: 模拟用户'],
    ['warn', '备用账号', '未启用, 跳过本次任务'],
    ['error', '主账号', '示例错误: 网络请求超时 (仅用于演示日志配色)'],
  ]
  seed.forEach(([level, account, msg], i) => {
    logs.push({ time: new Date(Date.now() - (seed.length - i) * 1000).toISOString(), level, account, msg })
  })
}

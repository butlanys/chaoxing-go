// 全局共享状态: 账号 / 课程 / 勾选 / 进度 / 配置 / 日志。
// 只用 useState + useRef 管理, 不引入任何状态库。

import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { bindings, isMockMode, onEvent } from './bindings'
import {
  AccountView,
  AppInfo,
  Config,
  Course,
  defaultConfig,
  LogEntry,
  Progress,
} from './types'

interface Store {
  ready: boolean
  mockMode: boolean
  appInfo: AppInfo | null

  accounts: AccountView[]
  accountsLoading: boolean
  reloadAccounts: () => Promise<void>

  courses: Record<string, Course[]>
  setCourses: (accountId: string, list: Course[]) => void

  selectedAccounts: string[]
  setSelectedAccounts: React.Dispatch<React.SetStateAction<string[]>>

  selectedCourses: Record<string, string[]>
  setSelectedCourses: React.Dispatch<React.SetStateAction<Record<string, string[]>>>
  toggleCourse: (accountId: string, key: string, checked: boolean) => void
  setAccountCourses: (accountId: string, keys: string[]) => void

  statuses: Progress[]
  setStatuses: React.Dispatch<React.SetStateAction<Progress[]>>
  running: boolean
  setRunning: React.Dispatch<React.SetStateAction<boolean>>

  config: Config
  setConfig: React.Dispatch<React.SetStateAction<Config>>
  reloadConfig: () => Promise<void>

  logs: LogEntry[]
  clearLogs: () => void
}

const Ctx = createContext<Store | null>(null)

export function useStore(): Store {
  const s = useContext(Ctx)
  if (!s) throw new Error('useStore 必须在 <StoreProvider> 内使用')
  return s
}

/** 日志批量刷新间隔 (毫秒): 高频日志事件先入缓冲, 定时合并进 state, 避免频繁重渲染 */
const LOG_FLUSH_MS = 250
const LOG_MAX = 3000

export function StoreProvider({ children }: { children: React.ReactNode }) {
  const [ready, setReady] = useState(false)
  const [mockMode] = useState(() => isMockMode())
  const [appInfo, setAppInfo] = useState<AppInfo | null>(null)

  const [accounts, setAccounts] = useState<AccountView[]>([])
  const [accountsLoading, setAccountsLoading] = useState(false)

  const [courses, setCoursesState] = useState<Record<string, Course[]>>({})
  const [selectedAccounts, setSelectedAccounts] = useState<string[]>([])
  const [selectedCourses, setSelectedCourses] = useState<Record<string, string[]>>({})

  const [statuses, setStatuses] = useState<Progress[]>([])
  const [running, setRunning] = useState(false)

  const [config, setConfig] = useState<Config>(() => defaultConfig())

  const [logs, setLogs] = useState<LogEntry[]>([])
  const logBuf = useRef<LogEntry[]>([])

  const setCourses = useCallback((accountId: string, list: Course[]) => {
    setCoursesState((prev) => ({ ...prev, [accountId]: list }))
  }, [])

  const toggleCourse = useCallback((accountId: string, key: string, checked: boolean) => {
    setSelectedCourses((prev) => {
      const cur = prev[accountId] ?? []
      const next = checked ? Array.from(new Set([...cur, key])) : cur.filter((k) => k !== key)
      return { ...prev, [accountId]: next }
    })
  }, [])

  const setAccountCourses = useCallback((accountId: string, keys: string[]) => {
    setSelectedCourses((prev) => ({ ...prev, [accountId]: keys }))
  }, [])

  const reloadAccounts = useCallback(async () => {
    setAccountsLoading(true)
    try {
      const list = await bindings.listAccounts()
      setAccounts(list ?? [])
    } finally {
      setAccountsLoading(false)
    }
  }, [])

  const reloadConfig = useCallback(async () => {
    const cfg = await bindings.loadConfig()
    if (cfg) setConfig(cfg)
  }, [])

  const clearLogs = useCallback(() => {
    logBuf.current = []
    setLogs([])
  }, [])

  // 首屏加载: 应用信息 / 账号 / 课程缓存 / 配置 / 历史日志 / 进度
  useEffect(() => {
    let alive = true
    ;(async () => {
      try {
        const [info, list, cfg, hist, st] = await Promise.all([
          bindings.appInfo().catch(() => null),
          bindings.listAccounts().catch(() => [] as AccountView[]),
          bindings.loadConfig().catch(() => null),
          bindings.getLogs(500).catch(() => [] as LogEntry[]),
          bindings.getStatuses().catch(() => [] as Progress[]),
        ])
        if (!alive) return
        if (info) setAppInfo(info)
        setAccounts(list ?? [])
        if (cfg) setConfig(cfg)
        if (hist?.length) setLogs(hist.slice(-LOG_MAX))
        if (st?.length) setStatuses(st)
        const isRun = await bindings.isRunning().catch(() => false)
        if (alive) setRunning(!!isRun)

        // 预热各账号已缓存的课程 (后端内存缓存), 供课程页/账号表单使用
        const pairs = await Promise.all(
          (list ?? []).map(async (a) => {
            const cs = await bindings.cachedCourses(a.id).catch(() => [] as Course[])
            return [a.id, cs ?? []] as const
          }),
        )
        if (!alive) return
        const map: Record<string, Course[]> = {}
        pairs.forEach(([id, cs]) => {
          if (cs.length) map[id] = cs
        })
        if (Object.keys(map).length) setCoursesState((prev) => ({ ...map, ...prev }))
      } finally {
        if (alive) setReady(true)
      }
    })()
    return () => {
      alive = false
    }
  }, [])

  // 事件订阅: log / progress / done
  useEffect(() => {
    const offLog = onEvent('log', (e) => {
      if (!e) return
      logBuf.current.push(e)
      if (logBuf.current.length > LOG_MAX) {
        logBuf.current.splice(0, logBuf.current.length - LOG_MAX)
      }
    })
    const offProgress = onEvent('progress', (p) => {
      if (!p) return
      setStatuses((prev) => {
        const idx = prev.findIndex((x) => x.account === p.account && x.course === p.course)
        if (idx < 0) return [...prev, p]
        const next = prev.slice()
        next[idx] = p
        return next
      })
      if (p.state === 'running' || p.state === 'pending') setRunning(true)
    })
    const offDone = onEvent('done', (list) => {
      if (Array.isArray(list) && list.length) setStatuses(list)
      setRunning(false)
    })
    return () => {
      offLog()
      offProgress()
      offDone()
    }
  }, [])

  // 日志缓冲定时刷新
  useEffect(() => {
    const t = window.setInterval(() => {
      if (!logBuf.current.length) return
      const chunk = logBuf.current
      logBuf.current = []
      setLogs((prev) => {
        const next = prev.concat(chunk)
        return next.length > LOG_MAX ? next.slice(next.length - LOG_MAX) : next
      })
    }, LOG_FLUSH_MS)
    return () => window.clearInterval(t)
  }, [])

  // 运行状态轮询 (2 秒一次)
  useEffect(() => {
    const t = window.setInterval(async () => {
      try {
        const r = await bindings.isRunning()
        setRunning((prev) => (prev === !!r ? prev : !!r))
      } catch {
        /* 忽略轮询错误 */
      }
    }, 2000)
    return () => window.clearInterval(t)
  }, [])

  const value = useMemo<Store>(
    () => ({
      ready,
      mockMode,
      appInfo,
      accounts,
      accountsLoading,
      reloadAccounts,
      courses,
      setCourses,
      selectedAccounts,
      setSelectedAccounts,
      selectedCourses,
      setSelectedCourses,
      toggleCourse,
      setAccountCourses,
      statuses,
      setStatuses,
      running,
      setRunning,
      config,
      setConfig,
      reloadConfig,
      logs,
      clearLogs,
    }),
    [
      ready,
      mockMode,
      appInfo,
      accounts,
      accountsLoading,
      reloadAccounts,
      courses,
      setCourses,
      selectedAccounts,
      selectedCourses,
      toggleCourse,
      setAccountCourses,
      statuses,
      running,
      config,
      reloadConfig,
      logs,
      clearLogs,
    ],
  )

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

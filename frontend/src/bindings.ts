// 类型化的 Wails 绑定封装。
//
// 不依赖 wails 自动生成的 wailsjs 目录 (构建顺序不可靠), 运行时直接读取
//   - (window as any).go?.main?.App      -> 后端方法
//   - (window as any).runtime            -> 事件订阅
// 当二者不存在时 (浏览器里 `npm run dev` 预览) 自动回退到 ./mock 的内存实现。
//
// 所有方法返回 Promise; 后端出错时 Promise 会 reject, 调用方用 catch + antd message 报错。

import { mock, onMockEvent } from './mock'
import {
  AccountInput,
  AccountView,
  AppInfo,
  Config,
  Course,
  LogEntry,
  LoginResult,
  Progress,
  RunRequest,
  normalizeConfig,
} from './types'

interface WailsApp {
  AppInfo(): Promise<AppInfo>
  ListAccounts(): Promise<AccountView[]>
  AddAccount(input: AccountInput): Promise<AccountView>
  UpdateAccount(input: AccountInput): Promise<void>
  DeleteAccount(id: string): Promise<void>
  LoginAccount(id: string): Promise<LoginResult>
  FetchCourses(id: string): Promise<Course[]>
  CachedCourses(id: string): Promise<Course[]>
  StartRun(req: RunRequest): Promise<void>
  StopRun(): Promise<void>
  GetStatuses(): Promise<Progress[]>
  IsRunning(): Promise<boolean>
  GetLogs(limit: number): Promise<LogEntry[]>
  SetLogLevel(level: string): Promise<void>
  LoadConfig(): Promise<Config>
  SaveConfig(cfg: Config): Promise<void>
  LoadConfigFile(path: string): Promise<Config>
  SaveConfigAs(path: string, cfg: Config): Promise<void>
  PickConfigFile(): Promise<string>
  ExportResults(): Promise<string>
}

interface WailsRuntime {
  EventsOn(eventName: string, callback: (payload: unknown) => void): (() => void) | void
  EventsOff(eventName: string, ...additionalEventNames: string[]): void
  EventsEmit?(eventName: string, ...data: unknown[]): void
}

declare global {
  interface Window {
    go?: { main?: { App?: Partial<WailsApp> } }
    runtime?: WailsRuntime
  }
}

export type EventName = 'log' | 'progress' | 'done'
export interface EventPayloads {
  log: LogEntry
  progress: Progress
  done: Progress[]
}

function getApp(): Partial<WailsApp> | undefined {
  return typeof window !== 'undefined' ? window.go?.main?.App : undefined
}

function getRuntime(): WailsRuntime | undefined {
  return typeof window !== 'undefined' ? window.runtime : undefined
}

/** 是否运行在浏览器 mock 模式 (即没有 Wails 后端) */
export function isMockMode(): boolean {
  return !getApp()
}

/**
 * 调用一个后端方法; 后端不存在或方法缺失时回退到 mock。
 * 返回的 Promise 在失败时 reject (错误信息已归一化为 Error)。
 */
function invoke<K extends keyof WailsApp>(
  method: K,
  ...args: Parameters<WailsApp[K]>
): Promise<Awaited<ReturnType<WailsApp[K]>>> {
  type R = Awaited<ReturnType<WailsApp[K]>>
  const app = getApp()
  const real = app?.[method] as ((...a: unknown[]) => Promise<R>) | undefined
  if (typeof real === 'function') {
    try {
      return Promise.resolve(real.apply(app, args)).catch((e: unknown) => {
        throw normalizeError(e)
      })
    } catch (e) {
      return Promise.reject(normalizeError(e))
    }
  }
  const m = (mock as Record<string, unknown>)[method]
  if (typeof m === 'function') {
    try {
      return Promise.resolve((m as (...a: unknown[]) => Promise<R>).apply(mock, args)).catch(
        (e: unknown) => {
          throw normalizeError(e)
        },
      )
    } catch (e) {
      return Promise.reject(normalizeError(e))
    }
  }
  return Promise.reject(new Error(`未实现的绑定方法: ${String(method)}`))
}

/** 把任意抛出物归一化成 Error */
export function normalizeError(e: unknown): Error {
  if (e instanceof Error) return e
  if (typeof e === 'string') return new Error(e)
  if (e && typeof e === 'object') {
    const anyE = e as Record<string, unknown>
    if (typeof anyE.message === 'string') return new Error(anyE.message)
    try {
      return new Error(JSON.stringify(e))
    } catch {
      return new Error('未知错误')
    }
  }
  return new Error(String(e ?? '未知错误'))
}

/** 取错误文案, 供 message.error 使用 */
export function errText(e: unknown): string {
  return normalizeError(e).message || '操作失败'
}

/** 订阅后端事件; 返回取消订阅函数 */
export function onEvent<K extends EventName>(
  name: K,
  cb: (payload: EventPayloads[K]) => void,
): () => void {
  const rt = getRuntime()
  if (rt && typeof rt.EventsOn === 'function') {
    try {
      const off = rt.EventsOn(name, (payload: unknown) => cb(payload as EventPayloads[K]))
      return () => {
        if (typeof off === 'function') {
          off()
        } else if (typeof rt.EventsOff === 'function') {
          rt.EventsOff(name)
        }
      }
    } catch {
      // 落到 mock
    }
  }
  return onMockEvent(name, (payload) => cb(payload as EventPayloads[K]))
}

/** 全部后端方法 (带类型) */
export const bindings = {
  appInfo: (): Promise<AppInfo> => invoke('AppInfo'),
  listAccounts: (): Promise<AccountView[]> => invoke('ListAccounts'),
  addAccount: (input: AccountInput): Promise<AccountView> => invoke('AddAccount', input),
  updateAccount: (input: AccountInput): Promise<void> => invoke('UpdateAccount', input),
  deleteAccount: (id: string): Promise<void> => invoke('DeleteAccount', id),
  loginAccount: (id: string): Promise<LoginResult> => invoke('LoginAccount', id),
  fetchCourses: (id: string): Promise<Course[]> => invoke('FetchCourses', id),
  cachedCourses: (id: string): Promise<Course[]> => invoke('CachedCourses', id),
  startRun: (req: RunRequest): Promise<void> => invoke('StartRun', req),
  stopRun: (): Promise<void> => invoke('StopRun'),
  getStatuses: (): Promise<Progress[]> => invoke('GetStatuses'),
  isRunning: (): Promise<boolean> => invoke('IsRunning'),
  getLogs: (limit: number): Promise<LogEntry[]> => invoke('GetLogs', limit),
  setLogLevel: (level: string): Promise<void> => invoke('SetLogLevel', level),
  loadConfig: async (): Promise<Config> => normalizeConfig(await invoke('LoadConfig')),
  saveConfig: (cfg: Config): Promise<void> => invoke('SaveConfig', cfg),
  loadConfigFile: async (path: string): Promise<Config> =>
    normalizeConfig(await invoke('LoadConfigFile', path)),
  saveConfigAs: (path: string, cfg: Config): Promise<void> => invoke('SaveConfigAs', path, cfg),
  pickConfigFile: (): Promise<string> => invoke('PickConfigFile'),
  exportResults: (): Promise<string> => invoke('ExportResults'),
}

export type Bindings = typeof bindings

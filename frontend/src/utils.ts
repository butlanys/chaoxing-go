// 通用小工具 (无第三方依赖)

import { LogLevel, ProgressState } from './types'

/** 把 ISO 时间格式化为 "YYYY-MM-DD HH:mm:ss"; 零值/空值返回占位符 */
export function formatTime(iso: string | undefined | null, placeholder = '—'): string {
  if (!iso || iso.startsWith('0001-01-01')) return placeholder
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return placeholder
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(
    d.getMinutes(),
  )}:${p(d.getSeconds())}`
}

/** 只取时分秒 */
export function formatClock(iso: string | undefined | null): string {
  if (!iso) return '--:--:--'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '--:--:--'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export const LOG_LEVELS: LogLevel[] = ['trace', 'debug', 'info', 'success', 'warn', 'error']

export const LOG_LEVEL_LABEL: Record<string, string> = {
  trace: 'TRACE',
  debug: 'DEBUG',
  info: 'INFO',
  success: 'SUCCESS',
  warn: 'WARN',
  error: 'ERROR',
}

export const LOG_LEVEL_COLOR: Record<string, string> = {
  trace: '#94a3b8',
  debug: '#60a5fa',
  info: '#1677ff',
  success: '#52c41a',
  warn: '#fa8c16',
  error: '#ff4d4f',
}

export const NOT_OPEN_ACTIONS = [
  { value: 'retry', label: 'retry · 重试 (默认)' },
  { value: 'ask', label: 'ask · 询问后决定' },
  { value: 'continue', label: 'continue · 跳过继续' },
]

export const TIKU_PROVIDERS = [
  '',
  'TikuYanxi',
  'TikuGo',
  'TikuLike',
  'TikuAdapter',
  'AI',
  'SiliconFlow',
  'TikuManual',
]

export const NOTIFY_PROVIDERS = ['', 'ServerChan', 'Qmsg', 'Bark', 'Telegram']

/** 课程唯一键 */
export function courseKeyOf(courseId: string, clazzId: string): string {
  return `${courseId}/${clazzId}`
}

/** 判断进度是否处于终态 */
export function isFinalState(state: ProgressState | string): boolean {
  return state === 'done' || state === 'error' || state === 'stopped'
}

/** 数值安全转换 */
export function toNum(v: unknown, def: number): number {
  if (typeof v === 'number' && Number.isFinite(v)) return v
  if (typeof v === 'string' && v.trim() !== '') {
    const n = Number(v)
    if (Number.isFinite(n)) return n
  }
  return def
}

/** 字符串安全转换 */
export function toStr(v: unknown, def = ''): string {
  if (v === null || v === undefined) return def
  return String(v)
}

import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Checkbox,
  Input,
  Popconfirm,
  Select,
  Space,
  Tag,
  Tooltip,
} from 'antd'
import {
  ClearOutlined,
  ExportOutlined,
  FilterOutlined,
  VerticalAlignBottomOutlined,
} from '@ant-design/icons'
import { bindings, errText } from '../bindings'
import { useStore } from '../store'
import { LOG_LEVEL_COLOR, LOG_LEVEL_LABEL, LOG_LEVELS, formatClock } from '../utils'

const MAX_RENDER = 2000

export default function LogPage() {
  const { message } = AntdApp.useApp()
  const { logs, clearLogs, accounts } = useStore()

  const [account, setAccount] = useState<string>('__all__')
  const [levels, setLevels] = useState<string[]>([])
  const [keyword, setKeyword] = useState('')
  const [autoScroll, setAutoScroll] = useState(true)
  const [exportPath, setExportPath] = useState('')

  const boxRef = useRef<HTMLDivElement | null>(null)

  const accountOptions = useMemo(() => {
    const set = new Set<string>()
    accounts.forEach((a) => a.label && set.add(a.label))
    logs.forEach((l) => l.account && set.add(l.account))
    return [
      { value: '__all__', label: '全部账号' },
      { value: '__global__', label: '(全局日志)' },
      ...Array.from(set)
        .sort()
        .map((v) => ({ value: v, label: v })),
    ]
  }, [accounts, logs])

  const filtered = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    const out = logs.filter((l) => {
      if (account === '__global__') {
        if (l.account) return false
      } else if (account !== '__all__' && l.account !== account) {
        return false
      }
      if (levels.length && !levels.includes(l.level)) return false
      if (kw && !`${l.msg} ${l.account}`.toLowerCase().includes(kw)) return false
      return true
    })
    return out.length > MAX_RENDER ? out.slice(out.length - MAX_RENDER) : out
  }, [logs, account, levels, keyword])

  // 自动滚动到底部
  useEffect(() => {
    if (!autoScroll) return
    const el = boxRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [filtered.length, autoScroll])

  async function setLevel(level: string) {
    try {
      await bindings.setLogLevel(level)
      message.success(`日志级别已设置为 ${level}`)
    } catch (e) {
      message.error(errText(e))
    }
  }

  async function exportResults() {
    try {
      const p = await bindings.exportResults()
      if (p) {
        setExportPath(p)
        message.success(`运行结果已导出: ${p}`)
      } else {
        message.info('导出完成 (未返回路径)')
      }
    } catch (e) {
      message.error(`导出失败: ${errText(e)}`)
    }
  }

  const counts = useMemo(() => {
    const m: Record<string, number> = {}
    filtered.forEach((l) => {
      m[l.level] = (m[l.level] ?? 0) + 1
    })
    return m
  }, [filtered])

  return (
    <div>
      <Card className="cx-card" size="small" title="日志筛选">
        <div className="cx-toolbar">
          <span className="cx-hint">账号</span>
          <Select
            style={{ width: 180 }}
            value={account}
            onChange={setAccount}
            options={accountOptions}
            showSearch
            optionFilterProp="label"
          />
          <span className="cx-hint">级别</span>
          <Select
            mode="multiple"
            allowClear
            style={{ minWidth: 220 }}
            placeholder="全部级别"
            value={levels}
            onChange={setLevels}
            maxTagCount="responsive"
            options={LOG_LEVELS.map((l) => ({ value: l, label: LOG_LEVEL_LABEL[l] }))}
          />
          <Input.Search
            style={{ width: 260 }}
            allowClear
            placeholder="关键词搜索 (消息/账号)"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
          />
          <div className="cx-toolbar-spacer" />
          <Space size={6}>
            <span className="cx-hint">输出级别</span>
            <Select
              size="small"
              style={{ width: 110 }}
              defaultValue="info"
              onChange={setLevel}
              options={LOG_LEVELS.map((l) => ({ value: l, label: LOG_LEVEL_LABEL[l] }))}
            />
          </Space>
        </div>

        <div className="cx-toolbar" style={{ marginBottom: 0 }}>
          <Checkbox checked={autoScroll} onChange={(e) => setAutoScroll(e.target.checked)}>
            自动滚动
          </Checkbox>
          <Button
            size="small"
            icon={<VerticalAlignBottomOutlined />}
            onClick={() => {
              const el = boxRef.current
              if (el) el.scrollTop = el.scrollHeight
            }}
          >
            滚到底部
          </Button>
          <Button
            size="small"
            icon={<ExportOutlined />}
            onClick={exportResults}
            type="primary"
            ghost
          >
            导出结果
          </Button>
          <Popconfirm title="清空当前显示的日志?" okText="清空" cancelText="取消" onConfirm={clearLogs}>
            <Button size="small" danger icon={<ClearOutlined />}>
              清空日志
            </Button>
          </Popconfirm>
          <div className="cx-toolbar-spacer" />
          <Space size={8} wrap>
            <span className="cx-hint">
              共 {logs.length} 条 / 显示 {filtered.length} 条
            </span>
            {LOG_LEVELS.map((l) => (
              <Tooltip key={l} title={`${LOG_LEVEL_LABEL[l]} 级别条数`}>
                <Tag color={LOG_LEVEL_COLOR[l]} style={{ color: '#fff', marginInlineEnd: 0 }}>
                  {LOG_LEVEL_LABEL[l]} {counts[l] ?? 0}
                </Tag>
              </Tooltip>
            ))}
          </Space>
        </div>
      </Card>

      {exportPath && (
        <Alert
          className="cx-card"
          type="success"
          showIcon
          closable
          onClose={() => setExportPath('')}
          message="运行结果已导出"
          description={<span className="cx-mono">{exportPath}</span>}
        />
      )}

      <Card
        className="cx-card"
        size="small"
        title={
          <Space size={6}>
            <FilterOutlined />
            <span>实时日志</span>
            {autoScroll && <Tag color="processing">自动滚动</Tag>}
          </Space>
        }
        styles={{ body: { padding: 0 } }}
      >
        <div className="cx-log-panel" ref={boxRef}>
          {filtered.length === 0 ? (
            <div className="cx-log-empty">
              暂无日志 — 日志会通过 Wails 的 log 事件实时推送过来
            </div>
          ) : (
            filtered.map((l, i) => (
              <div key={`${l.time}-${i}`} className={`cx-log-line cx-lv-${l.level}`}>
                <span className="cx-log-time">{formatClock(l.time)}</span>
                <span className="cx-log-level">{LOG_LEVEL_LABEL[l.level] ?? l.level}</span>
                <span className="cx-log-account">{l.account ? `[${l.account}]` : ''}</span>
                <span className="cx-log-msg">{l.msg}</span>
              </div>
            ))
          )}
        </div>
      </Card>
    </div>
  )
}

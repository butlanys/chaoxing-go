import { useMemo, useState } from 'react'
import {
  Alert,
  App as AntdApp,
  Badge,
  Button,
  Card,
  Descriptions,
  Empty,
  Progress,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  PlayCircleOutlined,
  ReloadOutlined,
  StopOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { bindings, errText } from '../bindings'
import { useStore } from '../store'
import { Progress as ProgressRow, RunRequest, STATE_META } from '../types'
import { isFinalState } from '../utils'

export default function RunPage() {
  const { message } = AntdApp.useApp()
  const {
    accounts,
    config,
    selectedAccounts,
    setSelectedAccounts,
    selectedCourses,
    statuses,
    setStatuses,
    running,
    setRunning,
  } = useStore()

  const [starting, setStarting] = useState(false)

  const enabledSelected = useMemo(
    () => accounts.filter((a) => selectedAccounts.includes(a.id)),
    [accounts, selectedAccounts],
  )

  const aggregate = useMemo(() => {
    const total = statuses.reduce((n, p) => n + (p.total || 0), 0)
    const done = statuses.reduce((n, p) => n + (p.done || 0), 0)
    const failed = statuses.reduce((n, p) => n + (p.failed || 0), 0)
    const finished = statuses.filter((p) => isFinalState(p.state)).length
    return { total, done, failed, finished }
  }, [statuses])

  function buildRequest(): RunRequest | null {
    const ids = selectedAccounts.filter((id) => accounts.some((a) => a.id === id))
    if (!ids.length) {
      message.warning('请先选择要执行的账号')
      return null
    }
    const courseIds: Record<string, string[]> = {}
    ids.forEach((id) => {
      courseIds[id] = selectedCourses[id] ?? []
    })
    return {
      account_ids: ids,
      course_ids: courseIds,
      speed: config.Common.Speed,
      jobs: config.Common.Jobs,
      notopen_action: config.Common.NotOpenAction,
      retry_interval: config.Common.RetryInterval,
      add_learning_count: config.Common.AddLearningCount,
      target_count: config.Common.TargetCount,
    }
  }

  async function start() {
    const req = buildRequest()
    if (!req) return
    setStarting(true)
    try {
      await bindings.startRun(req)
      setRunning(true)
      setStatuses([])
      message.success(`已启动刷课任务 (${req.account_ids.length} 个账号)`)
      const st = await bindings.getStatuses().catch(() => [])
      if (st?.length) setStatuses(st)
    } catch (e) {
      message.error(`启动失败: ${errText(e)}`)
    } finally {
      setStarting(false)
    }
  }

  async function stop() {
    try {
      await bindings.stopRun()
      setRunning(false)
      message.warning('已请求停止所有任务')
      const st = await bindings.getStatuses().catch(() => [])
      if (st?.length) setStatuses(st)
    } catch (e) {
      message.error(`停止失败: ${errText(e)}`)
    }
  }

  async function refresh() {
    try {
      const [st, run] = await Promise.all([
        bindings.getStatuses().catch(() => []),
        bindings.isRunning().catch(() => false),
      ])
      if (st) setStatuses(st)
      setRunning(!!run)
      message.success('状态已刷新')
    } catch (e) {
      message.error(errText(e))
    }
  }

  const columns: ColumnsType<ProgressRow> = [
    {
      title: '账号',
      dataIndex: 'account',
      width: 140,
      render: (v: string) => <span style={{ fontWeight: 500 }}>{v || '—'}</span>,
    },
    { title: '课程', dataIndex: 'course', width: 220, ellipsis: true, render: (v: string) => v || '—' },
    { title: '当前章节', dataIndex: 'chapter', width: 160, ellipsis: true, render: (v: string) => v || '—' },
    {
      title: '进度',
      key: 'progress',
      width: 220,
      render: (_, r) => {
        const total = r.total || 0
        const done = r.done || 0
        const percent = total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0
        const status =
          r.state === 'error' ? 'exception' : r.state === 'done' ? 'success' : 'active'
        return (
          <Space direction="vertical" size={0} style={{ width: '100%' }}>
            <Progress percent={percent} size="small" status={status} showInfo={false} />
            <span className="cx-hint cx-mono">
              {done}/{total}
            </span>
          </Space>
        )
      },
    },
    {
      title: '失败',
      dataIndex: 'failed',
      width: 70,
      align: 'center',
      render: (v: number) => (v > 0 ? <Tag color="red">{v}</Tag> : <span className="cx-hint">0</span>),
    },
    {
      title: '状态',
      dataIndex: 'state',
      width: 100,
      render: (v: string) => {
        const meta = STATE_META[v] ?? { text: v || '未知', color: 'default' }
        return <Tag color={meta.color}>{meta.text}</Tag>
      },
    },
  ]

  return (
    <div>
      <Card
        className="cx-card"
        size="small"
        title="任务控制"
        extra={
          <Badge
            status={running ? 'processing' : 'default'}
            text={running ? '运行中' : '空闲'}
          />
        }
      >
        <Space wrap style={{ marginBottom: 12 }}>
          <span className="cx-hint">执行账号</span>
          <Select
            mode="multiple"
            allowClear
            style={{ minWidth: 320 }}
            placeholder="选择要刷课的账号 (与账号管理页勾选同步)"
            value={selectedAccounts}
            onChange={(v) => setSelectedAccounts(v)}
            options={accounts.map((a) => ({
              value: a.id,
              label: `${a.label} (${a.username || '无手机号'})${a.enabled ? '' : ' · 已禁用'}`,
            }))}
            maxTagCount="responsive"
          />
          <Button
            size="small"
            onClick={() => setSelectedAccounts(accounts.filter((a) => a.enabled).map((a) => a.id))}
          >
            选择全部启用账号
          </Button>
          <Button size="small" onClick={() => setSelectedAccounts([])}>
            清空
          </Button>
        </Space>

        <Space wrap>
          <Button
            type="primary"
            icon={<PlayCircleOutlined />}
            loading={starting}
            disabled={running}
            onClick={start}
          >
            开始刷课
          </Button>
          <Button danger icon={<StopOutlined />} disabled={!running} onClick={stop}>
            停止
          </Button>
          <Button icon={<ReloadOutlined />} onClick={refresh}>
            刷新状态
          </Button>
          <Button
            icon={<ThunderboltOutlined />}
            onClick={async () => {
              try {
                const p = await bindings.exportResults()
                message.success(p ? `运行结果已导出: ${p}` : '导出完成')
              } catch (e) {
                message.error(`导出失败: ${errText(e)}`)
              }
            }}
          >
            导出结果
          </Button>
        </Space>

        {!selectedAccounts.length && (
          <Alert
            style={{ marginTop: 12 }}
            type="info"
            showIcon
            message="尚未选择账号"
            description="请在“账号管理”页勾选账号, 或在上方下拉框中选择; 未在“课程选择”页勾选课程时, 默认学习该账号的全部课程。"
          />
        )}

        <div style={{ marginTop: 14 }}>
          <Descriptions size="small" column={4} bordered>
            <Descriptions.Item label="倍速">{config.Common.Speed}x</Descriptions.Item>
            <Descriptions.Item label="并发章节">{config.Common.Jobs}</Descriptions.Item>
            <Descriptions.Item label="关闭章节动作">
              {config.Common.NotOpenAction}
            </Descriptions.Item>
            <Descriptions.Item label="重试间隔">{config.Common.RetryInterval}s</Descriptions.Item>
            <Descriptions.Item label="增加学习次数">
              {config.Common.AddLearningCount ? '开启' : '关闭'}
            </Descriptions.Item>
            <Descriptions.Item label="目标次数">{config.Common.TargetCount}</Descriptions.Item>
            <Descriptions.Item label="选中账号">{enabledSelected.length}</Descriptions.Item>
            <Descriptions.Item label="任务参数来源">任务与配置页</Descriptions.Item>
          </Descriptions>
        </div>

        {!!enabledSelected.length && (
          <div style={{ marginTop: 10 }}>
            <Space wrap size={[6, 6]}>
              {enabledSelected.map((a) => {
                const n = (selectedCourses[a.id] ?? []).length
                return (
                  <Tag key={a.id} color="blue">
                    {a.label}: {n > 0 ? `${n} 门课程` : '全部课程'}
                  </Tag>
                )
              })}
            </Space>
          </div>
        )}
      </Card>

      <Card className="cx-card" size="small" title="总体进度">
        <Space size={48} wrap>
          <Statistic title="课程 / 任务总数" value={statuses.length} />
          <Statistic title="已完成章节" value={aggregate.done} suffix={`/ ${aggregate.total}`} />
          <Statistic
            title="失败章节"
            value={aggregate.failed}
            valueStyle={{ color: aggregate.failed ? '#cf1322' : undefined }}
          />
          <Statistic title="已结束任务" value={aggregate.finished} suffix={`/ ${statuses.length}`} />
          <Statistic
            title="整体完成度"
            value={aggregate.total > 0 ? Math.round((aggregate.done / aggregate.total) * 100) : 0}
            suffix="%"
          />
        </Space>
      </Card>

      <Card className="cx-card" size="small" title="各账号进度">
        {statuses.length === 0 ? (
          <Empty
            description={
              <span className="cx-hint">
                暂无进度数据 — 点击“开始刷课”后这里会实时显示每个账号/课程的进度
              </span>
            }
          />
        ) : (
          <Table<ProgressRow>
            className="cx-table"
            rowKey={(r) => `${r.account}||${r.course}`}
            size="small"
            dataSource={statuses}
            columns={columns}
            pagination={{ pageSize: 15, size: 'small', showSizeChanger: true }}
            scroll={{ x: 1000 }}
            rowClassName={(r) =>
              r.state === 'error' ? 'cx-row-error' : isFinalState(r.state) ? 'cx-row-done' : ''
            }
          />
        )}
      </Card>
    </div>
  )
}

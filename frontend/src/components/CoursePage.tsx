import { useEffect, useMemo, useState } from 'react'
import {
  App as AntdApp,
  Button,
  Card,
  Checkbox,
  Empty,
  Segmented,
  Select,
  Space,
  Table,
  Tabs,
  Tag,
  Tooltip,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import {
  AppstoreOutlined,
  CloudDownloadOutlined,
  TableOutlined,
} from '@ant-design/icons'
import { bindings, errText } from '../bindings'
import { useStore } from '../store'
import { Course } from '../types'
import { courseKeyOf } from '../utils'

type ViewMode = 'table' | 'card'

export default function CoursePage() {
  const { message } = AntdApp.useApp()
  const { accounts, courses, setCourses, selectedCourses, setAccountCourses } = useStore()

  const [accountId, setAccountId] = useState<string>('')
  const [activeKey, setActiveKey] = useState<string>('')
  const [loadingId, setLoadingId] = useState<string | null>(null)
  const [view, setView] = useState<ViewMode>('table')

  useEffect(() => {
    if (!accountId && accounts.length) setAccountId(accounts[0].id)
  }, [accounts, accountId])

  // 已有缓存的账号默认展开第一个
  useEffect(() => {
    if (activeKey) return
    const first = accounts.find((a) => (courses[a.id] ?? []).length)
    if (first) setActiveKey(first.id)
  }, [accounts, courses, activeKey])

  const loadedAccounts = useMemo(
    () => accounts.filter((a) => (courses[a.id] ?? []).length > 0),
    [accounts, courses],
  )

  const activeCourses: Course[] = activeKey ? courses[activeKey] ?? [] : []
  const activeSelected = activeKey ? selectedCourses[activeKey] ?? [] : []
  const activeKeys = activeCourses.map((c) => courseKeyOf(c.courseId, c.clazzId))

  async function fetchFor(id: string) {
    if (!id) {
      message.info('请先选择账号')
      return
    }
    setLoadingId(id)
    try {
      const list = await bindings.fetchCourses(id)
      setCourses(id, list ?? [])
      setActiveKey(id)
      if (!list || !list.length) {
        message.warning('该账号没有拉取到课程 (可能是登录失败或本学期无课)')
      } else {
        message.success(`已拉取 ${list.length} 门课程`)
        // 首次拉取时默认全选, 方便直接开跑
        if (!(selectedCourses[id] ?? []).length) {
          setAccountCourses(
            id,
            list.map((c) => courseKeyOf(c.courseId, c.clazzId)),
          )
        }
      }
    } catch (e) {
      message.error(`拉取课程失败: ${errText(e)}`)
    } finally {
      setLoadingId(null)
    }
  }

  function toggleOne(course: Course, checked: boolean) {
    if (!activeKey) return
    const key = courseKeyOf(course.courseId, course.clazzId)
    const cur = new Set(activeSelected)
    if (checked) cur.add(key)
    else cur.delete(key)
    setAccountCourses(activeKey, Array.from(cur))
  }

  function selectAll(all: boolean) {
    if (!activeKey) return
    setAccountCourses(activeKey, all ? activeKeys : [])
  }

  const columns: ColumnsType<Course> = [
    {
      title: '课程名',
      dataIndex: 'title',
      width: 260,
      render: (v: string, r) => (
        <Space size={6}>
          <span style={{ fontWeight: 500 }}>{v || r.courseId}</span>
          {r.info && <Tag color="blue">{r.info}</Tag>}
        </Space>
      ),
    },
    { title: '教师', dataIndex: 'teacher', width: 110, render: (v: string) => v || '—' },
    {
      title: '班级ID (clazzId)',
      dataIndex: 'clazzId',
      width: 140,
      render: (v: string) => <span className="cx-mono">{v}</span>,
    },
    {
      title: '课程ID (courseId)',
      dataIndex: 'courseId',
      width: 150,
      render: (v: string) => <span className="cx-mono">{v}</span>,
    },
    {
      title: '描述',
      dataIndex: 'desc',
      ellipsis: true,
      render: (v: string) => (v ? <Tooltip title={v}>{v}</Tooltip> : <span className="cx-hint">—</span>),
    },
  ]

  const totalSelected = useMemo(
    () =>
      Object.values(selectedCourses).reduce((n, list) => n + (list?.length ?? 0), 0),
    [selectedCourses],
  )

  return (
    <div>
      <Card className="cx-card" size="small">
        <div className="cx-toolbar" style={{ marginBottom: 0 }}>
          <span className="cx-hint">选择账号</span>
          <Select
            style={{ width: 260 }}
            value={accountId || undefined}
            placeholder="请选择账号"
            onChange={setAccountId}
            options={accounts.map((a) => ({
              value: a.id,
              label: `${a.label} (${a.username || '无手机号'})`,
            }))}
          />
          <Button
            type="primary"
            icon={<CloudDownloadOutlined />}
            loading={!!loadingId && loadingId === accountId}
            onClick={() => fetchFor(accountId)}
          >
            拉取课程
          </Button>
          <div className="cx-toolbar-spacer" />
          <Segmented
            value={view}
            onChange={(v) => setView(v as ViewMode)}
            options={[
              { value: 'table', icon: <TableOutlined />, label: '表格' },
              { value: 'card', icon: <AppstoreOutlined />, label: '卡片' },
            ]}
          />
        </div>
      </Card>

      <Card
        className="cx-card"
        size="small"
        title="课程列表"
        extra={<span className="cx-hint">已勾选课程总数: {totalSelected}</span>}
      >
        {loadedAccounts.length === 0 ? (
          <Empty
            description={
              <span className="cx-hint">
                还没有课程数据 — 请在上方选择账号后点击“拉取课程”
              </span>
            }
          />
        ) : (
          <Tabs
            activeKey={activeKey || loadedAccounts[0].id}
            onChange={setActiveKey}
            type="card"
            items={loadedAccounts.map((a) => {
              const list = courses[a.id] ?? []
              const sel = selectedCourses[a.id] ?? []
              const keys = list.map((c) => courseKeyOf(c.courseId, c.clazzId))
              const allChecked = keys.length > 0 && keys.every((k) => sel.includes(k))
              const someChecked = sel.length > 0 && !allChecked
              return {
                key: a.id,
                label: (
                  <Space size={6}>
                    <span>{a.label}</span>
                    <Tag color={sel.length ? 'blue' : 'default'}>
                      {sel.length}/{list.length}
                    </Tag>
                  </Space>
                ),
                children: (
                  <div>
                    <div className="cx-toolbar">
                      <Checkbox
                        checked={allChecked}
                        indeterminate={someChecked}
                        onChange={(e) => selectAll(e.target.checked)}
                      >
                        全选
                      </Checkbox>
                      <Button size="small" onClick={() => selectAll(true)}>
                        全选本页账号课程
                      </Button>
                      <Button size="small" onClick={() => selectAll(false)}>
                        清空
                      </Button>
                      <Button
                        size="small"
                        loading={loadingId === a.id}
                        onClick={() => fetchFor(a.id)}
                      >
                        重新拉取
                      </Button>
                      <div className="cx-toolbar-spacer" />
                      <span className="cx-hint">
                        该账号已选 {sel.length} / {list.length} 门
                      </span>
                    </div>

                    {view === 'table' ? (
                      <Table<Course>
                        className="cx-table"
                        rowKey={(r) => courseKeyOf(r.courseId, r.clazzId)}
                        size="small"
                        dataSource={list}
                        columns={columns}
                        pagination={{ pageSize: 10, size: 'small', showSizeChanger: true }}
                        scroll={{ x: 1000 }}
                        rowSelection={{
                          selectedRowKeys: sel,
                          onChange: (ks) => setAccountCourses(a.id, ks as string[]),
                        }}
                      />
                    ) : (
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
                          gap: 10,
                        }}
                      >
                        {list.map((c) => {
                          const key = courseKeyOf(c.courseId, c.clazzId)
                          const checked = sel.includes(key)
                          return (
                            <div
                              key={key}
                              className={`cx-course-card${checked ? ' is-selected' : ''}`}
                              onClick={() => toggleOne(c, !checked)}
                            >
                              <div className="cx-course-title">
                                <Checkbox checked={checked} onClick={(e) => e.stopPropagation()} onChange={(e) => toggleOne(c, e.target.checked)} />
                                <span>{c.title || c.courseId}</span>
                              </div>
                              <div className="cx-course-meta">
                                <div>教师: {c.teacher || '未知'}</div>
                                <div className="cx-mono">班级: {c.clazzId}</div>
                                <div className="cx-mono">课程: {c.courseId}</div>
                                {c.desc && <div>描述: {c.desc}</div>}
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                ),
              }
            })}
          />
        )}
      </Card>
    </div>
  )
}

import React, { useMemo, useState } from 'react'
import { Badge, Layout, Menu, Tag, Tooltip } from 'antd'
import {
  BookOutlined,
  FileTextOutlined,
  PlayCircleOutlined,
  SettingOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from '@ant-design/icons'
import AccountPage from './components/AccountPage'
import CoursePage from './components/CoursePage'
import ConfigPage from './components/ConfigPage'
import RunPage from './components/RunPage'
import LogPage from './components/LogPage'
import ErrorBoundary from './components/ErrorBoundary'
import { useStore } from './store'

const { Header, Sider, Content } = Layout

type PageKey = 'accounts' | 'courses' | 'config' | 'run' | 'logs'

interface PageMeta {
  key: PageKey
  label: string
  icon: React.ReactNode
  title: string
  desc: string
}

const PAGES: PageMeta[] = [
  {
    key: 'accounts',
    label: '账号管理',
    icon: <UserOutlined />,
    title: '账号管理',
    desc: '维护学习通账号、登录测试与课程授权',
  },
  {
    key: 'courses',
    label: '课程选择',
    icon: <BookOutlined />,
    title: '课程选择',
    desc: '按账号拉取课程列表并勾选要学习的课程',
  },
  {
    key: 'config',
    label: '任务与配置',
    icon: <SettingOutlined />,
    title: '任务与配置',
    desc: '运行参数、题库、通知与配置文件导入导出',
  },
  {
    key: 'run',
    label: '任务执行',
    icon: <PlayCircleOutlined />,
    title: '任务执行',
    desc: '启动 / 停止多账号并发刷课并查看实时进度',
  },
  {
    key: 'logs',
    label: '实时日志',
    icon: <FileTextOutlined />,
    title: '实时日志',
    desc: '查看运行日志、按账号与级别筛选、导出结果',
  },
]

export default function App() {
  const [page, setPage] = useState<PageKey>('accounts')
  const [collapsed, setCollapsed] = useState(false)
  const { running, mockMode, appInfo } = useStore()

  const meta = useMemo(() => PAGES.find((p) => p.key === page) ?? PAGES[0], [page])

  return (
    <Layout className="cx-layout">
      <Sider
        className="cx-sider"
        width={196}
        collapsible
        collapsed={collapsed}
        onCollapse={setCollapsed}
        theme="dark"
      >
        <div className="cx-logo">
          <span className="cx-logo-badge">
            <ThunderboltOutlined />
          </span>
          {!collapsed && (
            <span>
              超星助手
              <span className="cx-logo-sub">chaoxing-go</span>
            </span>
          )}
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[page]}
          onClick={(e) => setPage(e.key as PageKey)}
          items={PAGES.map((p) => ({ key: p.key, icon: p.icon, label: p.label }))}
        />
        <div className="cx-sider-foot">
          {collapsed ? `v${appInfo?.version ?? '1.1.1'}` : `版本 ${appInfo?.version ?? '1.1.1'} · GPL-3.0`}
        </div>
      </Sider>

      <Layout>
        <Header className="cx-header">
          <div>
            <span className="cx-header-title">{meta.title}</span>
            <span className="cx-header-desc">{meta.desc}</span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            {mockMode && (
              <Tooltip title="未检测到 Wails 后端, 当前使用内置 mock 数据 (npm run dev 预览模式)">
                <Tag color="orange">Mock 预览</Tag>
              </Tooltip>
            )}
            <Tooltip title={running ? '任务正在执行' : '当前没有任务在运行'}>
              <Badge
                status={running ? 'processing' : 'default'}
                text={running ? '运行中' : '空闲'}
                style={{ fontSize: 12 }}
              />
            </Tooltip>
          </div>
        </Header>

        <Content className="cx-content">
          {/* 每个页面独立包一层边界: 单页渲染异常不再拖垮整个窗口变成白页 */}
          <ErrorBoundary key={page} area={meta.title}>
            {page === 'accounts' && <AccountPage />}
            {page === 'courses' && <CoursePage />}
            {page === 'config' && <ConfigPage />}
            {page === 'run' && <RunPage />}
            {page === 'logs' && <LogPage />}
          </ErrorBoundary>
        </Content>
      </Layout>
    </Layout>
  )
}

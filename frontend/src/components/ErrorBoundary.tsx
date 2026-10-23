import React from 'react'
import { Alert, Button, Space, Typography } from 'antd'

interface Props {
  /** 出错时展示的区域名, 例如“任务与配置” */
  area: string
  children: React.ReactNode
}

interface State {
  error: Error | null
}

/**
 * 页面级错误边界。
 *
 * 为什么需要: React 18 中渲染期抛出的异常如果没有边界接管, 会一路冒泡到根节点,
 * 导致整棵组件树被卸载 —— 表现就是"整个窗口变成一张白页", 连侧边栏都消失,
 * 用户拿不到任何线索。这里把异常限制在当前页内部, 并直接显示错误信息,
 * 便于定位 (例如后端返回体与前端类型不一致导致的 undefined 解引用)。
 */
export default class ErrorBoundary extends React.Component<Props, State> {
  override state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  override componentDidCatch(error: Error, info: React.ErrorInfo) {
    // 同时打到控制台, 方便在 Wails 开发者工具里看到完整堆栈
    console.error(`[${this.props.area}] 渲染异常:`, error, info.componentStack)
  }

  override render() {
    const { error } = this.state
    if (!error) return this.props.children
    return (
      <Alert
        type="error"
        showIcon
        message={`“${this.props.area}”页面渲染失败`}
        description={
          <Space direction="vertical" size={6} style={{ width: '100%' }}>
            <Typography.Text code copyable style={{ fontSize: 12 }}>
              {error.message || String(error)}
            </Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              该异常已被页面级边界拦截, 应用其余部分仍可正常使用。请把上面的错误信息反馈给开发者。
            </Typography.Text>
            <Button size="small" onClick={() => this.setState({ error: null })}>
              重试渲染
            </Button>
          </Space>
        }
      />
    )
  }
}

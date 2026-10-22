import React from 'react'
import ReactDOM from 'react-dom/client'
import { ConfigProvider, App as AntdApp, theme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import App from './App'
import { StoreProvider } from './store'
import { isMockMode } from './bindings'
import { seedMockLogs } from './mock'
import './styles.css'

// 浏览器预览 (无 Wails 后端) 时预置几条演示日志
if (isMockMode()) {
  seedMockLogs()
}

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: {
          fontSize: 13,
          borderRadius: 4,
          colorPrimary: '#1677ff',
          controlHeight: 30,
        },
        components: {
          Table: { cellPaddingBlockSM: 6 },
          Menu: { itemHeight: 42, itemMarginInline: 0 },
        },
      }}
    >
      <AntdApp>
        <StoreProvider>
          <App />
        </StoreProvider>
      </AntdApp>
    </ConfigProvider>
  </React.StrictMode>,
)

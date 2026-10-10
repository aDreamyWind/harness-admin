import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ConfigProvider } from 'antd'
import { BrowserRouter, useLocation } from 'react-router-dom'
import zhCN from 'antd/locale/zh_CN'

import { App } from './App'
import { PlatformApp } from './PlatformApp'
import { AuthProvider } from './auth/AuthContext'
import { PlatformAuthProvider } from './auth/PlatformAuthContext'
import './styles.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: '#2457d6',
          colorSuccess: '#14856f',
          colorWarning: '#b85e2e',
          colorText: '#142033',
          colorTextSecondary: '#617086',
          colorBgLayout: '#edf3f7',
          colorBorderSecondary: '#dbe4ec',
          borderRadius: 10,
          fontFamily:
            '"Avenir Next", "SF Pro Text", "PingFang SC", "Microsoft YaHei", sans-serif',
        },
        components: {
          Card: { paddingLG: 22 },
          Layout: { siderBg: '#111b2d', headerBg: '#edf3f7' },
          Menu: {
            darkItemBg: '#111b2d',
            darkItemSelectedBg: '#2457d6',
            itemBorderRadius: 8,
          },
        },
      }}
    >
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <ApplicationRoot />
        </BrowserRouter>
      </QueryClientProvider>
    </ConfigProvider>
  </StrictMode>,
)

function ApplicationRoot() {
  const location = useLocation()
  if (location.pathname.startsWith('/platform')) {
    return <PlatformAuthProvider><PlatformApp /></PlatformAuthProvider>
  }
  return <AuthProvider><App /></AuthProvider>
}

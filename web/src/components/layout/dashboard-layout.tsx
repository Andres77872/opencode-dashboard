/* Vael app shell — full-height flex: Sidebar rail + (TopBar / FilterBar / PageBody).
   The in-page scroll lives in PageBody so the chrome stays fixed. */
import { Outlet, useLocation } from 'react-router-dom'
import { Sidebar } from './sidebar'
import { TopBar } from './topbar'
import { FilterBar } from './filter-bar'
import { SidebarProvider } from './sidebar-context'
import { ViewErrorBoundary } from './error-boundary'
import { SourceNotice } from '../source/source-notice'

export function DashboardLayout() {
  const { pathname } = useLocation()
  return (
    <SidebarProvider>
      <div style={{ display: 'flex', height: '100vh', overflow: 'hidden', background: 'var(--ink-900)' }}>
        <Sidebar />
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          <TopBar />
          <FilterBar />
          <div style={{ flex: 1, overflowY: 'auto', minHeight: 0 }}>
            <div style={{ maxWidth: 'var(--content-max)', margin: '0 auto', padding: '20px 24px 40px' }}>
              <SourceNotice />
              <ViewErrorBoundary key={pathname}>
                <Outlet />
              </ViewErrorBoundary>
            </div>
          </div>
        </div>
      </div>
    </SidebarProvider>
  )
}

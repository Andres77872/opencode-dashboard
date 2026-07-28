/* Vael app shell — full-height flex: Sidebar rail + (TopBar / FilterBar / PageBody).
   The in-page scroll lives in PageBody so the chrome stays fixed. */
import { Suspense, lazy } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { Sidebar } from './sidebar'
import { TopBar } from './topbar'
import { FilterBar } from './filter-bar'
import { SidebarProvider } from './sidebar-provider'
import { ViewErrorBoundary } from './error-boundary'
import { SourceNotice } from '../source/source-notice'
import { ViewFallback } from './view-fallback'

// The assistant brings a markdown renderer, a syntax highlighter and its
// streaming client, none of which the dashboard itself needs. It is a docked
// panel that starts closed, so it loads alongside the first view rather than
// ahead of it; until then the launcher is simply absent.
const AnalyticsAssistant = lazy(() =>
  import('../assistant/analytics-assistant').then((m) => ({ default: m.AnalyticsAssistant })),
)

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
                {/* Keyed by route so switching views shows the fallback rather
                    than holding the previous view's content on screen. */}
                <Suspense key={pathname} fallback={<ViewFallback />}>
                  <Outlet />
                </Suspense>
              </ViewErrorBoundary>
            </div>
          </div>
        </div>
      </div>
      <Suspense fallback={null}>
        <AnalyticsAssistant />
      </Suspense>
    </SidebarProvider>
  )
}

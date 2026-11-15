/* Vael app shell — full-height flex: Sidebar rail + (TopBar / FilterBar / PageBody).
   The in-page scroll lives in PageBody so the chrome stays fixed. */
import { Suspense, lazy, useEffect, useRef } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import { Sidebar } from './sidebar'
import { TopBar } from './topbar'
import { FilterBar } from './filter-bar'
import { SidebarProvider } from './sidebar-provider'
import { ViewErrorBoundary } from './error-boundary'
import { SourceBlocked, SourceNotice } from '../source/source-notice'
import { getSourceBlock } from '../source/source-block'
import { ViewFallback } from './view-fallback'
import { useDashboardContext } from './dashboard-context'

// The assistant brings a markdown renderer, a syntax highlighter and its
// streaming client, none of which the dashboard itself needs. It is a docked
// panel that starts closed, so it loads alongside the first view rather than
// ahead of it; until then the launcher is simply absent.
const AnalyticsAssistant = lazy(() =>
  import('../assistant/analytics-assistant').then((m) => ({ default: m.AnalyticsAssistant })),
)

export function DashboardLayout() {
  const { pathname } = useLocation()
  const context = useDashboardContext()
  // A view with no source it can query is replaced by the reason why. While
  // the source list is still loading the view is rendered (as its skeleton)
  // so its code and data load alongside the metadata, not after it.
  const block = getSourceBlock(context, pathname)

  // The page body is the scroll container and outlives the route, so a new
  // view would otherwise open at the previous view's scroll offset.
  const scrollRef = useRef<HTMLElement>(null)
  useEffect(() => {
    scrollRef.current?.scrollTo({ top: 0 })
  }, [pathname])

  return (
    <SidebarProvider>
      <a href="#main-content" className="vael-skip-link">Skip to content</a>
      <div className="vael-shell">
        <Sidebar />
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
          <TopBar />
          <FilterBar />
          <main ref={scrollRef} id="main-content" tabIndex={-1} style={{ flex: 1, overflowY: 'auto', minHeight: 0, outline: 'none' }}>
            <div className="vael-page">
              <SourceNotice />
              <ViewErrorBoundary key={pathname}>
                {/* Keyed by route so switching views shows the fallback rather
                    than holding the previous view's content on screen. */}
                <Suspense key={pathname} fallback={<ViewFallback />}>
                  {block ? <SourceBlocked block={block} /> : <Outlet />}
                </Suspense>
              </ViewErrorBoundary>
            </div>
          </main>
        </div>
      </div>
      <Suspense fallback={null}>
        <AnalyticsAssistant />
      </Suspense>
    </SidebarProvider>
  )
}

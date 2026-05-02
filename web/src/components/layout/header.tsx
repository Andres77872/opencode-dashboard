import { useEffect, useRef } from 'react'
import { Menu, RefreshCw } from 'lucide-react'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { formatRelativeTime } from '../../lib/format'
import { useDashboardContext } from './dashboard-context'
import { useSidebar } from './sidebar-context'

export function Header() {
  const { lastUpdatedAt, isRefreshing, requestRefresh } = useDashboardContext()
  const { toggleMobile } = useSidebar()
  const headerRef = useRef<HTMLElement>(null)

  // Track header height with ResizeObserver → syncs --header-height CSS var
  // so all sticky children use a consistent, dynamic offset.
  useEffect(() => {
    const el = headerRef.current
    if (!el) return

    const ro = new ResizeObserver(([entry]) => {
      document.documentElement.style.setProperty(
        '--header-height',
        `${entry.contentRect.height}px`,
      )
    })
    ro.observe(el)
    // Capture initial height immediately
    document.documentElement.style.setProperty(
      '--header-height',
      `${el.getBoundingClientRect().height}px`,
    )
    return () => ro.disconnect()
  }, [])

  return (
    <header
      ref={headerRef}
      className="sticky top-0 z-20 border-b border-border/70 bg-background/92 backdrop-blur-xl"
    >
      <div className="flex flex-col gap-4 px-4 py-4 sm:px-6 xl:px-8">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="flex items-start gap-3">
            {/* Mobile hamburger — visible below xl */}
            <Button
              variant="ghost"
              size="icon"
              className="-ml-1.5 mt-0.5 shrink-0 xl:hidden"
              onClick={toggleMobile}
              aria-label="Toggle navigation menu"
            >
              <Menu className="size-5" />
            </Button>
            <div className="min-w-0 space-y-2">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone="accent">opencode-dashboard</Badge>
                <Badge>Local-first</Badge>
                <Badge tone="success">Read-only API</Badge>
              </div>
              <div>
                <h1 className="text-xl font-semibold tracking-tight text-foreground sm:text-2xl">
                  OpenCode analytics dashboard
                </h1>
                <p className="text-sm text-muted-foreground">
                  Dense web shell for serious usage visibility — not marketing fluff.
                </p>
              </div>
            </div>
          </div>

          <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
            <div className="rounded-xl border border-border/70 bg-panel/70 px-3 py-2 text-sm text-muted-foreground">
              <span className="mr-2 font-medium text-foreground">Last sync</span>
              {isRefreshing ? 'Refreshing…' : formatRelativeTime(lastUpdatedAt)}
            </div>
            <Button variant="ghost" onClick={requestRefresh} disabled={isRefreshing}>
              {isRefreshing ? (
                <RefreshCw className="size-4 animate-spin" />
              ) : (
                <RefreshCw className="size-4" />
              )}
              {isRefreshing ? 'Refreshing…' : 'Refresh data'}
            </Button>
          </div>
        </div>
      </div>
    </header>
  )
}

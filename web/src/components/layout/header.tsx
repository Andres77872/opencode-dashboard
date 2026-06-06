import { useEffect, useRef } from 'react'
import { Menu, RefreshCw } from 'lucide-react'
import { Button } from '../ui/button'
import { SourcePicker } from '../source/source-picker'
import { cn } from '../../lib/utils'
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
      className="sticky top-0 z-20 border-b border-border/50 bg-background/85 backdrop-blur-xl"
    >
      <div className="flex h-14 items-center justify-between px-6 xl:px-8">
        {/* Left: hamburger on mobile */}
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="icon"
            className="-ml-2 shrink-0 xl:hidden"
            onClick={toggleMobile}
            aria-label="Toggle navigation menu"
          >
            <Menu className="size-5" />
          </Button>
        </div>

        {/* Right: source + sync status + refresh */}
        <div className="flex items-center gap-3 sm:gap-4">
          <SourcePicker />
          <span className="text-sm text-muted-foreground">
            <span className="hidden sm:inline">Last sync </span>
            {isRefreshing ? 'Refreshing…' : formatRelativeTime(lastUpdatedAt)}
          </span>
          <Button
            variant="ghost"
            size="icon"
            onClick={requestRefresh}
            disabled={isRefreshing}
            aria-label="Refresh data"
          >
            <RefreshCw className={cn('size-4', isRefreshing && 'animate-spin')} />
          </Button>
        </div>
      </div>
    </header>
  )
}

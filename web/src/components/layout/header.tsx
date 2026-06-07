import { Menu, RefreshCw } from 'lucide-react'
import { Button } from '../ui/button'
import { SourcePicker } from '../source/source-picker'
import { cn } from '../../lib/utils'
import { formatRelativeTime } from '../../lib/format'
import { useDashboardContext } from './dashboard-context'
import { useSidebar } from './sidebar-context'

/**
 * Top bar: nav toggle (mobile) + global source/sync/refresh controls.
 * Stickiness and the --header-height measurement are owned by the layout shell
 * wrapper (dashboard-layout.tsx), which also contains the FilterBar so the
 * combined offset is correct for sticky sidebar cards.
 */
export function Header() {
  const { lastUpdatedAt, isRefreshing, requestRefresh } = useDashboardContext()
  const { toggleMobile } = useSidebar()

  return (
    <header className="border-b border-border/50 bg-background/85 backdrop-blur-xl">
      <div className="mx-auto flex h-14 w-full max-w-7xl items-center justify-between px-6 xl:px-8">
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

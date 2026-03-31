import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { formatRelativeTime } from '../../lib/format'
import { useDashboardContext } from './dashboard-context'

export function Header() {
  const { lastUpdatedAt, isRefreshing, requestRefresh } = useDashboardContext()

  return (
    <header className="sticky top-0 z-20 border-b border-border/70 bg-background/92 backdrop-blur-xl">
      <div className="flex flex-col gap-4 px-4 py-4 sm:px-6 xl:px-8">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div className="space-y-2">
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

          <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
            <div className="rounded-xl border border-border/70 bg-panel/70 px-3 py-2 text-sm text-muted-foreground">
              <span className="mr-2 font-medium text-foreground">Last sync</span>
              {isRefreshing ? 'Refreshing…' : formatRelativeTime(lastUpdatedAt)}
            </div>
            <Button variant="ghost" onClick={requestRefresh} disabled={isRefreshing}>
              {isRefreshing ? 'Refreshing…' : 'Refresh data'}
            </Button>
          </div>
        </div>
      </div>
    </header>
  )
}

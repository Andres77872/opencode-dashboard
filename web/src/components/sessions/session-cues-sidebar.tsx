import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { formatCompactInteger, formatCurrencyWithProvenance } from '../../lib/format'
import type { SessionsSummary } from './sessions-kpi-grid'

interface SessionCuesSidebarProps {
  summary: SessionsSummary
}

export function SessionCuesSidebar({ summary }: SessionCuesSidebarProps) {
  return (
    <Card className="hidden border-border/70 bg-panel/55 2xl:block 2xl:sticky" style={{ top: 'var(--header-height)' }}>
      <CardHeader>
        <CardDescription>Session cues</CardDescription>
        <CardTitle>Read the page faster</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm text-muted-foreground">
        <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Top visible spend</div>
          <div className="mt-2 font-mono text-base text-foreground">
            {summary.hottestSession ? summary.hottestSession.label : 'No data'}
          </div>
          <div className="mt-1 text-sm text-muted-foreground">
            {summary.hottestSession
              ? `${formatCurrencyWithProvenance(summary.hottestSession.cost, summary.hottestSession.cost_status, summary.hottestSession.cost_provenance)} · ${formatCompactInteger(summary.hottestSession.message_count)} messages`
              : 'Awaiting activity'}
          </div>
        </div>

        <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Detail drawer behavior</div>
          <div className="mt-2 text-sm leading-6 text-muted-foreground">
            Open any row to pull live session metadata. The drawer fetches separately, so list browsing stays responsive even if detail hydration fails.
          </div>
        </div>

        <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Backend caveat</div>
          <div className="mt-2 text-sm leading-6 text-muted-foreground">
            Session detail exposes role, time, model, provider, agent, cost, and token metadata. It does <span className="font-semibold text-foreground">not</span> expose message transcript text yet, so this slice deliberately avoids fake conversation rendering.
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

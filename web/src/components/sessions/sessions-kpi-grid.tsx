import { MetricCard } from '../overview/metric-card'
import { formatCompactInteger, formatCurrency, formatInteger } from '../../lib/format'

// ── Session summary type (extracted from sessions-view) ────────

export interface SessionsSummary {
  totalPages: number
  firstVisible: number
  lastVisible: number
  visibleCost: number
  visibleMessages: number
  visibleProjects: number
  hottestSession: { label: string; cost: number; message_count: number } | null
  total: number
  pageSize: number
  page: number
  empty: boolean
}

// ── Props ──────────────────────────────────────────────────────

interface SessionsKpiGridProps {
  summary: SessionsSummary | null
}

// ── Component ──────────────────────────────────────────────────

export function SessionsKpiGrid({ summary }: SessionsKpiGridProps) {
  if (!summary) {
    return null
  }

  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
      <MetricCard
        label="Total sessions"
        value={formatInteger(summary.total)}
        hint={
          summary.empty
            ? 'No sessions recorded in the current database'
            : `${formatInteger(summary.totalPages)} pages at ${formatInteger(summary.pageSize)} rows each`
        }
      />
      <MetricCard
        label="Visible window"
        value={summary.empty ? '0' : `${summary.firstVisible}-${summary.lastVisible}`}
        hint={
          summary.empty
            ? 'Nothing to paginate yet'
            : `Page ${formatInteger(summary.page)} of ${formatInteger(summary.totalPages)}`
        }
      />
      <MetricCard
        label="Visible cost"
        value={formatCurrency(summary.visibleCost)}
        hint={`${formatCompactInteger(summary.visibleMessages)} messages across the current page`}
      />
      <MetricCard
        label="Projects on page"
        value={formatInteger(summary.visibleProjects)}
        hint={
          summary.hottestSession
            ? `${summary.hottestSession.label} is the highest visible spend session`
            : 'Awaiting session activity'
        }
      />
    </div>
  )
}

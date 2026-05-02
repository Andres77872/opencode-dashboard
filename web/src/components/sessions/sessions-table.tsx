import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { Progress } from '../ui/progress'
import { DataTable, type DataTableColumn } from '../common/data-table'
import type { SortState } from '../../lib/table-sort'
import { formatCompactCurrency, formatCompactInteger, formatDateTime, safeDivide } from '../../lib/format'
import type { SessionEntry } from '../../types/api'
import type { SessionsSummary } from './sessions-kpi-grid'

// ── Props ──────────────────────────────────────────────────────

interface SessionsTableProps {
  sessions: SessionEntry[]
  summary: SessionsSummary
  sortState: SortState<string> | null
  onSortChange: (key: string) => void
  onRowClick: (session: SessionEntry) => void
  onTriggerClick: (session: SessionEntry, e: React.MouseEvent) => void
}

// ── Helpers ────────────────────────────────────────────────────

function getSessionLabel(session: SessionEntry) {
  return session.title || 'Untitled session'
}

function getSessionProjectLabel(session: SessionEntry) {
  return session.project_name || session.project_id || 'No linked project'
}

// ── Component ──────────────────────────────────────────────────

export function SessionsTable({
  sessions,
  summary,
  sortState,
  onSortChange,
  onRowClick,
  onTriggerClick,
}: SessionsTableProps) {
  const columns: DataTableColumn<SessionEntry>[] = [
    {
      key: 'session',
      label: 'Session',
      width: 'min-w-[15rem]',
      sortable: false,
      render: (session) => {
        const share = safeDivide(session.cost, summary.visibleCost) * 100
        return (
          <div className="min-w-0 space-y-2">
            <div className="truncate font-medium text-foreground">{getSessionLabel(session)}</div>
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <Badge className="max-w-full px-2 py-0.5 text-[10px] tracking-[0.16em]">{getSessionProjectLabel(session)}</Badge>
              <span className="font-mono">id {session.id.slice(0, 10)}</span>
            </div>
            <div className="flex items-center gap-3">
              <Progress className="h-1.5" value={Math.max(share, session.cost > 0 ? 3 : 0)} />
              <span className="font-mono text-[11px] text-muted-foreground">{Math.round(share || 0)}%</span>
            </div>
          </div>
        )
      },
    },
    {
      key: 'activity',
      label: 'Activity',
      width: 'w-[11rem]',
      sortable: false,
      render: (session) => (
        <div className="space-y-2 text-xs text-muted-foreground">
          <div>
            <div className="uppercase tracking-[0.14em]">Created</div>
            <div className="mt-1 font-mono text-sm text-foreground">{formatDateTime(session.time_created)}</div>
          </div>
          <div>
            <div className="uppercase tracking-[0.14em]">Updated</div>
            <div className="mt-1 font-mono text-sm text-foreground">{formatDateTime(session.time_updated)}</div>
          </div>
        </div>
      ),
    },
    {
      key: 'summary',
      label: 'Summary',
      width: 'w-[9rem]',
      sortable: false,
      render: (session) => (
        <div className="space-y-2 text-xs text-muted-foreground">
          <div>
            <div className="uppercase tracking-[0.14em]">Messages</div>
            <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(session.message_count)}</div>
          </div>
          <div>
            <div className="uppercase tracking-[0.14em]">Cost</div>
            <div className="mt-1 font-mono text-sm text-foreground">{formatCompactCurrency(session.cost)}</div>
          </div>
        </div>
      ),
    },
    {
      key: 'open',
      label: 'Open',
      width: 'w-[5rem]',
      sortable: false,
      render: (session) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={(e) => onTriggerClick(session, e)}
          aria-label={`View details for ${getSessionLabel(session)}`}
          className="w-full justify-center text-accent"
        >
          Open
        </Button>
      ),
    },
  ]

  return (
    <DataTable<SessionEntry>
      rows={sessions}
      columns={columns}
      sortState={sortState}
      onSortChange={onSortChange}
      rowKey={(session) => session.id}
      onRowClick={onRowClick}
      mobileCard={(session) => (
        <button
          type="button"
          onClick={(e) => onTriggerClick(session, e)}
          className="w-full rounded-2xl border border-border/70 bg-panel/65 p-4 text-left transition-colors hover:bg-panel/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          aria-label={`View details for ${getSessionLabel(session)}`}
        >
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <div className="truncate font-medium text-foreground">{getSessionLabel(session)}</div>
              <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span className="uppercase tracking-[0.14em]">{getSessionProjectLabel(session)}</span>
                <span aria-hidden="true">•</span>
                <span className="font-mono">id {session.id.slice(0, 10)}</span>
              </div>
            </div>
            <div className="font-mono text-sm text-foreground">{formatCompactCurrency(session.cost)}</div>
          </div>

          <div className="mt-3 rounded-xl border border-border/60 bg-background/35 px-3 py-2.5 text-xs text-muted-foreground">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
              <span>
                <span className="uppercase tracking-[0.14em]">Messages</span>{' '}
                <span className="font-mono text-foreground">{formatCompactInteger(session.message_count)}</span>
              </span>
              <span>
                <span className="uppercase tracking-[0.14em]">Created</span>{' '}
                <span className="font-mono text-foreground">{formatDateTime(session.time_created)}</span>
              </span>
              <span>
                <span className="uppercase tracking-[0.14em]">Updated</span>{' '}
                <span className="font-mono text-foreground">{formatDateTime(session.time_updated)}</span>
              </span>
            </div>
          </div>

          <div className="mt-3 text-sm font-medium text-accent">Open detail</div>
        </button>
      )}
      emptyState={
        <div className="space-y-3 text-sm text-muted-foreground">
          <p>
            This route stays empty until the database contains session rows. Once activity exists, you get paginated browsing, per-session spend, message counts, and metadata detail.
          </p>
          <p>
            The detail drawer intentionally stays metadata-only because the backend does not expose transcript text in the current session detail payload.
          </p>
        </div>
      }
    />
  )
}

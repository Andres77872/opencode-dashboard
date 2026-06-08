/* Tools — per-source tool usage ranking (Vael). Costs/latency are not part of
   tool data, so those columns are omitted. No fabricated deltas or sparklines:
   the API has no per-tool trend. Source column is only shown if entries carry
   distinct source_id (overview view); a per-source view omits it. */
import { useMemo, useState } from 'react'
import {
  Card,
  StatCard,
  DataTable,
  Badge,
  Skeleton,
  ErrorState,
  Notice,
  type Column,
  type SortSpec,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { getTools } from '../lib/api'
import { usePeriodControls } from '../lib/use-period-controls'
import { usePeriodResource } from '../lib/use-period-resource'
import { getNextSortState, type SortState } from '../lib/table-sort'
import { formatCompactInteger, formatInteger, formatPercentage, safeDivide } from '../lib/format'
import type { ToolEntry } from '../types/api'

type SortKey = 'tool' | 'invocations' | 'successRate' | 'failures' | 'sessions' | 'share'

const DEFAULT_SORT_DIRECTIONS: Record<SortKey, 'asc' | 'desc'> = {
  tool: 'asc',
  invocations: 'desc',
  successRate: 'desc',
  failures: 'desc',
  sessions: 'desc',
  share: 'desc',
}

const DEFAULT_SORT: SortState<SortKey> = { key: 'invocations', direction: 'desc' }

interface ToolRow extends ToolEntry {
  share: number
  successRate: number
}

function toolLabel(tool: ToolEntry) {
  return tool.name || 'Unknown tool'
}

function stabilityTone(failures: number) {
  if (failures === 0) return 'success' as const
  if (failures < 5) return 'warning' as const
  return 'danger' as const
}

function stabilityLabel(failures: number) {
  if (failures === 0) return 'stable'
  if (failures < 5) return 'watch'
  return 'hot'
}

function successColor(rate: number) {
  if (rate >= 95) return 'var(--success)'
  if (rate >= 90) return 'var(--fg-primary)'
  return 'var(--warning)'
}

function compareRows(key: SortKey, a: ToolRow, b: ToolRow): number {
  switch (key) {
    case 'tool': return toolLabel(a).localeCompare(toolLabel(b))
    case 'successRate': return b.successRate - a.successRate
    case 'failures': return b.failures - a.failures
    case 'sessions': return b.sessions - a.sessions
    case 'share':
    case 'invocations': default: return b.invocations - a.invocations
  }
}

export function ToolsView() {
  const { requestRefresh } = useDashboardContext()
  const { cacheKey } = usePeriodControls()
  const { data, loading, error } = usePeriodResource(getTools, cacheKey)
  const [sortState, setSortState] = useState<SortState<SortKey> | null>(null)

  const summary = useMemo(() => {
    if (!data) return null

    const totalInvocations = data.tools.reduce((a, t) => a + t.invocations, 0)
    const totalSuccesses = data.tools.reduce((a, t) => a + t.successes, 0)
    const totalFailures = data.tools.reduce((a, t) => a + t.failures, 0)
    const maxInvocations = Math.max(1, ...data.tools.map((t) => t.invocations))

    const rows = data.tools.map<ToolRow>((tool) => ({
      ...tool,
      share: safeDivide(tool.invocations, totalInvocations) * 100,
      successRate: safeDivide(tool.successes, tool.invocations) * 100,
    }))

    const effective = sortState ?? DEFAULT_SORT
    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(effective.key, left, right)
      const m = effective.direction === DEFAULT_SORT_DIRECTIONS[effective.key] ? 1 : -1
      const d = primary * m
      if (d !== 0) return d
      if (right.invocations !== left.invocations) return right.invocations - left.invocations
      return toolLabel(left).localeCompare(toolLabel(right))
    })

    const usageLeader = [...rows].sort((a, b) => b.invocations - a.invocations)[0] ?? null

    return {
      rows: sortedRows,
      usageLeader,
      maxInvocations,
      totalInvocations,
      totalSuccesses,
      totalFailures,
      overallSuccessRate: safeDivide(totalSuccesses, totalInvocations) * 100,
      empty: rows.length === 0,
    }
  }, [data, sortState])

  // Only surface a source column if entries actually carry distinct sources.
  const distinctSources = useMemo(() => {
    const ids = new Set((data?.tools ?? []).map((t) => t.source_id).filter(Boolean))
    return ids.size > 1
  }, [data?.tools])

  const sortSpec: SortSpec = {
    key: (sortState ?? DEFAULT_SORT).key,
    dir: (sortState ?? DEFAULT_SORT).direction,
  }

  const handleSort = (key: string) => {
    setSortState((current) => {
      const next = getNextSortState(current, key as SortKey, DEFAULT_SORT_DIRECTIONS[key as SortKey])
      return next ?? DEFAULT_SORT
    })
  }

  const columns: Column<ToolRow>[] = useMemo(() => {
    const cols: Column<ToolRow>[] = [
      {
        key: 'tool',
        header: 'Tool',
        sortable: true,
        render: (row, i) => (
          <span style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <span
              style={{
                width: 26,
                height: 26,
                borderRadius: 7,
                flexShrink: 0,
                background: `color-mix(in srgb, var(--cat-${(i % 6) + 1}) 16%, var(--ink-800))`,
                border: '1px solid var(--border-subtle)',
              }}
            />
            <span style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0 }}>
              <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)' }}>{toolLabel(row)}</span>
            </span>
            <Badge tone={stabilityTone(row.failures)}>{stabilityLabel(row.failures)}</Badge>
          </span>
        ),
      },
      {
        key: 'invocations',
        header: 'Runs',
        numeric: true,
        sortable: true,
        width: 110,
        render: (row) => formatCompactInteger(row.invocations),
      },
      {
        key: 'successRate',
        header: 'Success %',
        numeric: true,
        sortable: true,
        width: 120,
        render: (row) => <span style={{ color: successColor(row.successRate) }}>{formatPercentage(row.successRate)}</span>,
      },
      {
        key: 'failures',
        header: 'Errors',
        numeric: true,
        sortable: true,
        width: 100,
        render: (row) => formatCompactInteger(row.failures),
      },
      {
        key: 'sessions',
        header: 'Sessions',
        numeric: true,
        sortable: true,
        width: 110,
        render: (row) => formatCompactInteger(row.sessions),
      },
      {
        key: 'share',
        header: 'Share',
        numeric: true,
        width: 150,
        render: (row) => {
          const pct = safeDivide(row.invocations, summary?.maxInvocations ?? 1) * 100
          return (
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, justifyContent: 'flex-end' }}>
              <span style={{ width: 60, height: 6, borderRadius: 3, background: 'var(--ink-700)', overflow: 'hidden' }}>
                <span style={{ display: 'block', width: `${Math.max(pct, row.invocations > 0 ? 4 : 0)}%`, height: '100%', background: 'var(--accent)' }} />
              </span>
              <span style={{ width: 34, textAlign: 'right' }}>{formatPercentage(row.share)}</span>
            </span>
          )
        },
      },
    ]
    return cols
  }, [summary?.maxInvocations])

  if (loading && !data) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)', padding: 16 }}>
              <Skeleton width={90} height={11} />
              <Skeleton width={120} height={28} style={{ marginTop: 12 }} />
            </div>
          ))}
        </div>
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 320 }} />
      </div>
    )
  }

  if (!data || !summary) {
    return <Card><ErrorState title="Tools failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  const topToolLabel = summary.usageLeader ? toolLabel(summary.usageLeader) : 'No data'

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && <Notice tone="warning" title="Tools partially loaded">{error}</Notice>}

      {/* KPI row */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
        <StatCard
          accent
          label="Tracked tools"
          value={formatInteger(summary.rows.length)}
          hint={summary.rows.length === 1 ? 'One tool recorded' : 'Distinct tool names'}
        />
        <StatCard
          label="Total runs"
          value={formatInteger(summary.totalInvocations)}
          title={formatInteger(summary.totalInvocations)}
          hint={`${formatCompactInteger(summary.totalSuccesses)} ok · ${formatCompactInteger(summary.totalFailures)} failed`}
        />
        <StatCard
          label="Overall success"
          value={formatPercentage(summary.overallSuccessRate)}
          hint={summary.totalFailures > 0 ? `${formatInteger(summary.totalFailures)} failed runs` : 'No failed runs'}
        />
        <StatCard
          label="Top tool"
          value={topToolLabel}
          title={topToolLabel}
          hint={summary.usageLeader ? `${formatPercentage(summary.usageLeader.share)} of all runs` : 'Awaiting activity'}
        />
      </div>

      {summary.empty ? (
        <Card>
          <Notice tone="info" title="No tool usage recorded">
            No tool event data was found for this range. Adjust the time range or check that the source provides tool events.
          </Notice>
        </Card>
      ) : (
        <Card title="Tool usage" subtitle="What your agents call, ranked by volume" pad={0}>
          <DataTable
            columns={columns}
            rows={summary.rows}
            sort={sortSpec}
            onSort={handleSort}
            rowKey={(row) => `${row.source_id ?? ''}/${row.name}`}
          />
          {distinctSources && (
            <div style={{ padding: '10px 14px', font: '400 12px/1 var(--font-ui)', color: 'var(--fg-faint)' }}>
              Tools aggregated across multiple sources.
            </div>
          )}
        </Card>
      )}
    </div>
  )
}

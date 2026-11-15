/* Tools — per-source tool usage ranking (Vael). Tool data carries no cost or
   latency, so there are no such columns, and no per-tool trend, so no deltas
   or sparklines either.

   A run ends one of three ways: it succeeded, it failed, or no result was
   recorded (interrupted, still running, never answered). Rates are taken over
   the runs that did finish, so a missing result is never counted as a failure,
   and reliability is judged by error rate over a minimum sample, never by a
   raw failure count. */
import { useMemo, useState } from 'react'
import {
  Badge,
  BarRow,
  Button,
  Card,
  DataTable,
  EmptyState,
  ErrorState,
  Notice,
  SearchInput,
  ShareBar,
  StatCard,
  StatGrid,
  ViewSkeleton,
  vendorMeta,
  type BadgeTone,
  type Column,
  type SortSpec,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { getTools } from '../lib/api'
import { usePeriodControls, useResetOnChange } from '../lib/use-period-controls'
import { usePeriodResource } from '../lib/use-period-resource'
import { getNextSortState, type SortState } from '../lib/table-sort'
import { formatCompactInteger, formatCount, formatInteger, formatShare, safeDivide } from '../lib/format'
import { isTranscriptSource } from '../lib/message-display'
import type { ToolEntry } from '../types/api'

const LEADERS_SHOWN = 5
// Below this many finished runs an error rate says little: one failure in two is not "50% broken".
const MIN_RUNS_FOR_RATE = 5
const HIGH_ERROR_RATE = 0.1

type SortKey = 'tool' | 'invocations' | 'successRate' | 'failures' | 'unfinished' | 'sessions' | 'share'

const DEFAULT_SORT_DIRECTIONS: Record<SortKey, 'asc' | 'desc'> = {
  tool: 'asc',
  invocations: 'desc',
  successRate: 'desc',
  failures: 'desc',
  unfinished: 'desc',
  sessions: 'desc',
  share: 'desc',
}

const DEFAULT_SORT: SortState<SortKey> = { key: 'invocations', direction: 'desc' }

interface ToolRow extends ToolEntry {
  key: string
  /** Share of all runs in range, 0–1. */
  share: number
  /** Runs with a recorded result: succeeded or failed. */
  finished: number
  /** Runs with no recorded result. */
  unfinished: number
  /** Successes over finished runs, 0–1. */
  successRate: number
  /** Failures over finished runs, 0–1. */
  errorRate: number
}

function outcomeSummary(successes: number, failures: number, unfinished: number) {
  const parts = [`${formatInteger(successes)} succeeded`, `${formatInteger(failures)} failed`]
  if (unfinished > 0) parts.push(`${formatInteger(unfinished)} with no recorded result`)
  return parts.join(' · ')
}

function toolLabel(tool: ToolEntry) {
  return tool.name || 'Unknown tool'
}

/** A rate as a percentage that never rounds a real shortfall up to "100%" or a real share down to "0%". */
function formatRate(rate: number, runs: number) {
  if (runs === 0) return '—'
  const percent = rate * 100
  if (percent >= 100) return '100%'
  if (percent > 99.9) return '99.9%'
  if (percent > 0 && percent < 0.1) return '<0.1%'
  return `${percent.toFixed(Number.isInteger(percent) ? 0 : 1)}%`
}

function reliability(row: ToolRow): { tone: BadgeTone; label: string; color: string } | null {
  if (row.failures === 0) return null
  if (row.finished >= MIN_RUNS_FOR_RATE && row.errorRate >= HIGH_ERROR_RATE) {
    return { tone: 'danger', label: 'High error rate', color: 'var(--danger)' }
  }
  return { tone: 'warning', label: 'Some errors', color: 'var(--warning)' }
}

function compareRows(key: SortKey, a: ToolRow, b: ToolRow): number {
  switch (key) {
    case 'tool':
      return toolLabel(a).localeCompare(toolLabel(b))
    case 'successRate':
      return b.successRate - a.successRate
    case 'failures':
      return b.failures - a.failures
    case 'unfinished':
      return b.unfinished - a.unfinished
    case 'sessions':
      return b.sessions - a.sessions
    case 'share':
    case 'invocations':
    default:
      return b.invocations - a.invocations
  }
}

export function ToolsView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const { cacheKey } = usePeriodControls()
  const { data, loading, error } = usePeriodResource(getTools, cacheKey, { cacheId: 'tools' })
  const [sortState, setSortState] = useState<SortState<SortKey> | null>(null)
  const [filter, setFilter] = useState('')
  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name

  // A filter or sort chosen for one source or range would silently hide the next one's rows.
  useResetOnChange(`${cacheKey}::${selectedSourceId}`, () => {
    setSortState(null)
    setFilter('')
  })

  const summary = useMemo(() => {
    if (!data) return null

    const totalInvocations = data.tools.reduce((acc, tool) => acc + tool.invocations, 0)
    const totalSuccesses = data.tools.reduce((acc, tool) => acc + tool.successes, 0)
    const totalFailures = data.tools.reduce((acc, tool) => acc + tool.failures, 0)

    const rows = data.tools.map<ToolRow>((tool) => {
      const finished = tool.successes + tool.failures
      return {
        ...tool,
        key: `${tool.source_id ?? ''}/${tool.name}`,
        share: safeDivide(tool.invocations, totalInvocations),
        finished,
        unfinished: Math.max(0, tool.invocations - finished),
        successRate: safeDivide(tool.successes, finished),
        errorRate: safeDivide(tool.failures, finished),
      }
    })

    const effective = sortState ?? DEFAULT_SORT
    const mult = effective.direction === DEFAULT_SORT_DIRECTIONS[effective.key] ? 1 : -1
    const sorted = [...rows].sort((a, b) => {
      const primary = compareRows(effective.key, a, b) * mult
      if (primary !== 0) return primary
      if (b.invocations !== a.invocations) return b.invocations - a.invocations
      return toolLabel(a).localeCompare(toolLabel(b))
    })

    const byRuns = [...rows].sort((a, b) => b.invocations - a.invocations)
    const byFailures = rows.filter((row) => row.failures > 0).sort((a, b) => b.failures - a.failures || b.errorRate - a.errorRate)

    return {
      rows: sorted,
      mostUsed: byRuns.slice(0, LEADERS_SHOWN),
      mostFailed: byFailures.slice(0, LEADERS_SHOWN),
      failingTools: byFailures.length,
      totalInvocations,
      totalSuccesses,
      totalFailures,
      totalFinished: totalSuccesses + totalFailures,
      totalUnfinished: Math.max(0, totalInvocations - totalSuccesses - totalFailures),
      overallSuccessRate: safeDivide(totalSuccesses, totalSuccesses + totalFailures),
      empty: rows.length === 0,
    }
  }, [data, sortState])

  // The filter narrows the table only — the KPI cards and each row's share stay
  // relative to every tool in the range, so a share doesn't silently rebase to
  // the visible subset (matches the TUI's `/` filter).
  const visibleRows = useMemo(() => {
    const rows = summary?.rows ?? []
    const needle = filter.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter((row) => toolLabel(row).toLowerCase().includes(needle))
  }, [summary?.rows, filter])

  if (!data || !summary) {
    if (loading) return <ViewSkeleton stats={4} panels={[220, 360]} label="Loading tools" />
    return <Card><ErrorState title="Tools failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  const effectiveSort = sortState ?? DEFAULT_SORT
  const sortSpec: SortSpec = { key: effectiveSort.key, dir: effectiveSort.direction }
  const handleSort = (key: string) => {
    const sortKey = key as SortKey
    // Start from the sort on screen, so the first click on the active column reverses it.
    setSortState((current) => getNextSortState(current ?? DEFAULT_SORT, sortKey, DEFAULT_SORT_DIRECTIONS[sortKey]))
  }

  const columns: Column<ToolRow>[] = [
    {
      key: 'tool',
      header: 'Tool',
      sortable: true,
      render: (row) => {
        const state = reliability(row)
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 10, minWidth: 0, maxWidth: 420 }}>
            <span title={toolLabel(row)} style={{ font: '600 13px/1.2 var(--font-mono)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{toolLabel(row)}</span>
            {state && <Badge tone={state.tone}>{state.label}</Badge>}
          </span>
        )
      },
    },
    {
      key: 'invocations',
      header: 'Runs',
      numeric: true,
      sortable: true,
      width: 110,
      render: (row) => <span title={formatCount(row.invocations, 'run')}>{formatCompactInteger(row.invocations)}</span>,
    },
    {
      key: 'successRate',
      header: 'Success',
      numeric: true,
      sortable: true,
      width: 120,
      render: (row) => (
        <span title={outcomeSummary(row.successes, row.failures, row.unfinished)} style={{ color: reliability(row)?.color }}>
          {formatRate(row.successRate, row.finished)}
        </span>
      ),
    },
    {
      key: 'failures',
      header: 'Errors',
      numeric: true,
      sortable: true,
      width: 100,
      render: (row) => (row.failures > 0 ? formatCompactInteger(row.failures) : <span style={{ color: 'var(--fg-faint)' }}>0</span>),
    },
    // Only sources that leave runs without a result (Claude Code) get the column.
    ...(summary.totalUnfinished > 0
      ? [
          {
            key: 'unfinished',
            header: 'No result',
            numeric: true,
            sortable: true,
            width: 110,
            render: (row: ToolRow) =>
              row.unfinished > 0 ? (
                <span title="Runs with no recorded result: interrupted, still running or never answered.">{formatCompactInteger(row.unfinished)}</span>
              ) : (
                <span style={{ color: 'var(--fg-faint)' }}>0</span>
              ),
          } satisfies Column<ToolRow>,
        ]
      : []),
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
      header: 'Share of runs',
      numeric: true,
      sortable: true,
      width: 160,
      // Same denominator as the number beside it: share of every run in range.
      render: (row) => <ShareBar share={row.share} />,
    },
  ]

  const topTool = summary.mostUsed[0] ?? null

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && (
        <Notice tone="warning" title="Couldn't refresh tools" action={<Button size="sm" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}>
          Showing the last loaded data. {error}
        </Notice>
      )}

      <StatGrid>
        <StatCard
          accent
          label="Tool runs"
          value={formatCompactInteger(summary.totalInvocations)}
          title={formatCount(summary.totalInvocations, 'run')}
          hint={summary.empty ? 'No runs in this range' : `across ${formatCount(summary.rows.length, 'tool')}`}
        />
        <StatCard
          label="Success rate"
          value={formatRate(summary.overallSuccessRate, summary.totalFinished)}
          title={outcomeSummary(summary.totalSuccesses, summary.totalFailures, summary.totalUnfinished)}
          hint={[
            summary.totalFailures > 0 ? `${formatCompactInteger(summary.totalFailures)} failed` : 'No failed runs',
            ...(summary.totalUnfinished > 0 ? [`${formatCompactInteger(summary.totalUnfinished)} with no result`] : []),
          ].join(' · ')}
        />
        <StatCard
          label="Tools with errors"
          value={formatInteger(summary.failingTools)}
          unit={`of ${formatInteger(summary.rows.length)}`}
          hint={summary.mostFailed[0] ? `${toolLabel(summary.mostFailed[0])} fails most` : 'Nothing failed'}
        />
        <StatCard
          label="Most used"
          value={topTool ? toolLabel(topTool) : '—'}
          title={topTool ? toolLabel(topTool) : undefined}
          hint={topTool ? `${formatShare(topTool.invocations, summary.totalInvocations)} of all runs` : 'No runs in this range'}
        />
      </StatGrid>

      {summary.empty ? (
        <Card>
          <EmptyState
            icon="wrench"
            title="No tool calls in this range"
            description={
              isTranscriptSource(selectedSourceInfo)
                ? `No ${sourceLabel} tool calls were found in local transcripts for this range.`
                : `${sourceLabel} recorded no tool calls in this range.`
            }
          />
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
            <Card title="Most used" subtitle="Tools with the most runs">
              {summary.mostUsed.map((row) => (
                <BarRow
                  key={row.key}
                  label={toolLabel(row)}
                  value={formatCompactInteger(row.invocations)}
                  rawValue={row.invocations}
                  max={summary.mostUsed[0].invocations}
                  sub={`${formatShare(row.invocations, summary.totalInvocations)} of runs · ${formatCount(row.sessions, 'session')}`}
                />
              ))}
            </Card>
            <Card title="Most errors" subtitle="Tools with the most failed runs">
              {summary.mostFailed.length === 0 ? (
                <EmptyState icon="check" title="No failed runs" description="Every tool call in this range succeeded." />
              ) : (
                summary.mostFailed.map((row) => (
                  <BarRow
                    key={row.key}
                    label={toolLabel(row)}
                    value={`${formatInteger(row.failures)} failed`}
                    rawValue={row.failures}
                    max={summary.mostFailed[0].failures}
                    color={reliability(row)?.color ?? 'var(--warning)'}
                    sub={`${formatRate(row.errorRate, row.finished)} of ${formatCount(row.finished, 'finished run')}`}
                  />
                ))
              )}
            </Card>
          </div>

          <Card
            flush
            title="All tools"
            subtitle={`${formatCount(summary.rows.length, 'tool')} · ranked by runs`}
            action={<SearchInput value={filter} onChange={setFilter} placeholder="Filter tools…" label="Filter tools by name" width={220} />}
          >
            {visibleRows.length === 0 ? (
              <EmptyState
                icon="search"
                title="No tools match this filter"
                description={`No tool name contains “${filter.trim()}”.`}
                action={<Button size="sm" onClick={() => setFilter('')}>Clear filter</Button>}
              />
            ) : (
              <DataTable bare columns={columns} rows={visibleRows} sort={sortSpec} onSort={handleSort} rowKey={(row) => row.key} />
            )}
          </Card>
        </>
      )}
    </div>
  )
}

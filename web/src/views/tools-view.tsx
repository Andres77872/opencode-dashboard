import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PeriodToggle } from '../components/daily/period-toggle'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { DataTable, type DataTableColumn } from '../components/common/data-table'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Progress } from '../components/ui/progress'
import { getTools } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { formatCompactInteger, formatInteger, formatPercentage, safeDivide } from '../lib/format'
import { type SortDirection, type SortState } from '../lib/table-sort'
import type { ToolEntry } from '../types/api'
import { isDailyPeriod, type DailyPeriod } from '../types/api'

type SortKey = 'invocations' | 'sessions' | 'tool' | 'successRate' | 'failures'

const DEFAULT_SORT_DIRECTIONS: Record<SortKey, SortDirection> = {
  failures: 'desc',
  invocations: 'desc',
  sessions: 'desc',
  successRate: 'desc',
  tool: 'asc',
}

const DEFAULT_TABLE_SORT: SortState<SortKey> = {
  key: 'invocations',
  direction: 'desc',
}

interface EnrichedToolRow extends ToolEntry {
  share: number
  successRate: number
}

function getToolLabel(tool: ToolEntry) {
  return tool.name || 'Unknown tool'
}

function compareRows(sortKey: SortKey, left: EnrichedToolRow, right: EnrichedToolRow) {
  switch (sortKey) {
    case 'tool': return getToolLabel(left).localeCompare(getToolLabel(right))
    case 'sessions': return right.sessions - left.sessions
    case 'successRate': return right.successRate - left.successRate
    case 'failures': return right.failures - left.failures
    case 'invocations': default: return right.invocations - left.invocations
  }
}

function getFailureTone(failures: number) {
  if (failures === 0) return 'success' as const
  if (failures < 5) return 'warning' as const
  return 'danger' as const
}

export function ToolsView() {
  const { requestRefresh } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const [sortState, setSortState] = useState<SortState<string> | null>(null)

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = isDailyPeriod(rawPeriod) ? rawPeriod : '7d'
  const { data, loading, error } = usePeriodResource(getTools, period)

  const handleSortChange = (key: string) => {
    setSortState((current) => {
      if (!current || current.key !== key) {
        return { key: key as SortKey, direction: DEFAULT_SORT_DIRECTIONS[key as SortKey] }
      }
      const dir = current.direction === 'asc' ? 'desc' : 'asc'
      return { key: key as SortKey, direction: dir }
    })
  }

  const handlePeriodChange = (nextPeriod: DailyPeriod) => {
    if (nextPeriod === period) return
    setSortState(null)
    setSearchParams((prev) => { const n = new URLSearchParams(prev); n.set('period', nextPeriod); return n })
  }

  const summary = useMemo(() => {
    if (!data) return null

    const totalInvocations = data.tools.reduce((a, t) => a + t.invocations, 0)
    const totalSuccesses = data.tools.reduce((a, t) => a + t.successes, 0)
    const totalFailures = data.tools.reduce((a, t) => a + t.failures, 0)

    const rows = data.tools.map<EnrichedToolRow>((tool) => ({
      ...tool,
      share: safeDivide(tool.invocations, totalInvocations) * 100,
      successRate: safeDivide(tool.successes, tool.invocations) * 100,
    }))

    const effectiveSort = (sortState ?? DEFAULT_TABLE_SORT) as SortState<SortKey>

    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(effectiveSort.key, left, right)
      const m = effectiveSort.direction === DEFAULT_SORT_DIRECTIONS[effectiveSort.key] ? 1 : -1
      const d = primary * m
      if (d !== 0) return d
      if (right.invocations !== left.invocations) return right.invocations - left.invocations
      return getToolLabel(left).localeCompare(getToolLabel(right))
    })

    const topTools = [...rows].sort((a, b) => b.invocations - a.invocations).slice(0, 3)
    const usageLeader = topTools[0] ?? null
    const failureLeader = [...rows].sort((a, b) => b.failures - a.failures)[0] ?? null

    return {
      rows: sortedRows, topTools, usageLeader, failureLeader,
      totalInvocations, totalSuccesses, totalFailures,
      overallSuccessRate: safeDivide(totalSuccesses, totalInvocations) * 100,
      empty: rows.length === 0,
    }
  }, [data, sortState])

  const handleRetry = () => requestRefresh()

  // DataTable columns
  const columns: DataTableColumn<EnrichedToolRow>[] = [
    {
      key: 'tool',
      label: 'Tool',
      width: 'min-w-[15rem]',
      sortable: true,
      render: (row) => (
        <div className="space-y-2">
          <div className="truncate font-medium text-foreground">{getToolLabel(row)}</div>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Badge tone={getFailureTone(row.failures)} className="px-2 py-0.5 text-[10px] tracking-[0.16em]">
              {row.failures === 0 ? 'stable' : row.failures < 5 ? 'watch' : 'hot'}
            </Badge>
            <span>{formatCompactInteger(row.successes)} ok</span>
            <span aria-hidden="true">•</span>
            <span>{formatCompactInteger(row.failures)} failed</span>
          </div>
        </div>
      ),
    },
    {
      key: 'sessions',
      label: 'Sessions',
      width: 'w-[6rem]',
      sortable: true,
      render: (row) => <span className="font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</span>,
    },
    {
      key: 'invocations',
      label: 'Runs',
      width: 'w-[7rem]',
      sortable: true,
      render: (row) => <span className="font-mono text-sm text-foreground">{formatCompactInteger(row.invocations)}</span>,
    },
    {
      key: 'successRate',
      label: 'Success',
      width: 'w-[8rem]',
      sortable: true,
      render: (row) => <span className="font-mono text-sm text-foreground">{formatPercentage(row.successRate)}</span>,
    },
    {
      key: 'failures',
      label: 'Errors',
      width: 'w-[6rem]',
      sortable: true,
      render: (row) => <span className="font-mono text-sm text-foreground">{formatCompactInteger(row.failures)}</span>,
    },
    {
      key: 'share',
      label: 'Share',
      width: 'w-[10rem]',
      sortable: false,
      render: (row) => (
        <div className="space-y-2">
          <Progress value={Math.max(row.share, row.invocations > 0 ? 4 : 0)} />
          <div className="font-mono text-xs text-muted-foreground">{formatPercentage(row.share)}</div>
        </div>
      ),
    },
  ]

  // Loading state
  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Tools</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Tool usage ranking from <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/tools</code>.
            </p>
          </div>
        </div>
        <DataPageSkeleton sections={['kpi-grid', 'chips', 'table']} tableRows={7} />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Tools</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            A table-first view of which tools dominate execution volume, how broadly sessions rely on them, and where failures are piling up.
          </p>
        </div>

        <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="text-sm text-muted-foreground">
            Endpoint: <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/tools?period={period}</code>
          </div>
          <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Tools failed to load</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>Retry</Button>
        </Alert>
      ) : null}

      {summary ? (
        <>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <MetricCard label="Tracked tools" value={formatInteger(summary.rows.length)}
              hint={summary.rows.length === 1 ? 'One tool recorded so far' : 'Distinct tool names aggregated from tool events'} />
            <MetricCard label="Total runs" value={formatInteger(summary.totalInvocations)}
              hint={`${formatCompactInteger(summary.totalSuccesses)} completed · ${formatCompactInteger(summary.totalFailures)} errored`} />
            <MetricCard label="Top tool" value={summary.usageLeader ? getToolLabel(summary.usageLeader) : 'No data'}
              hint={summary.usageLeader ? `${formatPercentage(summary.usageLeader.share)} of all tool invocations` : 'Awaiting activity'} />
            <MetricCard label="Overall success" value={formatPercentage(summary.overallSuccessRate)}
              hint={summary.totalFailures > 0 ? `${formatInteger(summary.totalFailures)} failed tool runs detected` : 'No failed tool runs recorded'} />
          </div>

          <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_18rem] 2xl:items-start">
            <div className="min-w-0 space-y-4">
              {summary.empty ? (
                <Card>
                  <CardHeader>
                    <CardDescription>Empty state</CardDescription>
                    <CardTitle>No tool usage recorded yet</CardTitle>
                  </CardHeader>
                  <CardContent className="text-sm text-muted-foreground">
                    <p>This endpoint stays empty until the backend finds tool event data.</p>
                  </CardContent>
                </Card>
              ) : (
                <Card>
                  <CardHeader className="gap-3 lg:flex-row lg:items-end lg:justify-between">
                    <div className="space-y-1.5">
                      <CardDescription>Primary artifact</CardDescription>
                      <CardTitle>Tool usage ranking</CardTitle>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge tone="success">Dense table</Badge>
                      {summary.topTools.map((tool, i) => (
                        <Badge key={tool.name} tone={i === 0 ? 'accent' : 'default'}>
                          #{i + 1} {getToolLabel(tool)} · {formatCompactInteger(tool.invocations)}
                        </Badge>
                      ))}
                    </div>
                  </CardHeader>

                  <CardContent className="space-y-4">
                    <DataTable<EnrichedToolRow>
                      rows={summary.rows}
                      columns={columns}
                      sortState={sortState}
                      onSortChange={handleSortChange}
                      rowKey={(row) => row.name}
                      mobileCard={(row) => (
                        <div className="rounded-2xl border border-border/70 bg-panel/65 p-4">
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <div className="truncate font-medium text-foreground">{getToolLabel(row)}</div>
                              <div className="mt-1 flex flex-wrap items-center gap-2 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                                <span>{formatPercentage(row.share)} share</span>
                                <span aria-hidden="true">•</span>
                                <span>{formatPercentage(row.successRate)} success</span>
                              </div>
                            </div>
                            <Badge tone={getFailureTone(row.failures)}>
                              {row.failures === 0 ? 'stable' : `${formatCompactInteger(row.failures)} errors`}
                            </Badge>
                          </div>
                          <Progress className="mt-3" value={Math.max(row.share, row.invocations > 0 ? 4 : 0)} />
                          <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-muted-foreground">
                            <div className="rounded-lg bg-background/40 px-2.5 py-2">
                              <div className="uppercase tracking-[0.14em]">Runs</div>
                              <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.invocations)}</div>
                            </div>
                            <div className="rounded-lg bg-background/40 px-2.5 py-2">
                              <div className="uppercase tracking-[0.14em]">Sessions</div>
                              <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</div>
                            </div>
                            <div className="rounded-lg bg-background/40 px-2.5 py-2">
                              <div className="uppercase tracking-[0.14em]">Completed</div>
                              <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.successes)}</div>
                            </div>
                            <div className="rounded-lg bg-background/40 px-2.5 py-2">
                              <div className="uppercase tracking-[0.14em]">Errors</div>
                              <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.failures)}</div>
                            </div>
                          </div>
                        </div>
                      )}
                      emptyState={<span className="text-sm text-muted-foreground">No tool usage recorded yet.</span>}
                    />
                  </CardContent>
                </Card>
              )}
            </div>

            {/* Sidebar cues */}
            <Card className="hidden border-border/70 bg-panel/55 2xl:block 2xl:sticky self-start" style={{ top: 'var(--header-height)' }}>
              <CardHeader>
                <CardDescription>Operational cues</CardDescription>
                <CardTitle>Read the table faster</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Most used</div>
                  <div className="mt-2 font-mono text-base text-foreground">
                    {summary.usageLeader ? getToolLabel(summary.usageLeader) : 'No data'}
                  </div>
                  <div className="mt-1 text-sm">
                    {summary.usageLeader
                      ? `${formatCompactInteger(summary.usageLeader.invocations)} runs across ${formatCompactInteger(summary.usageLeader.sessions)} sessions`
                      : 'Awaiting activity'}
                  </div>
                </div>
                <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Noisiest failure source</div>
                  <div className="mt-2 font-mono text-base text-foreground">
                    {summary.failureLeader && summary.failureLeader.failures > 0 ? getToolLabel(summary.failureLeader) : 'No failures logged'}
                  </div>
                  <div className="mt-1 text-sm">
                    {summary.failureLeader && summary.failureLeader.failures > 0
                      ? `${formatCompactInteger(summary.failureLeader.failures)} errors at ${formatPercentage(summary.failureLeader.successRate)} success`
                      : 'Nothing is throwing errors in the current aggregate'}
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      ) : null}
    </section>
  )
}

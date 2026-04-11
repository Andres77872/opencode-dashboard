import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PeriodToggle } from '../components/daily/period-toggle'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { ToolsSkeleton } from '../components/tools/tools-skeleton'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { SortButton } from '../components/ui/sort-button'
import { Progress } from '../components/ui/progress'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../components/ui/table'
import { getTools } from '../lib/api'
import { formatCompactInteger, formatInteger, formatPercentage, safeDivide } from '../lib/format'
import { getAriaSort, getNextSortState, type SortDirection, type SortState } from '../lib/table-sort'
import type { ToolEntry, ToolStats } from '../types/api'
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
    case 'tool':
      return getToolLabel(left).localeCompare(getToolLabel(right))
    case 'sessions':
      return right.sessions - left.sessions
    case 'successRate':
      return right.successRate - left.successRate
    case 'failures':
      return right.failures - left.failures
    case 'invocations':
    default:
      return right.invocations - left.invocations
  }
}

function getFailureTone(failures: number) {
  if (failures === 0) {
    return 'success' as const
  }

  if (failures < 5) {
    return 'warning' as const
  }

  return 'danger' as const
}

export function ToolsView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const [dataByPeriod, setDataByPeriod] = useState<Partial<Record<DailyPeriod, ToolStats>>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [sortState, setSortState] = useState<SortState<SortKey> | null>(null)
  const dataByPeriodRef = useRef<Partial<Record<DailyPeriod, ToolStats>>>({})
  const hasLoadedOnceRef = useRef(false)

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = isDailyPeriod(rawPeriod) ? rawPeriod : '7d'
  const dataForPeriod = dataByPeriod[period] ?? null

  const handleSortChange = (key: SortKey) => {
    setSortState((current) => getNextSortState(current, key, DEFAULT_SORT_DIRECTIONS[key]))
  }

  const isSortedBy = (key: SortKey) => sortState?.key === key

  const getSortDirection = (key: SortKey) => (sortState?.key === key ? sortState.direction : undefined)

  // Normalize missing/invalid period to ?period=7d on mount (preserves other params).
  useEffect(() => {
    if (!isDailyPeriod(rawPeriod)) {
      setSearchParams((previous) => {
        const next = new URLSearchParams(previous)
        next.set('period', '7d')
        return next
      })
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    // Cache short-circuit: if this period was already loaded, skip the fetch.
    if (dataByPeriodRef.current[period]) {
      return
    }

    const controller = new AbortController()

    async function loadTools() {
      setRefreshing(true)
      setError(null)
      setLoading(true)

      try {
        const next = await getTools(period, controller.signal)
        const nextDataByPeriod = {
          ...dataByPeriodRef.current,
          [period]: next,
        }

        dataByPeriodRef.current = nextDataByPeriod
        setDataByPeriod(nextDataByPeriod)
        hasLoadedOnceRef.current = true
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setError(caught instanceof Error ? caught.message : 'Failed to load tool stats')
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void loadTools()

    return () => controller.abort()
  }, [period, refreshNonce, setLastUpdatedAt, setRefreshing])

  const handlePeriodChange = (nextPeriod: DailyPeriod) => {
    if (nextPeriod === period) {
      return
    }

    setSortState(null)

    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('period', nextPeriod)
      return next
    })
  }

  const summary = useMemo(() => {
    if (!dataForPeriod) {
      return null
    }

    const totalInvocations = dataForPeriod.tools.reduce((accumulator, tool) => accumulator + tool.invocations, 0)
    const totalSuccesses = dataForPeriod.tools.reduce((accumulator, tool) => accumulator + tool.successes, 0)
    const totalFailures = dataForPeriod.tools.reduce((accumulator, tool) => accumulator + tool.failures, 0)

    const rows = dataForPeriod.tools.map<EnrichedToolRow>((tool) => ({
      ...tool,
      share: safeDivide(tool.invocations, totalInvocations) * 100,
      successRate: safeDivide(tool.successes, tool.invocations) * 100,
    }))

    const effectiveSort = sortState ?? DEFAULT_TABLE_SORT

    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(effectiveSort.key, left, right)
      const directionMultiplier = effectiveSort.direction === DEFAULT_SORT_DIRECTIONS[effectiveSort.key] ? 1 : -1
      const directedPrimary = primary * directionMultiplier

      if (directedPrimary !== 0) {
        return directedPrimary
      }

      if (right.invocations !== left.invocations) {
        return right.invocations - left.invocations
      }

      return getToolLabel(left).localeCompare(getToolLabel(right))
    })

    const topTools = [...rows].sort((left, right) => right.invocations - left.invocations).slice(0, 3)
    const usageLeader = topTools[0] ?? null
    const failureLeader = [...rows].sort((left, right) => right.failures - left.failures)[0] ?? null

    return {
      rows: sortedRows,
      topTools,
      usageLeader,
      failureLeader,
      totalInvocations,
      totalSuccesses,
      totalFailures,
      overallSuccessRate: safeDivide(totalSuccesses, totalInvocations) * 100,
      empty: rows.length === 0,
    }
  }, [dataForPeriod, sortState])

  const handleRetry = () => {
    requestRefresh()
  }

  if (loading && !dataForPeriod) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Tools</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Tool usage ranking from <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/tools</code>, built for dense scanning instead of toy dashboards.
            </p>
          </div>
        </div>
        <ToolsSkeleton />
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
            Endpoint: <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/tools?period={period}</code>
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
          <Button variant="ghost" onClick={handleRetry}>
            Retry
          </Button>
        </Alert>
      ) : null}

      {summary ? (
        <>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <MetricCard
              label="Tracked tools"
              value={formatInteger(summary.rows.length)}
              hint={summary.rows.length === 1 ? 'One tool recorded so far' : 'Distinct tool names aggregated from tool events'}
            />
            <MetricCard
              label="Total runs"
              value={formatInteger(summary.totalInvocations)}
              hint={`${formatCompactInteger(summary.totalSuccesses)} completed · ${formatCompactInteger(summary.totalFailures)} errored`}
            />
            <MetricCard
              label="Top tool"
              value={summary.usageLeader ? getToolLabel(summary.usageLeader) : 'No data'}
              hint={summary.usageLeader ? `${formatPercentage(summary.usageLeader.share)} of all tool invocations` : 'Awaiting activity'}
            />
            <MetricCard
              label="Overall success"
              value={formatPercentage(summary.overallSuccessRate)}
              hint={summary.totalFailures > 0 ? `${formatInteger(summary.totalFailures)} failed tool runs detected` : 'No failed tool runs recorded'}
            />
          </div>

          {summary.empty ? (
            <Card>
              <CardHeader>
                <CardDescription>Empty state</CardDescription>
                <CardTitle>No tool usage recorded yet</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <p>
                  This endpoint stays empty until the backend finds <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">type: &quot;tool&quot;</code> parts in the session data.
                </p>
                <p>
                  Once those events exist, this screen ranks tools by invocation volume and shows session reach plus completed versus errored runs.
                </p>
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
                  {summary.topTools.map((tool, index) => (
                    <Badge key={tool.name} tone={index === 0 ? 'accent' : 'default'}>
                      #{index + 1} {getToolLabel(tool)} · {formatCompactInteger(tool.invocations)}
                    </Badge>
                  ))}
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="grid gap-4 xl:grid-cols-[1.25fr_20rem]">
                  <div className="hidden lg:block">
                    <Table className="overflow-hidden rounded-2xl border border-border/70">
                      <TableHeader className="bg-panel/75">
                        <TableRow className="border-b border-border/70 hover:bg-transparent">
                          <TableHead className="min-w-[15rem]" aria-sort={getAriaSort(sortState, 'tool')}>
                            <SortButton
                              active={isSortedBy('tool')}
                              direction={getSortDirection('tool')}
                              label="Tool"
                              onClick={() => handleSortChange('tool')}
                            />
                          </TableHead>
                          <TableHead className="w-[6rem]" aria-sort={getAriaSort(sortState, 'sessions')}>
                            <SortButton
                              active={isSortedBy('sessions')}
                              direction={getSortDirection('sessions')}
                              label="Sessions"
                              onClick={() => handleSortChange('sessions')}
                            />
                          </TableHead>
                          <TableHead className="w-[7rem]" aria-sort={getAriaSort(sortState, 'invocations')}>
                            <SortButton
                              active={isSortedBy('invocations')}
                              direction={getSortDirection('invocations')}
                              label="Runs"
                              onClick={() => handleSortChange('invocations')}
                            />
                          </TableHead>
                          <TableHead className="w-[8rem]" aria-sort={getAriaSort(sortState, 'successRate')}>
                            <SortButton
                              active={isSortedBy('successRate')}
                              direction={getSortDirection('successRate')}
                              label="Success"
                              onClick={() => handleSortChange('successRate')}
                            />
                          </TableHead>
                          <TableHead className="w-[6rem]" aria-sort={getAriaSort(sortState, 'failures')}>
                            <SortButton
                              active={isSortedBy('failures')}
                              direction={getSortDirection('failures')}
                              label="Errors"
                              onClick={() => handleSortChange('failures')}
                            />
                          </TableHead>
                          <TableHead className="w-[10rem] text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                            Share
                          </TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {summary.rows.map((row) => (
                          <TableRow key={row.name} className="bg-card/40 hover:bg-white/4">
                            <TableCell className="min-w-[15rem]">
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
                            </TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatCompactInteger(row.invocations)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatPercentage(row.successRate)}</TableCell>
                            <TableCell className="font-mono text-sm text-foreground">{formatCompactInteger(row.failures)}</TableCell>
                            <TableCell className="w-[10rem]">
                              <div className="space-y-2">
                                <Progress value={Math.max(row.share, row.invocations > 0 ? 4 : 0)} />
                                <div className="font-mono text-xs text-muted-foreground">{formatPercentage(row.share)}</div>
                              </div>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>

                  <Card className="border-border/70 bg-panel/55">
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
                        <div className="mt-1 text-sm text-muted-foreground">
                          {summary.usageLeader
                            ? `${formatCompactInteger(summary.usageLeader.invocations)} runs across ${formatCompactInteger(summary.usageLeader.sessions)} sessions`
                            : 'Awaiting activity'}
                        </div>
                      </div>

                      <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                        <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Noisiest failure source</div>
                        <div className="mt-2 font-mono text-base text-foreground">
                          {summary.failureLeader && summary.failureLeader.failures > 0
                            ? getToolLabel(summary.failureLeader)
                            : 'No failures logged'}
                        </div>
                        <div className="mt-1 text-sm text-muted-foreground">
                          {summary.failureLeader && summary.failureLeader.failures > 0
                            ? `${formatCompactInteger(summary.failureLeader.failures)} errors at ${formatPercentage(summary.failureLeader.successRate)} success`
                            : 'Nothing is throwing errors in the current aggregate'}
                        </div>
                      </div>

                      <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                        <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Backend caveat</div>
                        <div className="mt-2 text-sm leading-6 text-muted-foreground">
                          Tool stats currently expose runs, session reach, successes, and failures. They do <span className="font-semibold text-foreground">not</span> expose cost impact yet, so this slice stays honest and skips fake spend math.
                        </div>
                      </div>
                    </CardContent>
                  </Card>
                </div>

                <div className="space-y-3 lg:hidden">
                  {summary.rows.map((row) => (
                    <div key={row.name} className="rounded-2xl border border-border/70 bg-panel/65 p-4">
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="truncate font-medium text-foreground">{getToolLabel(row)}</div>
                          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                            <span>{formatPercentage(row.share)} share</span>
                            <span aria-hidden="true">•</span>
                            <span>{formatPercentage(row.successRate)} success</span>
                          </div>
                        </div>
                        <Badge tone={getFailureTone(row.failures)}>{row.failures === 0 ? 'stable' : `${formatCompactInteger(row.failures)} errors`}</Badge>
                      </div>

<Progress
                         className="mt-3"
                         value={Math.max(row.share, row.invocations > 0 ? 4 : 0)}
                       />

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
                  ))}
                </div>
              </CardContent>
            </Card>
          )}
        </>
      ) : null}
    </section>
  )
}

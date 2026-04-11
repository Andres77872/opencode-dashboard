import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { SegmentedControl } from '../components/daily/segmented-control'
import { PeriodToggle } from '../components/daily/period-toggle'
import {
  getModelsMetricMeta,
  getModelsMetricValue,
  modelsMetricOptions,
  type EnrichedModelRow,
  type ModelsMetric,
} from '../components/models/models-metrics'
import { ModelsRowCard } from '../components/models/models-row-card'
import {
  compareRows,
  DEFAULT_SORT_DIRECTIONS,
  DEFAULT_TABLE_SORT,
  getModelLabel,
  getProviderLabel,
  getTotalTokens,
  ModelsTable,
  type SortKey,
} from '../components/models/models-table'
import { ModelsSkeleton } from '../components/models/models-skeleton'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../components/ui/tooltip'
import { getModels } from '../lib/api'
import { formatCompactInteger, formatCurrency, formatInteger, formatPercentage, safeDivide } from '../lib/format'
import { getNextSortState, type SortState } from '../lib/table-sort'
import { getTokenTotal } from '../lib/token-breakdown'
import { isDailyPeriod, type DailyPeriod, type ModelStats } from '../types/api'

function getEmptyWindowCopy(period: DailyPeriod): string {
  if (period === 'all') {
    return 'All historic stretches from the first recorded activity day through today when data exists.'
  }
  return 'No model usage recorded in the selected period. Models appear when assistant messages with modelID metadata exist.'
}

export function ModelsView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const [dataByPeriod, setDataByPeriod] = useState<Partial<Record<DailyPeriod, ModelStats>>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [sortState, setSortState] = useState<SortState<SortKey> | null>(null)
  const [metric, setMetric] = useState<ModelsMetric>('cost')
  const dataByPeriodRef = useRef<Partial<Record<DailyPeriod, ModelStats>>>({})
  const hasLoadedOnceRef = useRef(false)

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = isDailyPeriod(rawPeriod) ? rawPeriod : '7d'
  const dataForPeriod = dataByPeriod[period] ?? null

  useEffect(() => {
    const controller = new AbortController()

    async function loadModels() {
      setRefreshing(true)
      setError(null)

      if (!hasLoadedOnceRef.current) {
        setLoading(true)
      } else if (!dataByPeriodRef.current[period]) {
        setLoading(true)
      }

      try {
        const next = await getModels(period, controller.signal)
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

        setError(caught instanceof Error ? caught.message : 'Failed to load model stats')
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void loadModels()

    return () => controller.abort()
  }, [period, refreshNonce, setLastUpdatedAt, setRefreshing])

  const summary = useMemo(() => {
    if (!dataForPeriod) {
      return null
    }

    const totalCost = dataForPeriod.models.reduce((acc, m) => acc + m.cost, 0)
    const totalMessages = dataForPeriod.models.reduce((acc, m) => acc + m.messages, 0)
    const totalSessions = dataForPeriod.models.reduce((acc, m) => acc + m.sessions, 0)
    const totalTokens = dataForPeriod.models.reduce((acc, m) => acc + getTokenTotal(m.tokens), 0)

    const rows = dataForPeriod.models.map<EnrichedModelRow>((model) => ({
      ...model,
      totalTokens: getTotalTokens(model),
      avgCostPerMessage: safeDivide(model.cost, model.messages),
      costShare: safeDivide(model.cost, totalCost) * 100,
    }))

    const effectiveSort = sortState ?? DEFAULT_TABLE_SORT

    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(effectiveSort.key, left, right)
      const multiplier = effectiveSort.direction === DEFAULT_SORT_DIRECTIONS[effectiveSort.key] ? 1 : -1
      const directedPrimary = primary * multiplier

      if (directedPrimary !== 0) {
        return directedPrimary
      }

      if (right.cost !== left.cost) {
        return right.cost - left.cost
      }

      return getModelLabel(left).localeCompare(getModelLabel(right))
    })

    // Compute leaders without re-sorting - use reduce instead
    const costLeader = rows.reduce(
      (best, row) => (!best || row.cost > best.cost ? row : best),
      null as EnrichedModelRow | null
    )
    const usageLeader = rows.reduce(
      (best, row) => (!best || row.messages > best.messages ? row : best),
      null as EnrichedModelRow | null
    )
    const efficiencyLeader = rows
      .filter((row) => row.messages > 0)
      .reduce(
        (best, row) => (!best || row.avgCostPerMessage < best.avgCostPerMessage ? row : best),
        null as EnrichedModelRow | null
      )

    // Compute total for selected metric
    const totalMetricValue = rows.reduce((acc, row) => acc + getModelsMetricValue(row, metric), 0)

    return {
      rows: sortedRows,
      totalCost,
      totalMessages,
      totalSessions,
      totalTokens,
      totalMetricValue,
      empty: rows.length === 0,
      costLeader,
      usageLeader,
      efficiencyLeader,
    }
  }, [dataForPeriod, sortState, metric])

  const handleSortChange = (key: SortKey) => {
    setSortState((current) => getNextSortState(current, key, DEFAULT_SORT_DIRECTIONS[key]))
  }

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

  const handleMetricChange = (nextMetric: ModelsMetric) => {
    setMetric(nextMetric)
  }

  const handleRetry = () => {
    requestRefresh()
  }

  const metricMeta = getModelsMetricMeta(metric)

  if (loading && !dataForPeriod) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Models</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Ranked model usage from <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/models</code>.
            </p>
          </div>
        </div>
        <ModelsSkeleton />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Models</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Dense model comparison for spend, usage, and token posture. Switch between metrics and time windows without leaving the view.
          </p>
        </div>

        <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="text-sm text-muted-foreground">
            Endpoint:{' '}
            <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/models?period={period}</code>
          </div>
          <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Models failed to load</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>
            Retry
          </Button>
        </Alert>
      ) : null}

      {summary ? (
        <>
          <TooltipProvider>
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              <MetricCard
                label="Tracked models"
                value={formatInteger(summary.rows.length)}
                hint={summary.rows.length === 1 ? 'One assistant model detected' : 'Distinct assistant model/provider combinations'}
              />
              <MetricCard
                label="Total cost"
                value={formatCurrency(summary.totalCost)}
                tooltipValue={`${formatInteger(summary.totalMessages)} assistant messages`}
                hint={`${formatCompactInteger(summary.totalMessages)} messages in window`}
              />
              <MetricCard
                label="Sessions touched"
                value={formatInteger(summary.totalSessions)}
                hint={summary.usageLeader ? `${getModelLabel(summary.usageLeader)} leads message volume` : 'Awaiting activity'}
              />
              <MetricCard
                label="Spend / message"
                value={formatCurrency(safeDivide(summary.totalCost, summary.totalMessages))}
                hint={summary.efficiencyLeader ? `${getModelLabel(summary.efficiencyLeader)} is cheapest per message` : 'Not enough data yet'}
              />
            </div>
          </TooltipProvider>

          {summary.empty ? (
            <Card>
              <CardHeader>
                <CardDescription>Empty state</CardDescription>
                <CardTitle>No model usage in this window</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <p>{getEmptyWindowCopy(period)}</p>
                <p>
                  Once data exists, this route will rank models by cost and expose message volume, token load, and per-message spend.
                </p>
              </CardContent>
            </Card>
          ) : (
            <>
              <div className="grid gap-4 xl:grid-cols-[1fr_1fr_1fr]">
                <Card>
                  <CardHeader>
                    <CardDescription>Highest cost model</CardDescription>
                    <CardTitle>{summary.costLeader ? getModelLabel(summary.costLeader) : 'No data'}</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3 text-sm text-muted-foreground">
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Provider</span>
                      <span className="font-mono text-foreground">{summary.costLeader ? getProviderLabel(summary.costLeader) : '—'}</span>
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Cost share</span>
                      <span className="font-mono text-foreground">{summary.costLeader ? formatPercentage(summary.costLeader.costShare) : '0%'}</span>
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Total cost</span>
                      <span className="font-mono text-foreground">{summary.costLeader ? formatCurrency(summary.costLeader.cost) : '$0.00'}</span>
                    </div>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader>
                    <CardDescription>Most used model</CardDescription>
                    <CardTitle>{summary.usageLeader ? getModelLabel(summary.usageLeader) : 'No data'}</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3 text-sm text-muted-foreground">
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Messages</span>
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="cursor-default font-mono text-foreground transition-opacity hover:opacity-80">
                              {summary.usageLeader ? formatInteger(summary.usageLeader.messages) : '0'}
                            </span>
                          </TooltipTrigger>
                          <TooltipContent side="top" className="max-w-xs text-center">
                            <p>Assistant message count. One API request may produce multiple messages during tool use.</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Sessions</span>
                      <span className="font-mono text-foreground">{summary.usageLeader ? formatInteger(summary.usageLeader.sessions) : '0'}</span>
                    </div>
                    <div className="flex items-center justify-between gap-3 rounded-xl bg-panel/75 px-3 py-3">
                      <span>Total tokens</span>
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="cursor-default font-mono text-foreground transition-opacity hover:opacity-80">
                              {summary.usageLeader ? formatInteger(summary.usageLeader.totalTokens) : '0'}
                            </span>
                          </TooltipTrigger>
                          <TooltipContent side="top" className="font-mono">
                            <p>{summary.usageLeader ? formatInteger(summary.usageLeader.totalTokens) : '0'} tokens</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader>
                    <CardDescription>Best cost / message</CardDescription>
                    <CardTitle>{summary.efficiencyLeader ? getModelLabel(summary.efficiencyLeader) : 'Insufficient data'}</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-3 text-sm text-muted-foreground">
                    <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
                      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Cost per message</div>
                      <div className="mt-2 font-mono text-lg text-foreground">
                        {summary.efficiencyLeader ? formatCurrency(summary.efficiencyLeader.avgCostPerMessage) : '$0.00'}
                      </div>
                      <div className="text-sm text-muted-foreground">
                        {summary.efficiencyLeader
                          ? `${formatInteger(summary.efficiencyLeader.messages)} assistant messages sampled`
                          : 'Need at least one active model'}
                      </div>
                    </div>
                  </CardContent>
                </Card>
              </div>

              <Card>
                <CardHeader className="gap-3 lg:flex-row lg:items-end lg:justify-between">
                  <div className="space-y-1.5">
                    <CardDescription>Primary artifact</CardDescription>
                    <CardTitle>Model usage ranking</CardTitle>
                  </div>
                  <div className="flex flex-wrap items-center gap-3">
                    <Badge tone="success">Dense table</Badge>
                    <Badge>{metricMeta.progressLabel}</Badge>
                    <SegmentedControl
                      ariaLabel="Metric selection"
                      disabled={loading}
                      onChange={handleMetricChange}
                      options={modelsMetricOptions}
                      value={metric}
                    />
                  </div>
                </CardHeader>
                <CardContent className="space-y-4">
                  <p className="text-sm text-muted-foreground">{metricMeta.description}</p>

                  <div className="hidden lg:block">
                    <ModelsTable
                      rows={summary.rows}
                      metric={metric}
                      totalMetricValue={summary.totalMetricValue}
                      sortState={sortState}
                      onSortChange={handleSortChange}
                    />
                  </div>

                  <div className="space-y-3 lg:hidden">
                    {summary.rows.map((row) => (
                      <ModelsRowCard
                        key={`${row.provider_id}:${row.model_id}`}
                        row={row}
                        metric={metric}
                        totalMetricValue={summary.totalMetricValue}
                      />
                    ))}
                  </div>
                </CardContent>
              </Card>
            </>
          )}
        </>
      ) : null}
    </section>
  )
}
import { ChevronRightIcon } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { SegmentedControl } from '../components/daily/segmented-control'
import { PeriodToggle } from '../components/daily/period-toggle'
import type { PeriodMode } from '../components/daily/period-toggle'
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
  ModelsTable,
  type SortKey,
} from '../components/models/models-table'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../components/ui/tooltip'
import { getModels } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { formatCompactInteger, formatCurrency, formatInteger, formatPercentage, formatTokenCount, safeDivide } from '../lib/format'
import { getNextSortState, type SortState } from '../lib/table-sort'
import { getAvgTokenTotal, getTokenTotal } from '../lib/token-breakdown'
import { usePeriodState, serializeCustomPeriod, applyPeriodToUrl } from '../lib/use-period-state'
import type { CustomPeriod, DailyPeriod } from '../types/api'

function getEmptyWindowCopy(period: string): string {
  if (period === 'all') {
    return 'All historic stretches from the first recorded activity day through today when data exists.'
  }
  return 'No model usage recorded in the selected period. Models appear when assistant messages with modelID metadata exist.'
}

export function ModelsView() {
  const { requestRefresh } = useDashboardContext()
  const [, setSearchParams] = useSearchParams()
  const [sortState, setSortState] = useState<SortState<SortKey> | null>(null)
  const [metric, setMetric] = useState<ModelsMetric>('cost')

  const periodState = usePeriodState()
  const cacheKey = periodState.mode === 'custom' && periodState.customRange
    ? serializeCustomPeriod(periodState.customRange.from, periodState.customRange.to)
    : periodState.preset
  const { data, loading, error } = usePeriodResource(getModels, cacheKey)

  const summary = useMemo(() => {
    if (!data) {
      return null
    }

    const totalCost = data.models.reduce((acc, m) => acc + m.cost, 0)
    const totalMessages = data.models.reduce((acc, m) => acc + m.messages, 0)
    const totalSessions = data.models.reduce((acc, m) => acc + m.sessions, 0)
    const totalTokens = data.models.reduce((acc, m) => acc + getTokenTotal(m.tokens), 0)

    const rows = data.models.map<EnrichedModelRow>((model) => ({
      ...model,
      totalTokens: getTokenTotal(model.tokens),
      avgCostPerMessage: safeDivide(model.cost, model.messages),
      costShare: safeDivide(model.cost, totalCost) * 100,
      avgTokensPerMessage: model.avg_tokens_per_message,
      avgTokensPerSession: model.avg_tokens_per_session,
      totalAvgTokensPerMessage: model.avg_tokens_per_message ? getAvgTokenTotal(model.avg_tokens_per_message) : 0,
      totalAvgTokensPerSession: model.avg_tokens_per_session ? getAvgTokenTotal(model.avg_tokens_per_session) : 0,
      tokenShare: safeDivide(getTokenTotal(model.tokens), totalTokens) * 100,
      sessionShare: safeDivide(model.sessions, totalSessions) * 100,
      messageShare: safeDivide(model.messages, totalMessages) * 100,
    }))

    const effectiveSort = sortState ?? DEFAULT_TABLE_SORT

    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(effectiveSort.key, left, right, metric)
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
  }, [data, sortState, metric])

  const handleSortChange = (key: SortKey) => {
    setSortState((current) => getNextSortState(current, key, DEFAULT_SORT_DIRECTIONS[key]))
  }

  const handlePresetChange = (nextPeriod: DailyPeriod) => {
    setSortState(null)
    setSearchParams((previous) => {
      return applyPeriodToUrl(previous, { mode: 'preset', preset: nextPeriod })
    })
  }

  const handleCustomRangeChange = (range: CustomPeriod) => {
    setSortState(null)
    setSearchParams((previous) => {
      return applyPeriodToUrl(previous, { mode: 'custom', customRange: range })
    })
  }

  const handleModeChange = (mode: PeriodMode) => {
    setSortState(null)
    if (mode === 'preset') {
      setSearchParams((previous) => {
        return applyPeriodToUrl(previous, { mode: 'preset', preset: periodState.preset })
      })
    } else {
      setSearchParams((previous) => {
        return applyPeriodToUrl(previous, {
          mode: 'custom',
          customRange: periodState.customRange ?? { from: '' },
        })
      })
    }
  }

  const handleMetricChange = (nextMetric: ModelsMetric) => {
    setMetric(nextMetric)
    setSortState(null) // reset sort to default on metric change
  }

  const handleRetry = () => {
    requestRefresh()
  }

  const metricMeta = getModelsMetricMeta(metric)

  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Models</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Ranked model usage from <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/models</code>.
            </p>
          </div>
        </div>
        <DataPageSkeleton sections={['kpi-grid', 'table']} tableRows={7} />
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
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/models?period={cacheKey}</code>
          </div>
          <PeriodToggle
              mode={periodState.mode}
              preset={periodState.preset}
              customRange={periodState.customRange}
              onPresetChange={handlePresetChange}
              onCustomRangeChange={handleCustomRangeChange}
              onModeChange={handleModeChange}
              disabled={loading}
            />
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
                <p>{getEmptyWindowCopy(cacheKey)}</p>
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

              <details className="group">
                <summary className="cursor-pointer text-sm font-medium text-muted-foreground hover:text-foreground">
                  <ChevronRightIcon className="inline h-4 w-4 transition-transform group-open:rotate-90" />
                  {' '}Advanced Metrics (token averages)
                </summary>
                <div className="mt-4 grid grid-cols-2 gap-6">
                  <div>
                    <h4 className="text-sm font-medium text-foreground">Avg per message</h4>
                    <div className="mt-2 space-y-1 text-sm text-muted-foreground">
                      <div>Input: {formatTokenCount(summary.rows[0]?.avgTokensPerMessage?.input ?? 0)}</div>
                      <div>Output: {formatTokenCount(summary.rows[0]?.avgTokensPerMessage?.output ?? 0)}</div>
                      <div>Reasoning: {formatTokenCount(summary.rows[0]?.avgTokensPerMessage?.reasoning ?? 0)}</div>
                      <div>Cache Read: {formatTokenCount(summary.rows[0]?.avgTokensPerMessage?.cache_read ?? 0)}</div>
                      <div>Cache Write: {formatTokenCount(summary.rows[0]?.avgTokensPerMessage?.cache_write ?? 0)}</div>
                    </div>
                  </div>
                  <div>
                    <h4 className="text-sm font-medium text-foreground">Avg per session</h4>
                    <div className="mt-2 space-y-1 text-sm text-muted-foreground">
                      <div>Input: {formatTokenCount(summary.rows[0]?.avgTokensPerSession?.input ?? 0)}</div>
                      <div>Output: {formatTokenCount(summary.rows[0]?.avgTokensPerSession?.output ?? 0)}</div>
                      <div>Reasoning: {formatTokenCount(summary.rows[0]?.avgTokensPerSession?.reasoning ?? 0)}</div>
                      <div>Cache Read: {formatTokenCount(summary.rows[0]?.avgTokensPerSession?.cache_read ?? 0)}</div>
                      <div>Cache Write: {formatTokenCount(summary.rows[0]?.avgTokensPerSession?.cache_write ?? 0)}</div>
                    </div>
                  </div>
                </div>
              </details>
            </>
          )}
        </>
      ) : null}
    </section>
  )
}

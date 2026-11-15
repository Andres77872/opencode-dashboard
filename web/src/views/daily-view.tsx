/* Daily — single-source daily trends (Vael). Cost is per selected source, so
   summing days' cost is fine. No fabricated period-over-period deltas: KPIs and
   the chart only show real derived values. The message history at the bottom
   is its own component (components/daily) with its own request and ?page=. */
import { useMemo, useState } from 'react'
import {
  AreaChart,
  Button,
  Card,
  DataTable,
  EmptyState,
  ErrorState,
  Legend,
  Notice,
  SegmentedControl,
  Skeleton,
  StackedBars,
  StatCard,
  StatGrid,
  ViewSkeleton,
  useWidth,
  vendorMeta,
  type Column,
} from '../components/vael'
import { RequestedTierBadge } from '../components/daily/request-badges'
import { RequestsHistory } from '../components/daily/requests-history'
import { useHistoryPage } from '../components/daily/use-history-page'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { usePeriodControls, useResetOnChange } from '../lib/use-period-controls'
import { usePeriodResource } from '../lib/use-period-resource'
import { getDaily, getDailyDimension } from '../lib/api'
import {
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCostProvenance,
  formatAxisCurrency,
  formatCount,
  formatCurrency,
  formatCurrencyExact,
  formatCurrencyWithProvenance,
  formatInteger,
  formatShortDate,
  formatTokenCount,
  getCostLabel,
  isMissingCost,
  safeDivide,
} from '../lib/format'
import { isIncompleteRequestAccounting, requestAccountingDisclosure } from '../lib/request-accounting'
import { groupModelDaysByDate } from '../lib/daily-models'
import { isTranscriptSource } from '../lib/message-display'
import { getTokenTotal } from '../lib/token-breakdown'
import {
  aggregateProcessingModeUsage,
  getProcessingModeMeta,
  PROCESSING_MODE_ORDER,
  REQUESTED_TIER_DISCLOSURE,
  resolveProcessingMode,
  type ProcessingModeUsage,
} from '../lib/processing-mode'
import type { DayStats, DailyDimensionStats, DimensionDayStats, ProcessingMode, SourceID } from '../types/api'

const DAILY_MODELS_SHOWN = 3
// Long ranges produce hundreds of buckets; the table starts with the recent ones.
const BREAKDOWN_ROWS_COLLAPSED = 14
const CHART_HEIGHT = 260
const CHART_MIN_WIDTH = 220

// Period-resource fetcher for the per-day model breakdown (daily?dimension=model).
function getDailyModels(period: string, signal?: AbortSignal, sourceId?: SourceID) {
  return getDailyDimension('model', period, signal, sourceId)
}

// Keep this dimension request Codex-only. Other adapters do not expose Codex's
// locally requested processing tier and must not receive an unsupported query.
function getDailyProcessingModes(period: string, signal?: AbortSignal, sourceId?: SourceID): Promise<DailyDimensionStats> {
  if (sourceId !== 'codex') {
    return Promise.resolve({ source_id: sourceId, days: [], dimension: 'processing_mode', period, granularity: 'day' as const })
  }
  return getDailyDimension('processing_mode', period, signal, sourceId)
}

type DailyMetric = 'cost' | 'tokens' | 'requests'
type DailyBreakdown = 'overall' | 'processing_mode'

const BREAKDOWN_OPTS: { value: DailyBreakdown; label: string }[] = [
  { value: 'overall', label: 'Overall' },
  { value: 'processing_mode', label: 'Requested mode' },
]

const METRIC_SUBTITLES: Record<DailyMetric, (unitPlural: string) => string> = {
  cost: (unitPlural) => `${unitPlural[0].toUpperCase()}${unitPlural.slice(1)} without activity show as zero.`,
  tokens: () => 'Input, output, reasoning and cache tokens combined.',
  requests: () => 'Assistant requests sent to the provider, including retries and requests with no recorded usage.',
}

const METRIC_FORMATS: Record<DailyMetric, { axis: (value: number) => string; value: (value: number) => string }> = {
  cost: { axis: formatAxisCurrency, value: formatCurrency },
  tokens: { axis: formatTokenCount, value: formatTokenCount },
  requests: { axis: formatCompactInteger, value: formatInteger },
}

const NO_MODE_VALUES: Record<ProcessingMode, number> = { fast: 0, standard: 0, flex: 0, unknown: 0 }
const VIEW_COLUMN = { display: 'flex', flexDirection: 'column', gap: 12 } as const

function getDayMetricValue(day: DayStats, metric: DailyMetric): number {
  switch (metric) {
    case 'requests':
      return day.requests ?? 0
    case 'tokens':
      return getTokenTotal(day.tokens)
    default:
      return day.cost
  }
}

function getDimensionMetricValue(row: DimensionDayStats, metric: DailyMetric): number {
  switch (metric) {
    case 'cost':
      return row.cost
    case 'tokens':
      return getTokenTotal(row.tokens)
    default:
      return row.messages
  }
}

function hasActivity(day: DayStats): boolean {
  return day.sessions > 0 || day.messages > 0 || (day.requests ?? 0) > 0 || day.cost > 0 || getTokenTotal(day.tokens) > 0
}

function ChartMessage({ children, tone = 'muted' }: { children: string; tone?: 'muted' | 'danger' }) {
  return (
    <div style={{ height: CHART_HEIGHT, display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '0 16px', font: '400 13px/1.5 var(--font-ui)', color: tone === 'danger' ? 'var(--danger)' : 'var(--fg-muted)', textAlign: 'center' }}>
      {children}
    </div>
  )
}

export function DailyView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name
  const isCodex = selectedSourceId === 'codex'

  const [metric, setMetric] = useState<DailyMetric>('cost')
  const [breakdownChoice, setBreakdownChoice] = useState<DailyBreakdown>('overall')
  const [showAllBuckets, setShowAllBuckets] = useState(false)
  // Only Codex records a requested mode; elsewhere the choice simply does not apply.
  const breakdown: DailyBreakdown = isCodex ? breakdownChoice : 'overall'

  const { cacheKey: period } = usePeriodControls()
  const scope = `${selectedSourceId}::${period}`
  useResetOnChange(scope, () => setShowAllBuckets(false))
  // Owned here because this view stays mounted while a new source or range
  // loads; the list below does not.
  const history = useHistoryPage(scope)

  const { data, loading, error } = usePeriodResource(getDaily, period, { cacheId: 'daily' })
  // Per-day model message counts for the breakdown table. Loads independently;
  // the table renders without the Models column data until it arrives.
  const { data: modelDaily } = usePeriodResource(getDailyModels, period, { cacheId: 'daily-models' })
  const {
    data: processingModeDaily,
    loading: processingModeLoading,
    error: processingModeError,
  } = usePeriodResource(getDailyProcessingModes, period, { cacheId: 'daily-modes' })

  const modelsByDate = useMemo(() => groupModelDaysByDate(modelDaily?.days), [modelDaily])

  // ── Summary derived from real data ──
  const summary = useMemo(() => {
    if (!data) return null

    const totals = data.days.reduce(
      (acc, day) => {
        acc.sessionDays += day.sessions
        acc.messages += day.messages
        acc.requests += day.requests ?? 0
        acc.cost += day.cost
        acc.tokens += getTokenTotal(day.tokens)
        if (hasActivity(day)) acc.activeBuckets += 1
        return acc
      },
      { sessionDays: 0, messages: 0, requests: 0, cost: 0, tokens: 0, activeBuckets: 0 },
    )

    return {
      ...totals,
      isHourly: data.granularity === 'hour',
      bucketCount: data.days.length,
      recentFirst: [...data.days].reverse(),
      empty: totals.activeBuckets === 0,
    }
  }, [data])

  const chart = useMemo(() => {
    const days = data?.days ?? []
    return {
      labels: days.map((day) => formatShortDate(day.date)),
      values: days.map((day) => getDayMetricValue(day, metric)),
    }
  }, [data?.days, metric])

  const modeBreakdown = useMemo(() => {
    const rows = processingModeDaily?.days ?? []
    const byDate = new Map<string, Record<ProcessingMode, number>>()
    for (const row of rows) {
      const perMode = byDate.get(row.date) ?? { ...NO_MODE_VALUES }
      perMode[resolveProcessingMode(row.dimension_key)] += getDimensionMetricValue(row, metric)
      byDate.set(row.date, perMode)
    }

    // Every bucket of the range keeps its slot, so a quiet day reads as a gap
    // instead of its neighbours closing ranks around it.
    const dates = [...new Set([...(data?.days ?? []).map((day) => day.date), ...byDate.keys()])].sort()

    return {
      hasRows: rows.length > 0,
      totals: aggregateProcessingModeUsage(rows),
      days: dates.map((date) => ({ key: formatShortDate(date), per: byDate.get(date) ?? NO_MODE_VALUES })),
      keys: PROCESSING_MODE_ORDER.map((mode) => {
        const meta = getProcessingModeMeta(mode)
        return { id: mode, short: meta.shortLabel, color: meta.color }
      }),
    }
  }, [data?.days, metric, processingModeDaily?.days])

  const [chartRef, chartWidth] = useWidth(720)

  // Rendered in both the loading and the loaded layout under the same key, so
  // the list keeps its place (and its request runs alongside the charts')
  // instead of starting over once they arrive.
  const messageHistory = <RequestsHistory key={scope} period={period} sourceLabel={sourceLabel} history={history} />

  if (!data || !summary) {
    if (!loading) return <Card><ErrorState title="Daily usage failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
    return (
      <div style={VIEW_COLUMN}>
        <ViewSkeleton stats={5} panels={[340, 300]} label="Loading daily usage" />
        {messageHistory}
      </div>
    )
  }

  const bucketUnit = summary.isHourly ? 'hour' : 'day'
  const bucketUnitPlural = `${bucketUnit}s`
  const costLabel = getCostLabel(data.cost_status, data.cost_provenance)
  const metricLabels: Record<DailyMetric, string> = { cost: costLabel, tokens: 'Tokens', requests: 'Requests' }
  const metricOptions = (['cost', 'tokens', 'requests'] as const).map((value) => ({ value, label: metricLabels[value] }))
  const metricFormat = METRIC_FORMATS[metric]
  const byMode = breakdown === 'processing_mode'
  const chartTitle = byMode ? `${metricLabels[metric]} by requested mode` : `${metricLabels[metric]} per ${bucketUnit}`
  const chartWidthPx = Math.max(CHART_MIN_WIDTH, chartWidth)
  const perActive = (value: number) => safeDivide(value, summary.activeBuckets)
  const costMissing = isMissingCost(data.cost_status, data.cost_provenance)
  const costBasis = formatCostProvenance(data.cost_status, data.cost_provenance)

  const visibleBuckets = showAllBuckets ? summary.recentFirst : summary.recentFirst.slice(0, BREAKDOWN_ROWS_COLLAPSED)

  const dailyCols: Column<DayStats>[] = [
    {
      key: 'date',
      header: summary.isHourly ? 'Hour' : 'Date',
      render: (day) => <span style={{ color: hasActivity(day) ? 'var(--fg-primary)' : 'var(--fg-faint)' }}>{formatShortDate(day.date)}</span>,
    },
    { key: 'sessions', header: 'Sessions', numeric: true, render: (day) => formatInteger(day.sessions) },
    { key: 'messages', header: 'Messages', numeric: true, render: (day) => formatInteger(day.messages) },
    { key: 'requests', header: 'Requests', numeric: true, render: (day) => formatInteger(day.requests ?? 0) },
    // The dimension endpoint buckets with the same granularity as the daily
    // series, so the keys match in hourly mode too.
    {
      key: 'models',
      header: 'Models · requests',
      wrap: true,
      render: (day) => {
        const rows = modelsByDate.get(day.date) ?? []
        if (rows.length === 0) return <span style={{ color: 'var(--fg-faint)' }}>—</span>
        const shown = rows.slice(0, DAILY_MODELS_SHOWN)
        const hidden = rows.length - shown.length
        return (
          <span title={rows.map((row) => `${row.dimension_key}: ${formatCount(row.messages, 'request')}`).join('\n')} style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0, padding: '6px 0' }}>
            {shown.map((row) => (
              <span key={`${row.provider_id ?? ''}/${row.dimension_key}`} style={{ display: 'flex', alignItems: 'baseline', gap: 6, minWidth: 0 }}>
                <span style={{ font: '400 11px/1.3 var(--font-mono)', color: 'var(--fg-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.dimension_key}</span>
                <span style={{ font: '600 11px/1.3 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{formatInteger(row.messages)}</span>
              </span>
            ))}
            {hidden > 0 && <span style={{ font: '400 11px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>+{hidden} more</span>}
          </span>
        )
      },
    },
    {
      key: 'tokens',
      header: 'Tokens',
      numeric: true,
      render: (day) => {
        const total = getTokenTotal(day.tokens)
        return <span title={total > 0 ? `${formatInteger(total)} tokens` : undefined}>{formatTokenCount(total)}</span>
      },
    },
    {
      key: 'cost',
      header: costLabel,
      numeric: true,
      render: (day) => (
        <span title={isMissingCost(day.cost_status, day.cost_provenance) ? undefined : formatCurrencyExact(day.cost)}>
          {formatCompactCurrencyWithProvenance(day.cost, day.cost_status, day.cost_provenance)}
        </span>
      ),
    },
  ]

  const modeTotalCols: Column<ProcessingModeUsage>[] = [
    { key: 'mode', header: 'Requested tier', render: (row) => <RequestedTierBadge processingMode={row.mode} /> },
    { key: 'messages', header: 'Requests', numeric: true, render: (row) => formatInteger(row.messages) },
    { key: 'tokens', header: 'Tokens', numeric: true, render: (row) => <span title={`${formatInteger(row.tokens)} tokens`}>{formatTokenCount(row.tokens)}</span> },
    { key: 'cost', header: costLabel, numeric: true, render: (row) => formatCurrencyWithProvenance(row.cost, row.costStatus, row.costProvenance) },
  ]

  return (
    <div style={VIEW_COLUMN}>
      {error && (
        <Notice tone="warning" title="Couldn't refresh daily usage" action={<Button size="sm" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}>
          Showing the last loaded data. {error}
        </Notice>
      )}
      {selectedSourceId === 'kimi_code' && data.request_accounting && isIncompleteRequestAccounting(data.request_accounting) && (
        <Notice tone="warning" title="Kimi usage evidence has gaps">{requestAccountingDisclosure(data.request_accounting)}</Notice>
      )}

      <StatGrid>
        <StatCard
          accent
          label={costLabel}
          value={formatCurrencyWithProvenance(summary.cost, data.cost_status, data.cost_provenance)}
          title={costMissing ? undefined : [formatCurrencyExact(summary.cost), costBasis].filter(Boolean).join(' · ')}
          // How the cost is derived is on the source strip above; the card
          // keeps it only when there is no figure to show and it is the reason.
          hint={costMissing
            ? costBasis ?? 'No cost data for this range'
            : summary.activeBuckets > 0
              ? `${formatCurrency(perActive(summary.cost))} per active ${bucketUnit}`
              : `${formatCount(summary.bucketCount, bucketUnit)} in range`}
        />
        <StatCard
          label={`Active ${bucketUnitPlural}`}
          value={formatInteger(summary.activeBuckets)}
          unit={`of ${formatInteger(summary.bucketCount)}`}
          // Per-bucket session counts overlap (a session can span several), so
          // they are averaged, never summed into a range total.
          hint={summary.empty ? 'No activity in this range' : `${perActive(summary.sessionDays).toFixed(1)} sessions per active ${bucketUnit}`}
        />
        <StatCard
          label="Messages"
          value={formatInteger(summary.messages)}
          hint={`${formatCompactInteger(Math.round(perActive(summary.messages)))} per active ${bucketUnit}`}
        />
        <StatCard
          label="Requests"
          value={formatInteger(summary.requests)}
          hint={`${formatCompactInteger(Math.round(perActive(summary.requests)))} per active ${bucketUnit}`}
        />
        <StatCard
          label="Tokens"
          value={formatTokenCount(summary.tokens)}
          title={`${formatInteger(summary.tokens)} tokens`}
          hint={`${formatTokenCount(Math.round(perActive(summary.tokens)))} per active ${bucketUnit}`}
        />
      </StatGrid>

      <Card
        title={chartTitle}
        subtitle={byMode ? REQUESTED_TIER_DISCLOSURE : METRIC_SUBTITLES[metric](bucketUnitPlural)}
        action={(
          <>
            {isCodex && <SegmentedControl size="sm" ariaLabel="Breakdown" options={BREAKDOWN_OPTS} value={breakdown} onChange={setBreakdownChoice} />}
            <SegmentedControl size="sm" ariaLabel="Metric" options={metricOptions} value={metric} onChange={setMetric} />
          </>
        )}
      >
        {/* Clipped so the chart's first paint, before its width is measured, cannot widen the page. */}
        <div ref={chartRef} style={{ minWidth: 0, overflow: 'hidden' }}>
          {byMode ? (
            processingModeLoading && !processingModeDaily ? (
              <Skeleton width="100%" height={CHART_HEIGHT} />
            ) : processingModeError && !modeBreakdown.hasRows ? (
              <ChartMessage tone="danger">The requested-mode breakdown could not be loaded.</ChartMessage>
            ) : modeBreakdown.hasRows ? (
              <>
                <StackedBars
                  days={modeBreakdown.days}
                  keys={modeBreakdown.keys}
                  width={chartWidthPx}
                  height={CHART_HEIGHT}
                  // One formatter serves this chart's axis and its tooltip, so it has to be the short one.
                  valueFmt={metricFormat.value}
                  axisFmt={metricFormat.axis}
                  label={metricLabels[metric]}
                  ariaLabel={`${chartTitle}, ${modeBreakdown.days.length} ${bucketUnitPlural}`}
                />
                <div style={{ marginTop: 10 }}>
                  <Legend
                    items={modeBreakdown.totals.map((row) => {
                      const meta = getProcessingModeMeta(row.mode)
                      return {
                        label: meta.label,
                        color: meta.color,
                        value:
                          metric === 'cost'
                            ? formatCurrencyWithProvenance(row.cost, row.costStatus, row.costProvenance)
                            : metricFormat.value(metric === 'tokens' ? row.tokens : row.messages),
                      }
                    })}
                  />
                </div>
              </>
            ) : (
              <ChartMessage>Codex recorded no requested mode in this range.</ChartMessage>
            )
          ) : chart.labels.length > 0 ? (
            <div role="img" aria-label={`${chartTitle}, ${chart.labels[0]} to ${chart.labels[chart.labels.length - 1]}`}>
              <AreaChart
                labels={chart.labels}
                series={[{ name: metricLabels[metric], color: 'var(--cat-1)', data: chart.values, fmt: metricFormat.value }]}
                width={chartWidthPx}
                height={CHART_HEIGHT}
                yFormat={metricFormat.axis}
              />
            </div>
          ) : (
            <ChartMessage>No data for this range.</ChartMessage>
          )}
        </div>
      </Card>

      {byMode && (
        <Card flush title="Totals by requested mode" subtitle="Cost and tokens are summed from assistant requests.">
          {processingModeError && !modeBreakdown.hasRows ? (
            <ErrorState title="Requested-mode totals failed to load" message={processingModeError} onRetry={requestRefresh} />
          ) : (
            <DataTable bare dense columns={modeTotalCols} rows={modeBreakdown.totals} rowKey={(row) => row.mode} />
          )}
        </Card>
      )}

      <Card
        flush
        title={`${summary.isHourly ? 'Hourly' : 'Daily'} breakdown`}
        subtitle={`${formatCount(summary.bucketCount, bucketUnit)} · most recent first`}
      >
        {summary.empty ? (
          <EmptyState
            icon="calendar"
            title="No activity in this range"
            description={
              isTranscriptSource(selectedSourceInfo)
                ? `No ${sourceLabel} activity was found in local transcripts for this range.`
                : `${sourceLabel} recorded no sessions or messages in this range.`
            }
          />
        ) : (
          <>
            <DataTable bare dense columns={dailyCols} rows={visibleBuckets} rowKey={(day) => day.date} />
            {summary.recentFirst.length > BREAKDOWN_ROWS_COLLAPSED && (
              <div style={{ display: 'flex', justifyContent: 'center', padding: '10px 14px', borderTop: '1px solid var(--border-subtle)' }}>
                <Button size="sm" variant="ghost" onClick={() => setShowAllBuckets((shown) => !shown)}>
                  {showAllBuckets ? `Show the latest ${BREAKDOWN_ROWS_COLLAPSED} ${bucketUnitPlural}` : `Show all ${formatCount(summary.recentFirst.length, bucketUnit)}`}
                </Button>
              </div>
            )}
          </>
        )}
      </Card>

      {messageHistory}
    </div>
  )
}

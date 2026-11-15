/* Overview — all-sources aggregate (Vael). The Usage section can group the same
   tokens/cost/requests metrics by source or by source-scoped model. Model data
   is a second, leaner request that starts once the headline numbers are in, so
   it never delays them. Costs are never rolled into a headline KPI; chart
   totals are only the visible stacked arithmetic. */
import { useMemo, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import {
  Button,
  Card,
  CompositionBar,
  StatCard,
  StatGrid,
  SectionTitle,
  DataTable,
  KeyValueRow,
  VendorChip,
  BarRow,
  Legend,
  Donut,
  ShareBar,
  StackedBars,
  SegmentedControl,
  Skeleton,
  ViewSkeleton,
  ErrorState,
  Notice,
  useWidth,
  vendorMeta,
  type Column,
  type DonutSegment,
  type StackedBarDay,
  type StackedBarKey,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { QuotasSection } from '../components/quotas/quotas-detail'
import { useOverviewAll } from '../lib/use-overview-all'
import { useOverviewModelUsage } from '../lib/use-overview-model-usage'
import { usePeriodControls } from '../lib/use-period-controls'
import { isIncompleteRequestAccounting, requestAccountingDisclosure } from '../lib/request-accounting'
import { getAvgTokenTotal, getTokenBreakdownItems, getTokenTotal } from '../lib/token-breakdown'
import { globalSearch } from '../lib/global-params'
import {
  buildCombinedDailyTotals,
  buildModelMetricBreakdown,
  buildSourceMetricShares,
  buildSourceTrendData,
  usageMetricCopy,
  type TrendMetric,
  type UsageGrouping,
} from '../lib/overview-all'
import {
  formatAxisCurrency,
  formatCompactCurrency,
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCostProvenance,
  formatCount,
  formatInteger,
  formatShare,
  formatShortDate,
  formatShortWeekday,
  formatTokenCount,
  getCostStatus,
} from '../lib/format'
import type { CostProvenance, CostStatus } from '../types/api'
import type { ModelEntry, ProjectEntry, SourceID, SourceOverview, ToolEntry } from '../types/api'

const METRIC_VALUES: TrendMetric[] = ['tokens', 'cost', 'requests']

const GROUPING_OPTS: { value: UsageGrouping; label: string }[] = [
  { value: 'source', label: 'Source' },
  { value: 'model', label: 'Model' },
]

const GROUPING_NOUN: Record<UsageGrouping, string> = { source: 'source', model: 'model' }
const MODEL_PALETTE = ['var(--cat-1)', 'var(--cat-2)', 'var(--cat-3)', 'var(--cat-4)', 'var(--cat-5)', 'var(--cat-6)', 'var(--cat-7)', 'var(--cat-8)']

interface UsageDonutItem {
  id: string
  label: string
  secondary?: string
  value: number
  share: number
  color: string
  costStatus?: CostStatus
  costProvenance?: CostProvenance
}

function modelColor(index: number, isOther: boolean): string {
  if (isOther) return 'var(--fg-faint)'
  return MODEL_PALETTE[index % MODEL_PALETTE.length] ?? 'var(--cat-1)'
}

function providerSummary(providerIds: string[]): string {
  if (providerIds.length === 0) return 'Provider unknown'
  if (providerIds.length <= 2) return providerIds.join(' + ')
  return `${providerIds.length} providers`
}

// Donut-only: cost shown with exactly two decimals (the shared currency formatter
// allows up to 6). Scoped here on purpose — other surfaces keep their formatting.
const donutUsd2 = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 })
function donutCostText(value: number): string {
  if (!Number.isFinite(value)) return donutUsd2.format(0)
  return Math.abs(value) >= 1_000_000 ? formatCompactCurrency(value) : donutUsd2.format(value)
}
function donutCostWithProvenance(value: number, status?: CostStatus, provenance?: CostProvenance): string {
  const st = getCostStatus(status, provenance)
  if (st === 'missing') return 'Unknown'
  const f = donutCostText(value)
  return st === 'approximate' || st === 'estimated_api_equivalent' ? `≈ ${f}` : f
}

function metricFmt(metric: TrendMetric): (v: number) => string {
  if (metric === 'cost') return (v) => formatCompactCurrency(v)
  if (metric === 'requests') return (v) => formatCompactInteger(v)
  return (v) => formatTokenCount(v)
}

export function OverviewView() {
  const { requestRefresh } = useDashboardContext()
  const navigate = useNavigate()
  const { search } = useLocation()
  const { cacheKey } = usePeriodControls()
  const { data, loading, error } = useOverviewAll(cacheKey)
  const [grouping, setGrouping] = useState<UsageGrouping>('source')
  const [metric, setMetric] = useState<TrendMetric>('tokens')
  // Held back until the base overview is in: the model breakdown feeds the
  // Model grouping and the Top models card, neither of which is above the fold.
  const modelUsage = useOverviewModelUsage(cacheKey, Boolean(data))
  const [donutHover, setDonutHover] = useState<number | null>(null)
  const [chartRef, chartWidth] = useWidth(320)

  const labelFor = useMemo(() => {
    const map = new Map<string, string>()
    for (const src of data?.sources ?? []) map.set(src.source_id, src.label ?? src.source_id)
    return (id?: SourceID) => (id ? map.get(id) ?? id : 'unknown')
  }, [data?.sources])

  // Ignore the previous period's response during the effect handoff.
  const currentModelUsage = modelUsage.data?.period === cacheKey ? modelUsage.data : null
  const modelBreakdown = useMemo(
    () => buildModelMetricBreakdown(currentModelUsage?.models ?? [], currentModelUsage?.trend ?? [], metric),
    [currentModelUsage, metric],
  )

  // Chart series switch between the eager source trend and lazy model trend.
  const { stackedDays, stackedKeys } = useMemo(() => {
    if (grouping === 'model') {
      const keyed: StackedBarKey[] = modelBreakdown.series
        .map((series, index) => ({ series, index }))
        .filter(({ series }) => modelBreakdown.trend.some((row) => Number(row[series.id] ?? 0) > 0))
        .map(({ series, index }) => ({
          id: series.id,
          short: series.isOther
            ? `${series.modelId} (${series.memberCount})`
            : `${series.modelId} · ${series.sourceId ? vendorMeta(series.sourceId).short : 'Unknown source'}`,
          color: modelColor(index, series.isOther),
        }))
      // The dimension trend is sparse (only buckets with data), while Daily
      // zero-fills its buckets. Union the axes so both groupings render the
      // identical time domain.
      const rowByDate = new Map(modelBreakdown.trend.map((row) => [String(row.date), row]))
      const axis = new Set(rowByDate.keys())
      for (const src of data?.sources ?? []) for (const day of src.trend ?? []) axis.add(day.date)
      const days: StackedBarDay[] = [...axis].sort().map((date) => {
        const row = rowByDate.get(date)
        return {
          key: formatShortDate(date),
          wd: formatShortWeekday(date),
          per: Object.fromEntries(keyed.map((key) => [key.id, Number(row?.[key.id] ?? 0)])),
        }
      })
      return { stackedDays: days, stackedKeys: keyed }
    }

    const sources = data?.sources ?? []
    const keyed: StackedBarKey[] = sources
      .filter((s) => (s.trend?.length ?? 0) > 0)
      .map((s) => ({ id: s.source_id, short: vendorMeta(s.source_id).short, color: vendorMeta(s.source_id).color }))
    const rows = buildSourceTrendData(sources, metric)
    const days: StackedBarDay[] = rows.map((r) => ({
      key: formatShortDate(String(r.date)),
      wd: formatShortWeekday(String(r.date)),
      per: Object.fromEntries(keyed.map((k) => [k.id, Number(r[k.id] ?? 0)])),
    }))
    return { stackedDays: days, stackedKeys: keyed }
  }, [data?.sources, grouping, metric, modelBreakdown])

  // Donut rows share the same bounded model series as the chart. Source rows
  // retain their vendor colors and cost provenance.
  const donutItems = useMemo<UsageDonutItem[]>(() => {
    if (grouping === 'model') {
      return modelBreakdown.series
        .map((series, index) => ({ series, index }))
        .filter(({ series }) => series.value > 0)
        .map(({ series, index }) => ({
          id: series.id,
          label: series.modelId,
          secondary: series.isOther
            ? `${series.memberCount} lower-volume models`
            : `${providerSummary(series.providerIds)} · ${series.sourceId ? labelFor(series.sourceId) : 'Unknown source'}`,
          value: series.value,
          share: series.share,
          color: modelColor(index, series.isOther),
          costStatus: series.costStatus,
          costProvenance: series.costProvenance,
        }))
    }

    return buildSourceMetricShares(data?.sources ?? [], metric)
      .filter((item) => item.value > 0)
      .sort((a, b) => b.value - a.value)
      .map((item) => ({
        id: item.source.source_id,
        label: vendorMeta(item.source.source_id).short,
        value: item.value,
        share: item.share,
        color: vendorMeta(item.source.source_id).color,
        costStatus: item.source.overview.cost_status,
        costProvenance: item.source.overview.cost_provenance,
      }))
  }, [data?.sources, grouping, labelFor, metric, modelBreakdown])

  // Combined daily totals for the always-on KPI sparklines (metric-independent;
  // cost is intentionally absent — never combined across sources).
  const { sparkLabels, tokenSpark, sessionSpark, messageSpark, requestSpark } = useMemo(() => {
    const daily = buildCombinedDailyTotals(data?.sources ?? [])
    return {
      sparkLabels: daily.map((d) => `${formatShortDate(d.date)} · ${formatShortWeekday(d.date)}`),
      tokenSpark: daily.map((d) => d.tokens),
      sessionSpark: daily.map((d) => d.sessions),
      messageSpark: daily.map((d) => d.messages),
      requestSpark: daily.map((d) => d.requests),
    }
  }, [data?.sources])

  if (loading && !data) {
    return <ViewSkeleton stats={5} panels={[200, 340]} label="Loading overview" />
  }

  if (!data) {
    return <Card><ErrorState title="Overview failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  const totalTokens = getTokenTotal(data.token_distribution)
  const activeSources = data.sources.filter((s) => s.overview.sessions > 0 || s.overview.messages > 0 || s.overview.requests > 0).length
  const kimiAccounting = data.sources.find((source) => source.source_id === 'kimi_code')?.overview.request_accounting
  const breakdown = getTokenBreakdownItems(data.token_distribution)
  const partialSources = Array.from(new Set((data.partial_errors ?? []).map((item) => labelFor(item.source_id))))

  // Segments and legend rows derive from one array so hover/focus highlighting
  // remains index-aligned in both grouping modes.
  const donutFmt = metricFmt(metric)
  const metricCopy = usageMetricCopy(metric, grouping)
  const groupedRequestCopy = usageMetricCopy('requests', grouping)
  const donutTotal = donutItems.reduce((sum, item) => sum + item.value, 0)
  const aggregateCostNeedsQualifier = metric === 'cost' && donutItems.some((item) => {
    const status = getCostStatus(item.costStatus, item.costProvenance)
    return status !== 'reported' && status !== 'computed'
  })
  const donutSegments: DonutSegment[] = donutItems.map((item) => ({
    value: item.value,
    color: item.color,
    label: item.label,
    valueText:
      metric === 'cost'
        ? donutCostWithProvenance(item.value, item.costStatus, item.costProvenance)
        : donutFmt(item.value),
    shareText: formatShare(item.value, donutTotal),
    sub: [item.secondary, metric === 'cost' ? formatCostProvenance(item.costStatus, item.costProvenance) : null]
      .filter(Boolean)
      .join(' · ') || undefined,
  }))
  const modelsPending = !currentModelUsage && !modelUsage.error
  const modelsUnavailable = !currentModelUsage && Boolean(modelUsage.error)
  const modelPending = grouping === 'model' && modelsPending
  const modelUnavailable = grouping === 'model' && modelsUnavailable
  const topModelRows = currentModelUsage?.models ?? data.top_models
  let topModelsEmptyMessage: string | undefined
  if (topModelRows.length === 0) {
    if (modelsPending) {
      topModelsEmptyMessage = 'Loading model rankings…'
    } else if (
      modelsUnavailable ||
      (currentModelUsage && (currentModelUsage.errors.length > 0 || currentModelUsage.partialErrors.some((item) => item.dimension === 'models')))
    ) {
      topModelsEmptyMessage = 'Model rankings are unavailable right now.'
    }
  }

  const sourceCols: Column<SourceOverview>[] = [
    { key: 'source', header: 'Source', render: (s) => <VendorChip id={s.source_id} /> },
    { key: 'sessions', header: 'Sessions', numeric: true, render: (s) => formatInteger(s.overview.sessions) },
    { key: 'messages', header: 'Messages', numeric: true, render: (s) => formatInteger(s.overview.messages) },
    { key: 'requests', header: 'Requests', numeric: true, render: (s) => formatInteger(s.overview.requests) },
    { key: 'tokens', header: 'Tokens', numeric: true, render: (s) => formatTokenCount(getTokenTotal(s.overview.tokens)) },
    {
      key: 'cost',
      header: 'Est. cost',
      numeric: true,
      render: (s) => formatCompactCurrencyWithProvenance(s.overview.cost, s.overview.cost_status, s.overview.cost_provenance),
    },
    {
      key: 'share',
      header: 'Token share',
      numeric: true,
      width: 130,
      render: (s) => <ShareBar share={s.token_share} color={vendorMeta(s.source_id).color} />,
    },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && (
        <Notice
          tone="warning"
          title="Couldn't refresh the overview"
          action={<Button size="sm" variant="secondary" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}
        >
          Showing the last loaded data. {error}
        </Notice>
      )}
      {partialSources.length > 0 && (
        <Notice tone="warning" title="Some breakdowns are incomplete">
          Models, projects, tools or the daily trend could not be loaded for {partialSources.join(', ')}. Totals for those sources are unaffected.
        </Notice>
      )}
      {grouping === 'model' && modelUsage.error && (
        <Notice tone="warning" title="Model usage could not be loaded">{modelUsage.error}</Notice>
      )}
      {data.errors?.map((e) => (
        <Notice key={e.source_id} tone="danger" title={`${labelFor(e.source_id)} could not be loaded`}>{e.message}</Notice>
      ))}
      {data.warnings?.map((warning) => (
        <Notice key={`recent-${warning.source_id}`} tone="warning" title={`${labelFor(warning.source_id)} recent data is temporarily incomplete`}>
          The last hours could not be read from the raw source, so they may show as zero. Older consolidated data is unaffected — refresh to retry.
        </Notice>
      ))}
      {kimiAccounting && isIncompleteRequestAccounting(kimiAccounting) && (
        <Notice tone="warning" title="Kimi usage evidence has gaps">{requestAccountingDisclosure(kimiAccounting)}</Notice>
      )}
      {grouping === 'model' && currentModelUsage?.errors.map((e) => (
        <Notice key={`model-${e.source_id}`} tone="warning" title={`${labelFor(e.source_id)} model usage is unavailable`}>{e.message}</Notice>
      ))}
      {grouping === 'model' && (currentModelUsage?.partialErrors.length ?? 0) > 0 && (
        <Notice tone="warning" title="Model breakdown is partial">
          Some model totals or daily trends could not be loaded for {Array.from(new Set(currentModelUsage?.partialErrors.map((item) => labelFor(item.source_id)) ?? [])).join(', ')}.
        </Notice>
      )}
      {data.total.sessions === 0 && (
        <Notice tone="info" title="No activity in range">No activity recorded across any source for this range. Adjust the time range or check that a source has data.</Notice>
      )}

      {/* KPI row */}
      <StatGrid>
        <StatCard accent label="Tokens" value={formatTokenCount(totalTokens)} title={formatInteger(totalTokens)} hint={`${formatCompactInteger(getAvgTokenTotal(data.tokens_per_message))} per message`} spark={tokenSpark} sparkLabels={sparkLabels} sparkFmt={formatTokenCount} />
        <StatCard label="Sessions" value={formatInteger(data.total.sessions)} hint={formatCount(data.total.days, 'active day')} spark={sessionSpark} sparkLabels={sparkLabels} sparkFmt={formatInteger} />
        <StatCard label="Messages" value={formatInteger(data.total.messages)} hint={`${data.messages_per_session.toFixed(1)} per session`} spark={messageSpark} sparkLabels={sparkLabels} sparkFmt={formatCompactInteger} />
        <StatCard label="Requests" value={formatInteger(data.total.requests)} hint="API calls made by agents" spark={requestSpark} sparkLabels={sparkLabels} sparkFmt={formatCompactInteger} />
        <StatCard label="Active sources" value={`${activeSources} of ${data.sources.length}`} hint="With activity in this range" />
      </StatGrid>

      {/* Provider quotas — live subscription usage, independent of the period filter */}
      <QuotasSection />

      {/* Usage — grouping and metric switches drive both charts in sync. */}
      <div>
        <SectionTitle
          sub={`Tokens, cost and ${groupedRequestCopy.noun} by ${GROUPING_NOUN[grouping]}`}
          action={
            <>
              <SegmentedControl
                size="sm"
                options={GROUPING_OPTS}
                value={grouping}
                ariaLabel="Group usage by"
                onChange={(next) => {
                  setDonutHover(null)
                  setGrouping(next)
                }}
              />
              <SegmentedControl
                size="sm"
                options={METRIC_VALUES.map((value) => ({ value, label: usageMetricCopy(value, grouping).label }))}
                value={metric}
                ariaLabel="Usage metric"
                onChange={(next) => {
                  setDonutHover(null)
                  setMetric(next)
                }}
              />
            </>
          }
        >
          Usage
        </SectionTitle>

        {metricCopy.explanation && (
          <p role="note" style={{ margin: '-2px 0 12px', font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>
            {metricCopy.explanation}
          </p>
        )}

        <div className="grid grid-cols-1 gap-3 xl:grid-cols-[2.1fr_1fr]">
          <Card title="Usage over time" subtitle={`Per ${GROUPING_NOUN[grouping]}, stacked`}>
            <div ref={chartRef} style={{ minWidth: 0, overflowX: 'auto' }}>
              {modelPending ? (
                <Skeleton width="100%" height={300} />
              ) : modelUnavailable ? (
                <div style={{ minHeight: 300 }}>
                  <ErrorState title="Model trend unavailable" message={modelUsage.error ?? undefined} onRetry={requestRefresh} />
                </div>
              ) : stackedDays.length > 0 && stackedKeys.length > 0 ? (
                <StackedBars
                  days={stackedDays}
                  keys={stackedKeys}
                  width={Math.max(280, chartWidth)}
                  height={300}
                  valueFmt={metricFmt(metric)}
                  axisFmt={metric === 'cost' ? formatAxisCurrency : undefined}
                  totalValueFmt={aggregateCostNeedsQualifier ? (value) => `≈ ${formatCompactCurrency(value)}` : undefined}
                  label={metricCopy.label}
                  showTotal
                  totalLine={false}
                  ariaLabel={`${metricCopy.label} usage over time grouped by ${GROUPING_NOUN[grouping]}. Series: ${stackedKeys.map((key) => key.short).join(', ')}.`}
                />
              ) : (
                <div style={{ height: 300, display: 'flex', alignItems: 'center', justifyContent: 'center', font: '400 13px/1.4 var(--font-ui)', color: 'var(--fg-muted)', textAlign: 'center' }}>No activity to chart in this range.</div>
              )}
            </div>
            {!modelPending && !modelUnavailable && stackedKeys.length > 0 && (
              <div style={{ marginTop: 12 }}>
                <Legend items={stackedKeys.map((k) => ({ label: k.short, color: k.color }))} />
              </div>
            )}
          </Card>

          <Card title={`Usage by ${GROUPING_NOUN[grouping]}`} subtitle={`Share of ${metricCopy.noun}`}>
            <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 18 }}>
              {modelPending ? (
                <Skeleton width={180} height={180} />
              ) : modelUnavailable ? (
                <div style={{ height: 180, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--fg-muted)', font: '400 13px/1.4 var(--font-ui)', textAlign: 'center' }}>The model breakdown is unavailable right now.</div>
              ) : donutSegments.length > 0 ? (
                <Donut
                  segments={donutSegments}
                  size={180}
                  thickness={20}
                  centerTop={metric === 'cost' ? `${aggregateCostNeedsQualifier ? '≈ ' : ''}${donutCostText(donutTotal)}` : donutFmt(donutTotal)}
                  centerBottom={metricCopy.noun}
                  activeIndex={donutHover}
                  onHoverIndex={setDonutHover}
                  ariaLabel={`${metricCopy.label} share by ${GROUPING_NOUN[grouping]}. ${donutItems.map((item) => `${item.label}: ${formatShare(item.value, donutTotal)}`).join(', ')}.`}
                />
              ) : (
                <div style={{ height: 180, display: 'flex', alignItems: 'center', color: 'var(--fg-muted)', font: '400 13px/1 var(--font-ui)' }}>No {metricCopy.noun} in this range.</div>
              )}
              {!modelPending && !modelUnavailable && donutSegments.length > 0 && (
                <div style={{ width: '100%', display: 'flex', flexDirection: 'column' }}>
                  {donutSegments.map((seg, i) => {
                    const on = donutHover === i
                    const item = donutItems[i]
                    if (!item) return null
                    return (
                      <button
                        key={item.id}
                        type="button"
                        aria-label={`${item.label}, ${seg.valueText}, ${seg.shareText}${item.secondary ? `, ${item.secondary}` : ''}`}
                        onMouseEnter={() => setDonutHover(i)}
                        onMouseLeave={() => setDonutHover(null)}
                        onFocus={() => setDonutHover(i)}
                        onBlur={() => setDonutHover(null)}
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          gap: 8,
                          width: '100%',
                          padding: '7px 8px',
                          border: 'none',
                          borderRadius: 'var(--radius-sm)',
                          background: on ? 'var(--ink-750)' : 'transparent',
                          color: 'inherit',
                          textAlign: 'left',
                          cursor: 'default',
                          transition: 'background var(--dur-fast) var(--ease-out)',
                        }}
                      >
                        <span style={{ width: 9, height: 9, borderRadius: 2, background: seg.color, flexShrink: 0 }} />
                        <span style={{ flex: 1, minWidth: 0 }}>
                          <span style={{ display: 'block', font: '500 13px/1.1 var(--font-ui)', color: 'var(--fg-secondary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{seg.label}</span>
                          {item.secondary && <span style={{ display: 'block', marginTop: 4, font: '400 10px/1.1 var(--font-ui)', color: 'var(--fg-faint)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{item.secondary}</span>}
                        </span>
                        <span style={{ flexShrink: 0, font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{seg.valueText}</span>
                        <span style={{ width: 42, textAlign: 'right', font: '500 12px/1 var(--font-mono)', color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>{seg.shareText}</span>
                      </button>
                    )
                  })}
                </div>
              )}
            </div>
          </Card>
        </div>
      </div>

      {/* Token breakdown + efficiency */}
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-[1.4fr_1fr]">
        <Card title="Token composition" subtitle="Where the tokens went, across every source">
          <CompositionBar items={breakdown} format={formatTokenCount} ariaLabel="Token composition" />
        </Card>

        <Card title="Averages" subtitle="Per session and per message">
          <div>
            <KeyValueRow label="Messages per session" value={data.messages_per_session.toFixed(1)} />
            <KeyValueRow label="Tokens per message" value={formatCompactInteger(getAvgTokenTotal(data.tokens_per_message))} />
            <KeyValueRow label="Input per message" value={formatCompactInteger(Math.round(data.tokens_per_message.input))} />
            <KeyValueRow label="Output per message" value={formatCompactInteger(Math.round(data.tokens_per_message.output))} />
            <KeyValueRow label="Reasoning per message" value={formatCompactInteger(Math.round(data.tokens_per_message.reasoning))} last />
          </div>
        </Card>
      </div>

      {/* Per-source usage table — each row opens that source's daily view. */}
      <div>
        <SectionTitle sub="Costs stay separate per source. Select a row to open that source.">Usage by source</SectionTitle>
        <DataTable
          columns={sourceCols}
          rows={data.sources}
          rowKey={(s) => s.source_id}
          onRowClick={(s) => navigate({ pathname: '/daily', search: globalSearch(search, { source: s.source_id }) })}
        />
      </div>

      {/* Top signals */}
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        <TopModels
          models={topModelRows}
          labelFor={labelFor}
          emptyMessage={topModelsEmptyMessage}
        />
        <TopProjects projects={data.top_projects} labelFor={labelFor} />
        <TopTools tools={data.top_tools} labelFor={labelFor} />
      </div>
    </div>
  )
}

function TopModels({ models, labelFor, emptyMessage }: { models: ModelEntry[]; labelFor: (id?: SourceID) => string; emptyMessage?: string }) {
  const top = models.slice(0, 6)
  const max = Math.max(1, ...top.map((m) => getTokenTotal(m.tokens)))
  return (
    <Card title="Top models" subtitle="By tokens">
      {top.length === 0 ? (
        <Empty message={emptyMessage} />
      ) : (
        top.map((m) => {
          const v = getTokenTotal(m.tokens)
          return <BarRow key={`${m.source_id}/${m.provider_id}/${m.model_id}`} label={m.model_id} value={formatTokenCount(v)} rawValue={v} max={max} color="var(--cat-1)" sub={`${m.provider_id} · ${labelFor(m.source_id)}`} />
        })
      )}
    </Card>
  )
}

function TopProjects({ projects, labelFor }: { projects: ProjectEntry[]; labelFor: (id?: SourceID) => string }) {
  const top = projects.slice(0, 6)
  const max = Math.max(1, ...top.map((p) => getTokenTotal(p.tokens)))
  return (
    <Card title="Top projects" subtitle="By tokens">
      {top.length === 0 ? (
        <Empty />
      ) : (
        top.map((p) => {
          const v = getTokenTotal(p.tokens)
          return <BarRow key={`${p.source_id}/${p.project_id}`} label={p.project_name} value={formatTokenCount(v)} rawValue={v} max={max} color="var(--cat-3)" sub={`${formatCount(p.sessions, 'session')} · ${labelFor(p.source_id)}`} />
        })
      )}
    </Card>
  )
}

function TopTools({ tools, labelFor }: { tools: ToolEntry[]; labelFor: (id?: SourceID) => string }) {
  const top = tools.slice(0, 6)
  const max = Math.max(1, ...top.map((t) => t.invocations))
  return (
    <Card title="Most-used tools" subtitle="By calls">
      {top.length === 0 ? (
        <Empty />
      ) : (
        top.map((t) => (
          <BarRow key={`${t.source_id}/${t.name}`} label={t.name} value={formatCompactInteger(t.invocations)} rawValue={t.invocations} max={max} color="var(--cat-2)" sub={`${formatCount(t.sessions, 'session')} · ${labelFor(t.source_id)}`} />
        ))
      )}
    </Card>
  )
}

function Empty({ message = 'Nothing recorded in this range.' }: { message?: string }) {
  return <div style={{ padding: '24px 0', textAlign: 'center', font: '400 13px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>{message}</div>
}

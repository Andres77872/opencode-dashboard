/* Overview — all-sources aggregate (Vael). Costs are shown per source, never
   combined (see useOverviewAll). No fabricated deltas: KPI sparklines come from
   the per-source daily trend; period-over-period deltas are omitted. */
import { useMemo, useState } from 'react'
import {
  Card,
  StatCard,
  SectionTitle,
  DataTable,
  VendorChip,
  BarRow,
  Legend,
  Donut,
  StackedBars,
  SegmentedControl,
  Skeleton,
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
import { useOverviewAll } from '../lib/use-overview-all'
import { usePeriodControls } from '../lib/use-period-controls'
import { getAvgTokenTotal, getTokenBreakdownItems, getTokenTotal } from '../lib/token-breakdown'
import { buildSourceTrendData, type TrendMetric } from '../lib/overview-all'
import {
  formatCompactCurrency,
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatInteger,
  formatShortDate,
  formatShortWeekday,
  formatTokenCount,
} from '../lib/format'
import type { ModelEntry, ProjectEntry, SourceID, SourceOverview, ToolEntry } from '../types/api'

const METRIC_OPTS: { value: TrendMetric; label: string }[] = [
  { value: 'tokens', label: 'Tokens' },
  { value: 'cost', label: 'Cost' },
  { value: 'messages', label: 'Messages' },
]

function metricFmt(metric: TrendMetric): (v: number) => string {
  if (metric === 'cost') return (v) => formatCompactCurrency(v)
  if (metric === 'messages') return (v) => formatCompactInteger(v)
  return (v) => formatTokenCount(v)
}

export function OverviewView() {
  const { requestRefresh } = useDashboardContext()
  const { cacheKey } = usePeriodControls()
  const { data, loading, error } = useOverviewAll(cacheKey)
  const [metric, setMetric] = useState<TrendMetric>('tokens')
  const [chartRef, chartWidth] = useWidth(720)

  const labelFor = useMemo(() => {
    const map = new Map<string, string>()
    for (const src of data?.sources ?? []) map.set(src.source_id, src.label ?? src.source_id)
    return (id?: SourceID) => (id ? map.get(id) ?? id : 'unknown')
  }, [data?.sources])

  // Per-source daily series (stacked) + a combined daily total for the KPI spark.
  const { stackedDays, stackedKeys, sparkSeries } = useMemo(() => {
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
    const spark = rows.map((r) => keyed.reduce((sum, k) => sum + Number(r[k.id] ?? 0), 0))
    return { stackedDays: days, stackedKeys: keyed, sparkSeries: spark }
  }, [data?.sources, metric])

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
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 300 }} />
      </div>
    )
  }

  if (!data) {
    return <Card><ErrorState title="Failed to load overview" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  const totalTokens = getTokenTotal(data.token_distribution)
  const activeSources = data.sources.filter((s) => s.overview.sessions > 0 || s.overview.messages > 0).length
  const breakdown = getTokenBreakdownItems(data.token_distribution).filter((i) => i.value > 0)
  const breakdownTotal = breakdown.reduce((s, i) => s + i.value, 0) || 1

  const donutSegments: DonutSegment[] = data.sources
    .filter((s) => getTokenTotal(s.overview.tokens) > 0)
    .map((s) => ({ value: getTokenTotal(s.overview.tokens), color: vendorMeta(s.source_id).color, label: vendorMeta(s.source_id).short }))

  const sourceCols: Column<SourceOverview>[] = [
    { key: 'source', header: 'Source', render: (s) => <VendorChip id={s.source_id} /> },
    { key: 'sessions', header: 'Sessions', numeric: true, render: (s) => formatInteger(s.overview.sessions) },
    { key: 'messages', header: 'Messages', numeric: true, render: (s) => formatInteger(s.overview.messages) },
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
      render: (s) => (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, justifyContent: 'flex-end' }}>
          <span style={{ width: 48, height: 6, borderRadius: 3, background: 'var(--ink-700)', overflow: 'hidden' }}>
            <span style={{ display: 'block', width: `${Math.round(s.token_share * 100)}%`, height: '100%', background: vendorMeta(s.source_id).color }} />
          </span>
          <span style={{ width: 34, textAlign: 'right' }}>{Math.round(s.token_share * 100)}%</span>
        </span>
      ),
    },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && <Notice tone="warning" title="Overview partially loaded">{error}</Notice>}
      {data.errors?.map((e) => (
        <Notice key={e.source_id} tone="danger" title={`${labelFor(e.source_id)} could not be loaded`}>{e.message}</Notice>
      ))}
      {data.total.sessions === 0 && (
        <Notice tone="info" title="No activity in range">No activity recorded across any source for this range. Adjust the time range or check that a source has data.</Notice>
      )}

      {/* KPI row */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
        <StatCard accent label="Tokens" value={formatTokenCount(totalTokens)} title={formatInteger(totalTokens)} hint={`${formatCompactInteger(getAvgTokenTotal(data.tokens_per_message))} / message`} spark={metric === 'tokens' ? sparkSeries : undefined} />
        <StatCard label="Sessions" value={formatInteger(data.total.sessions)} hint={`${data.total.days} active days`} />
        <StatCard label="Messages" value={formatInteger(data.total.messages)} hint={`${data.messages_per_session.toFixed(1)} / session`} />
        <StatCard label="Sources active" value={`${activeSources} / ${data.sources.length}`} hint="with activity in range" />
      </div>

      {/* Charts row */}
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-[2.1fr_1fr]">
        <Card
          title="Usage over time"
          subtitle="Per source, stacked"
          action={<SegmentedControl size="sm" options={METRIC_OPTS} value={metric} onChange={setMetric} />}
        >
          <div ref={chartRef} style={{ minWidth: 0 }}>
            {stackedDays.length > 0 && stackedKeys.length > 0 ? (
              <StackedBars days={stackedDays} keys={stackedKeys} width={Math.max(320, chartWidth)} height={240} valueFmt={metricFmt(metric)} />
            ) : (
              <div style={{ height: 240, display: 'flex', alignItems: 'center', justifyContent: 'center', font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>No trend data for this range.</div>
            )}
          </div>
          {stackedKeys.length > 0 && (
            <div style={{ marginTop: 12 }}>
              <Legend items={stackedKeys.map((k) => ({ label: k.short, color: k.color }))} />
            </div>
          )}
        </Card>

        <Card title="Usage by source" subtitle="Share of tokens">
          <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 16 }}>
            {donutSegments.length > 0 ? (
              <Donut segments={donutSegments} size={172} thickness={18} centerTop={formatTokenCount(totalTokens)} centerBottom="tokens" />
            ) : (
              <div style={{ height: 172, display: 'flex', alignItems: 'center', color: 'var(--fg-muted)', font: '400 13px/1 var(--font-ui)' }}>No token data.</div>
            )}
            <Legend
              items={data.sources
                .filter((s) => getTokenTotal(s.overview.tokens) > 0)
                .map((s) => ({ label: vendorMeta(s.source_id).short, color: vendorMeta(s.source_id).color, value: `${Math.round(s.token_share * 100)}%` }))}
            />
          </div>
        </Card>
      </div>

      {/* Token breakdown + efficiency */}
      <div className="grid grid-cols-1 gap-3 xl:grid-cols-[1.4fr_1fr]">
        <Card title="Token composition" subtitle="Combined token mix across sources">
          <div style={{ display: 'flex', height: 12, borderRadius: 6, overflow: 'hidden', background: 'var(--ink-700)' }}>
            {breakdown.map((i) => (
              <div key={i.key} title={`${i.label}: ${formatInteger(i.value)}`} style={{ width: `${(i.value / breakdownTotal) * 100}%`, background: i.color }} />
            ))}
          </div>
          <div style={{ marginTop: 14 }}>
            <Legend items={breakdown.map((i) => ({ label: i.label, color: i.color, value: formatTokenCount(i.value) }))} />
          </div>
        </Card>

        <Card title="Throughput" subtitle="Per-message / per-session ratios">
          <div>
            <Ratio label="Messages / session" value={data.messages_per_session.toFixed(1)} />
            <Ratio label="Tokens / message" value={formatCompactInteger(getAvgTokenTotal(data.tokens_per_message))} />
            <Ratio label="Input / message" value={formatCompactInteger(Math.round(data.tokens_per_message.input))} />
            <Ratio label="Output / message" value={formatCompactInteger(Math.round(data.tokens_per_message.output))} />
            <Ratio label="Reasoning / message" value={formatCompactInteger(Math.round(data.tokens_per_message.reasoning))} last />
          </div>
        </Card>
      </div>

      {/* Per-source usage table */}
      <div>
        <SectionTitle sub="Costs are reported per source and never combined.">Usage by source</SectionTitle>
        <DataTable columns={sourceCols} rows={data.sources} rowKey={(s) => s.source_id} />
      </div>

      {/* Top signals */}
      <div className="grid grid-cols-1 gap-3 lg:grid-cols-3">
        <TopModels models={data.top_models} labelFor={labelFor} />
        <TopProjects projects={data.top_projects} labelFor={labelFor} />
        <TopTools tools={data.top_tools} labelFor={labelFor} />
      </div>
    </div>
  )
}

function Ratio({ label, value, last }: { label: string; value: string; last?: boolean }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '10px 0', borderBottom: last ? 'none' : '1px solid var(--border-subtle)' }}>
      <span style={{ font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{label}</span>
      <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{value}</span>
    </div>
  )
}

function TopModels({ models, labelFor }: { models: ModelEntry[]; labelFor: (id?: SourceID) => string }) {
  const top = models.slice(0, 6)
  const max = Math.max(1, ...top.map((m) => getTokenTotal(m.tokens)))
  return (
    <Card title="Top models" subtitle="By tokens">
      {top.length === 0 ? (
        <Empty />
      ) : (
        top.map((m) => {
          const v = getTokenTotal(m.tokens)
          return <BarRow key={`${m.provider_id}/${m.model_id}`} label={m.model_id} value={formatTokenCount(v)} rawValue={v} max={max} color="var(--cat-1)" sub={`${m.provider_id} · ${labelFor(m.source_id)}`} />
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
          return <BarRow key={p.project_id} label={p.project_name} value={formatTokenCount(v)} rawValue={v} max={max} color="var(--cat-3)" sub={`${formatInteger(p.sessions)} sessions · ${labelFor(p.source_id)}`} />
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
          <BarRow key={`${t.source_id}/${t.name}`} label={t.name} value={formatCompactInteger(t.invocations)} rawValue={t.invocations} max={max} color="var(--cat-2)" sub={`${formatInteger(t.sessions)} sessions · ${labelFor(t.source_id)}`} />
        ))
      )}
    </Card>
  )
}

function Empty() {
  return <div style={{ padding: '24px 0', textAlign: 'center', font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>No data in range.</div>
}

/* Models — per-source model comparison (Vael). Single source (selectedSourceId).
   Costs use the *WithProvenance helpers and are never summed across sources.
   No fabricated deltas/trends: the API exposes none per model, so KPI deltas and
   per-row sparklines are omitted. Share bars reflect the selected metric. */
import { useMemo, useState } from 'react'
import {
  Card,
  StatCard,
  SectionTitle,
  DataTable,
  VendorChip,
  Badge,
  SegmentedControl,
  Skeleton,
  ErrorState,
  EmptyState,
  vendorMeta,
  type Column,
  type SortSpec,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { usePeriodControls } from '../lib/use-period-controls'
import { usePeriodResource } from '../lib/use-period-resource'
import { getModels } from '../lib/api'
import {
  formatCompactInteger,
  formatCurrencyWithProvenance,
  formatCompactCurrencyWithProvenance,
  formatInteger,
  formatPercentage,
  formatTokenCount,
  safeDivide,
} from '../lib/format'
import { getNextSortState, type SortState } from '../lib/table-sort'
import { getAvgTokenTotal, getTokenTotal } from '../lib/token-breakdown'
import type { AvgTokenStats, ModelEntry, SourceID } from '../types/api'

// ── Metric model (inlined from models-metrics.ts) ──────────────────
type ModelsMetric = 'cost' | 'sessions' | 'messages' | 'tokens'

const METRIC_OPTS: { value: ModelsMetric; label: string }[] = [
  { value: 'cost', label: 'Cost' },
  { value: 'sessions', label: 'Sessions' },
  { value: 'messages', label: 'Messages' },
  { value: 'tokens', label: 'Tokens' },
]

const METRIC_META: Record<ModelsMetric, { shareLabel: string; description: string }> = {
  cost: { shareLabel: 'Cost share', description: 'Share bars show how much each model contributes to total spend.' },
  sessions: { shareLabel: 'Session share', description: 'Share bars show how many coding sessions each model touched.' },
  messages: { shareLabel: 'Message share', description: 'Share bars show assistant response volume per model. One API request may produce multiple messages.' },
  tokens: { shareLabel: 'Token share', description: 'Share bars show cumulative input, output, reasoning, and cache tokens per model.' },
}

interface ModelRow extends ModelEntry {
  totalTokens: number
  avgCostPerMessage: number
  metricValue: number
  metricShare: number // 0..100
}

function metricValueOf(model: ModelEntry, totalTokens: number, metric: ModelsMetric): number {
  switch (metric) {
    case 'sessions':
      return model.sessions
    case 'messages':
      return model.messages
    case 'tokens':
      return totalTokens
    default:
      return model.cost
  }
}

function modelLabel(model: ModelEntry): string {
  return model.model_id || 'Unknown model'
}

function providerLabel(model: ModelEntry): string {
  return model.provider_id || 'Unknown provider'
}

// Sort keys map to the visible columns + share.
type SortKey = 'model' | 'sessions' | 'messages' | 'tokens' | 'cost' | 'avgCostPerMessage' | 'share'

const DEFAULT_SORT_DIR: Record<SortKey, 'asc' | 'desc'> = {
  model: 'asc',
  sessions: 'desc',
  messages: 'desc',
  tokens: 'desc',
  cost: 'desc',
  avgCostPerMessage: 'asc',
  share: 'desc',
}

const DEFAULT_SORT: SortState<SortKey> = { key: 'cost', direction: 'desc' }

function compareRows(key: SortKey, a: ModelRow, b: ModelRow): number {
  switch (key) {
    case 'model':
      return modelLabel(a).localeCompare(modelLabel(b))
    case 'sessions':
      return b.sessions - a.sessions
    case 'messages':
      return b.messages - a.messages
    case 'tokens':
      return b.totalTokens - a.totalTokens
    case 'avgCostPerMessage':
      return b.avgCostPerMessage - a.avgCostPerMessage
    case 'share':
      return b.metricShare - a.metricShare
    case 'cost':
    default:
      return b.cost - a.cost
  }
}

function getEmptyWindowCopy(period: string, sourceLabel: string, sourceId: SourceID): string {
  if (sourceId === 'claude_code') {
    return 'No Claude Code assistant model usage was found in readable local transcripts for this window.'
  }
  if (period === 'all') {
    return `All historic ${sourceLabel} stretches from the first recorded activity day through today when data exists.`
  }
  return `No ${sourceLabel} model usage recorded in the selected period. Models appear when assistant messages with model metadata exist.`
}

export function ModelsView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [sortState, setSortState] = useState<SortState<SortKey> | null>(null)
  const [metric, setMetric] = useState<ModelsMetric>('cost')

  const { cacheKey } = usePeriodControls({
    onChange: () => {
      setSortState(null)
      setMetric('cost')
    },
  })
  const { data, loading, error } = usePeriodResource(getModels, cacheKey)
  const sourceLabel = selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  const summary = useMemo(() => {
    if (!data) return null

    const totalCost = data.models.reduce((acc, m) => acc + m.cost, 0)
    const totalMessages = data.models.reduce((acc, m) => acc + m.messages, 0)
    const totalSessions = data.models.reduce((acc, m) => acc + m.sessions, 0)
    const totalTokens = data.models.reduce((acc, m) => acc + getTokenTotal(m.tokens), 0)

    const base = data.models.map((model) => {
      const mTok = getTokenTotal(model.tokens)
      return {
        ...model,
        totalTokens: mTok,
        avgCostPerMessage: safeDivide(model.cost, model.messages),
      }
    })

    const totalMetricValue = base.reduce((acc, r) => acc + metricValueOf(r, r.totalTokens, metric), 0)

    const rows: ModelRow[] = base.map((r) => {
      const metricValue = metricValueOf(r, r.totalTokens, metric)
      return {
        ...r,
        metricValue,
        metricShare: safeDivide(metricValue, totalMetricValue) * 100,
      }
    })

    const effective = sortState ?? DEFAULT_SORT
    const mult = effective.direction === DEFAULT_SORT_DIR[effective.key] ? 1 : -1
    const sorted = [...rows].sort((a, b) => {
      const primary = compareRows(effective.key, a, b) * mult
      if (primary !== 0) return primary
      if (b.cost !== a.cost) return b.cost - a.cost
      return modelLabel(a).localeCompare(modelLabel(b))
    })

    const costLeader = rows.reduce<ModelRow | null>((best, r) => (!best || r.cost > best.cost ? r : best), null)
    const usageLeader = rows.reduce<ModelRow | null>((best, r) => (!best || r.messages > best.messages ? r : best), null)
    const efficiencyLeader = rows
      .filter((r) => r.messages > 0)
      .reduce<ModelRow | null>((best, r) => (!best || r.avgCostPerMessage < best.avgCostPerMessage ? r : best), null)

    const advancedRow = sorted.find((r) => r.avg_tokens_per_message || r.avg_tokens_per_session) ?? null

    return {
      rows: sorted,
      totalCost,
      totalMessages,
      totalSessions,
      totalTokens,
      maxMetric: Math.max(1, ...rows.map((r) => r.metricValue)),
      empty: rows.length === 0,
      costLeader,
      usageLeader,
      efficiencyLeader,
      advancedRow,
    }
  }, [data, sortState, metric])

  const sortSpec: SortSpec | null = useMemo(() => {
    const s = sortState ?? DEFAULT_SORT
    return { key: s.key, dir: s.direction }
  }, [sortState])

  const handleSort = (key: string) => {
    setSortState((current) => getNextSortState(current, key as SortKey, DEFAULT_SORT_DIR[key as SortKey]))
  }

  const handleMetricChange = (next: ModelsMetric) => {
    setMetric(next)
    setSortState(null) // reset sort to default on metric change
  }

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
        <div className="grid grid-cols-1 gap-3 xl:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 140 }} />
          ))}
        </div>
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 320 }} />
      </div>
    )
  }

  if (!data || !summary) {
    return <Card><ErrorState title="Models failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  const meta = METRIC_META[metric]

  const cols: Column<ModelRow>[] = [
    {
      key: 'model',
      header: 'Model',
      sortable: true,
      render: (m) => (
        <span style={{ display: 'inline-flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
          <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{modelLabel(m)}</span>
          <span style={{ font: '400 11px/1 var(--font-ui)', color: 'var(--fg-faint)' }}>{providerLabel(m)}</span>
        </span>
      ),
    },
    {
      key: 'source',
      header: 'Source',
      render: (m) => <VendorChip id={m.source_id ?? selectedSourceId} />,
    },
    { key: 'sessions', header: 'Sessions', numeric: true, sortable: true, render: (m) => formatCompactInteger(m.sessions) },
    {
      key: 'messages',
      header: 'Messages',
      numeric: true,
      sortable: true,
      render: (m) => <span title={`${formatInteger(m.messages)} assistant messages`}>{formatCompactInteger(m.messages)}</span>,
    },
    {
      key: 'tokens',
      header: 'Tokens',
      numeric: true,
      sortable: true,
      render: (m) => <span title={formatInteger(m.totalTokens)}>{formatTokenCount(m.totalTokens)}</span>,
    },
    {
      key: 'cost',
      header: 'Est. cost',
      numeric: true,
      sortable: true,
      render: (m) => formatCompactCurrencyWithProvenance(m.cost, m.cost_status, m.cost_provenance),
    },
    {
      key: 'avgCostPerMessage',
      header: 'Avg / msg',
      numeric: true,
      sortable: true,
      render: (m) => formatCurrencyWithProvenance(m.avgCostPerMessage, m.cost_status, m.cost_provenance),
    },
    {
      key: 'share',
      header: meta.shareLabel,
      numeric: true,
      sortable: true,
      width: 150,
      render: (m) => {
        const pct = Math.max(m.metricValue > 0 ? 4 : 0, m.metricShare)
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, justifyContent: 'flex-end' }}>
            <span style={{ width: 54, height: 6, borderRadius: 3, background: 'var(--ink-700)', overflow: 'hidden' }}>
              <span style={{ display: 'block', width: `${Math.min(100, pct)}%`, height: '100%', background: vendorMeta(m.source_id ?? selectedSourceId).color }} />
            </span>
            <span style={{ width: 40, textAlign: 'right' }}>{formatPercentage(m.metricShare)}</span>
          </span>
        )
      },
    },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && <Card><ErrorState title="Models failed to load" message={error} onRetry={requestRefresh} /></Card>}

      {/* KPI row */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
        <StatCard
          accent
          label="Tracked models"
          value={formatInteger(summary.rows.length)}
          hint={summary.rows.length === 1 ? 'One assistant model detected' : 'Distinct model / provider combinations'}
        />
        <StatCard
          label="Total cost"
          value={formatCurrencyWithProvenance(summary.totalCost, data.cost_status, data.cost_provenance)}
          title={`${formatInteger(summary.totalMessages)} assistant messages`}
          hint={`${formatCompactInteger(summary.totalMessages)} messages in window`}
        />
        <StatCard
          label="Sessions touched"
          value={formatInteger(summary.totalSessions)}
          hint={summary.usageLeader ? `${modelLabel(summary.usageLeader)} leads volume` : 'Awaiting activity'}
        />
        <StatCard
          label="Spend / message"
          value={formatCurrencyWithProvenance(safeDivide(summary.totalCost, summary.totalMessages), data.cost_status, data.cost_provenance)}
          hint={summary.efficiencyLeader ? `${modelLabel(summary.efficiencyLeader)} is cheapest` : 'Not enough data yet'}
        />
      </div>

      {summary.empty ? (
        <Card>
          <EmptyState
            icon="cpu"
            title="No model usage in this window"
            description={`${getEmptyWindowCopy(cacheKey, sourceLabel, selectedSourceId)} Once data exists, models are ranked by cost with message volume, token load, and per-message spend.`}
          />
        </Card>
      ) : (
        <>
          {/* Leaders */}
          <div className="grid grid-cols-1 gap-3 xl:grid-cols-3">
            <LeaderCard
              eyebrow="Highest cost model"
              title={summary.costLeader ? modelLabel(summary.costLeader) : 'No data'}
              rows={[
                { label: 'Provider', value: summary.costLeader ? providerLabel(summary.costLeader) : '—' },
                { label: 'Cost share', value: summary.costLeader ? formatPercentage(safeDivide(summary.costLeader.cost, summary.totalCost) * 100) : '0%' },
                {
                  label: 'Total cost',
                  value: summary.costLeader
                    ? formatCurrencyWithProvenance(summary.costLeader.cost, summary.costLeader.cost_status, summary.costLeader.cost_provenance)
                    : 'Unknown',
                },
              ]}
            />
            <LeaderCard
              eyebrow="Most used model"
              title={summary.usageLeader ? modelLabel(summary.usageLeader) : 'No data'}
              rows={[
                { label: 'Messages', value: summary.usageLeader ? formatInteger(summary.usageLeader.messages) : '0', title: summary.usageLeader ? `${formatInteger(summary.usageLeader.messages)} assistant messages` : undefined },
                { label: 'Sessions', value: summary.usageLeader ? formatInteger(summary.usageLeader.sessions) : '0' },
                { label: 'Total tokens', value: summary.usageLeader ? formatTokenCount(summary.usageLeader.totalTokens) : '0', title: summary.usageLeader ? formatInteger(summary.usageLeader.totalTokens) : undefined },
              ]}
            />
            <LeaderCard
              eyebrow="Best cost / message"
              title={summary.efficiencyLeader ? modelLabel(summary.efficiencyLeader) : 'Insufficient data'}
              rows={[
                {
                  label: 'Cost per message',
                  value: summary.efficiencyLeader
                    ? formatCurrencyWithProvenance(summary.efficiencyLeader.avgCostPerMessage, summary.efficiencyLeader.cost_status, summary.efficiencyLeader.cost_provenance)
                    : 'Unknown',
                },
                { label: 'Provider', value: summary.efficiencyLeader ? providerLabel(summary.efficiencyLeader) : '—' },
                {
                  label: 'Messages sampled',
                  value: summary.efficiencyLeader ? formatInteger(summary.efficiencyLeader.messages) : '0',
                },
              ]}
            />
          </div>

          {/* Ranking table */}
          <Card
            title="Model usage ranking"
            subtitle={meta.description}
            pad={0}
            action={
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
                <Badge>{meta.shareLabel}</Badge>
                <SegmentedControl size="sm" options={METRIC_OPTS} value={metric} onChange={handleMetricChange} />
              </span>
            }
          >
            <DataTable
              columns={cols}
              rows={summary.rows}
              sort={sortSpec}
              onSort={handleSort}
              rowKey={(m) => `${m.provider_id}:${m.model_id}`}
            />
          </Card>

          {/* Advanced disclosure — only when avg-token stats are present */}
          {summary.advancedRow && (summary.advancedRow.avg_tokens_per_message || summary.advancedRow.avg_tokens_per_session) && (
            <details>
              <summary style={{ cursor: 'pointer', font: '600 13px/1 var(--font-ui)', color: 'var(--fg-muted)', padding: '8px 0' }}>
                Advanced — token averages ({modelLabel(summary.advancedRow)})
              </summary>
              <div style={{ marginTop: 8 }}>
                <SectionTitle sub="Per-message and per-session token averages for the top-ranked model with metadata.">
                  Token averages
                </SectionTitle>
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  {summary.advancedRow.avg_tokens_per_message && (
                    <AvgTokenCard title="Avg per message" stats={summary.advancedRow.avg_tokens_per_message} />
                  )}
                  {summary.advancedRow.avg_tokens_per_session && (
                    <AvgTokenCard title="Avg per session" stats={summary.advancedRow.avg_tokens_per_session} />
                  )}
                </div>
              </div>
            </details>
          )}
        </>
      )}
    </div>
  )
}

interface LeaderRow {
  label: string
  value: string
  title?: string
}

function LeaderCard({ eyebrow, title, rows }: { eyebrow: string; title: string; rows: LeaderRow[] }) {
  return (
    <Card eyebrow={eyebrow} title={title}>
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {rows.map((r, i) => (
          <div
            key={r.label}
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 12,
              padding: '10px 0',
              borderBottom: i < rows.length - 1 ? '1px solid var(--border-subtle)' : 'none',
            }}
          >
            <span style={{ font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{r.label}</span>
            <span title={r.title} style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{r.value}</span>
          </div>
        ))}
      </div>
    </Card>
  )
}

function AvgTokenCard({ title, stats }: { title: string; stats: AvgTokenStats }) {
  const items: { label: string; value: number }[] = [
    { label: 'Input', value: stats.input },
    { label: 'Output', value: stats.output },
    { label: 'Reasoning', value: stats.reasoning },
    { label: 'Cache read', value: stats.cache_read },
    { label: 'Cache write', value: stats.cache_write },
  ]
  return (
    <Card title={title} subtitle={`${formatTokenCount(getAvgTokenTotal(stats))} total`}>
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {items.map((it, i) => (
          <div
            key={it.label}
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 12,
              padding: '8px 0',
              borderBottom: i < items.length - 1 ? '1px solid var(--border-subtle)' : 'none',
            }}
          >
            <span style={{ font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{it.label}</span>
            <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{formatTokenCount(it.value)}</span>
          </div>
        ))}
      </div>
    </Card>
  )
}

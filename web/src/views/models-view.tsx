/* Models — per-source model comparison (Vael). Single source (selectedSourceId).
   Costs use the *WithProvenance helpers and are never summed across sources.
   No fabricated deltas/trends: the API exposes none per model, so KPI deltas and
   per-row sparklines are omitted. The table ranks by the selected metric; a row
   opens that model's token mix and averages. */
import { useMemo, useState } from 'react'
import {
  Button,
  Card,
  CompositionBar,
  DataTable,
  Drawer,
  EmptyState,
  ErrorState,
  KeyValueRow,
  Notice,
  SearchInput,
  SegmentedControl,
  ShareBar,
  StatCard,
  StatGrid,
  ViewSkeleton,
  vendorMeta,
  type Column,
  type SortSpec,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { usePeriodControls, useResetOnChange } from '../lib/use-period-controls'
import { usePeriodResource } from '../lib/use-period-resource'
import { getModels } from '../lib/api'
import {
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCostProvenance,
  formatCount,
  formatCurrencyExact,
  formatCurrencyWithProvenance,
  formatInteger,
  formatShare,
  formatTokenCount,
  getCostLabel,
  getCostStatus,
  isMissingCost,
  safeDivide,
} from '../lib/format'
import { isTranscriptSource } from '../lib/message-display'
import { getNextSortState, type SortState } from '../lib/table-sort'
import { getAvgTokenTotal, getTokenBreakdownItems, getTokenTotal } from '../lib/token-breakdown'
import type { AvgTokenStats, ModelEntry } from '../types/api'

type ModelsMetric = 'cost' | 'tokens' | 'sessions' | 'messages'

const METRIC_ORDER: ModelsMetric[] = ['cost', 'tokens', 'sessions', 'messages']

const METRIC_META: Record<Exclude<ModelsMetric, 'cost'>, { label: string; shareLabel: string; description: string }> = {
  tokens: { label: 'Tokens', shareLabel: 'Token share', description: 'Ranked by input, output, reasoning and cache tokens combined.' },
  // Sessions overlap between models, so this is a share of appearances, not of distinct sessions.
  sessions: { label: 'Sessions', shareLabel: 'Session share', description: 'Ranked by sessions each model appeared in. A session that used several models counts once for each.' },
  messages: { label: 'Messages', shareLabel: 'Message share', description: 'Ranked by assistant messages. One request can produce several messages.' },
}

interface ModelRow extends ModelEntry {
  key: string
  totalTokens: number
  avgCostPerMessage: number
  metricValue: number
  /** Share of the selected metric across every model in range, 0–1. */
  metricShare: number
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

function hasKnownCost(model: ModelEntry): boolean {
  return getCostStatus(model.cost_status, model.cost_provenance) !== 'missing' && (model.cost_provenance?.missing_count ?? 0) === 0
}

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

// The table is a ranking by the selected metric until a column is chosen.
const DEFAULT_SORT: SortState<SortKey> = { key: 'share', direction: 'desc' }

/**
 * Orders two rows for `key`.
 *
 * INVARIANT: the order returned here must match DEFAULT_SORT_DIR[key], because
 * the caller multiplies by +1 exactly when the active direction equals that
 * default (and by -1 otherwise). A comparator that disagrees with its own
 * default renders both directions inverted relative to the header arrow.
 */
function compareRows(key: SortKey, a: ModelRow, b: ModelRow): number {
  switch (key) {
    // ascending — matches DEFAULT_SORT_DIR 'asc'
    case 'model':
      return modelLabel(a).localeCompare(modelLabel(b))
    case 'avgCostPerMessage':
      return a.avgCostPerMessage - b.avgCostPerMessage
    // descending — matches DEFAULT_SORT_DIR 'desc'
    case 'sessions':
      return b.sessions - a.sessions
    case 'messages':
      return b.messages - a.messages
    case 'tokens':
      return b.totalTokens - a.totalTokens
    case 'cost':
      return b.cost - a.cost
    case 'share':
    default:
      return b.metricValue - a.metricValue
  }
}

export function ModelsView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [sortState, setSortState] = useState<SortState<SortKey> | null>(null)
  const [metric, setMetric] = useState<ModelsMetric>('cost')
  const [filter, setFilter] = useState('')
  const [selectedKey, setSelectedKey] = useState<string | null>(null)

  const { cacheKey } = usePeriodControls()
  // Sort, metric, filter and the open model describe a result set that no
  // longer exists once the period or source changes, so drop them.
  useResetOnChange(`${cacheKey}::${selectedSourceId}`, () => {
    setSortState(null)
    setMetric('cost')
    setFilter('')
    setSelectedKey(null)
  })
  const { data, loading, error } = usePeriodResource(getModels, cacheKey, { cacheId: 'models' })
  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name
  const shareColor = vendorMeta(selectedSourceId).color

  const summary = useMemo(() => {
    if (!data) return null

    const base = data.models.map((model) => {
      const totalTokens = getTokenTotal(model.tokens)
      return {
        ...model,
        key: `${model.provider_id}:${model.model_id}`,
        totalTokens,
        avgCostPerMessage: safeDivide(model.cost, model.messages),
        metricValue: metricValueOf(model, totalTokens, metric),
      }
    })
    const totalMetricValue = base.reduce((acc, row) => acc + row.metricValue, 0)
    const rows: ModelRow[] = base.map((row) => ({ ...row, metricShare: safeDivide(row.metricValue, totalMetricValue) }))

    const effective = sortState ?? DEFAULT_SORT
    const mult = effective.direction === DEFAULT_SORT_DIR[effective.key] ? 1 : -1
    const sorted = [...rows].sort((a, b) => {
      const primary = compareRows(effective.key, a, b) * mult
      if (primary !== 0) return primary
      if (b.cost !== a.cost) return b.cost - a.cost
      return modelLabel(a).localeCompare(modelLabel(b))
    })

    const leaderBy = (pick: (row: ModelRow) => number) =>
      rows.reduce<ModelRow | null>((best, row) => (pick(row) > 0 && (!best || pick(row) > pick(best)) ? row : best), null)
    const priced = rows.filter((row) => row.messages > 0 && hasKnownCost(row))

    return {
      rows: sorted,
      totalCost: rows.reduce((acc, row) => acc + row.cost, 0),
      totalMessages: rows.reduce((acc, row) => acc + row.messages, 0),
      totalTokens: rows.reduce((acc, row) => acc + row.totalTokens, 0),
      providers: new Set(rows.map((row) => row.provider_id)).size,
      empty: rows.length === 0,
      costLeader: leaderBy((row) => row.cost),
      usageLeader: leaderBy((row) => row.messages),
      efficiencyLeader: priced.reduce<ModelRow | null>((best, row) => (!best || row.avgCostPerMessage < best.avgCostPerMessage ? row : best), null),
    }
  }, [data, sortState, metric])

  // Matches the TUI's `/` filter, which matches on model *or* provider. Narrows
  // the table only: the highlights, KPIs, and per-row shares stay relative to
  // every model in the range.
  const visibleRows = useMemo(() => {
    const rows = summary?.rows ?? []
    const needle = filter.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter((row) => modelLabel(row).toLowerCase().includes(needle) || providerLabel(row).toLowerCase().includes(needle))
  }, [summary?.rows, filter])

  if (!data || !summary) {
    if (loading) return <ViewSkeleton stats={5} panels={[110, 360]} label="Loading models" />
    return <Card><ErrorState title="Models failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  const costLabel = getCostLabel(data.cost_status, data.cost_provenance)
  const metricLabel = metric === 'cost' ? costLabel : METRIC_META[metric].label
  const shareLabel = metric === 'cost' ? 'Cost share' : METRIC_META[metric].shareLabel
  const description = metric === 'cost' ? 'Ranked by cost. Open a model for its token mix and averages.' : METRIC_META[metric].description
  const metricOptions = METRIC_ORDER.map((value) => ({ value, label: value === 'cost' ? costLabel : METRIC_META[value].label }))

  const effectiveSort = sortState ?? DEFAULT_SORT
  const sortSpec: SortSpec = { key: effectiveSort.key, dir: effectiveSort.direction }
  const handleSort = (key: string) => {
    const sortKey = key as SortKey
    // Start from the sort on screen, so the first click on the active column reverses it.
    setSortState((current) => getNextSortState(current ?? DEFAULT_SORT, sortKey, DEFAULT_SORT_DIR[sortKey]))
  }

  const selected = selectedKey ? summary.rows.find((row) => row.key === selectedKey) ?? null : null

  const cols: Column<ModelRow>[] = [
    {
      key: 'model',
      header: 'Model',
      sortable: true,
      render: (m) => (
        <span style={{ display: 'inline-flex', flexDirection: 'column', gap: 3, minWidth: 0, maxWidth: 340 }}>
          <span title={modelLabel(m)} style={{ font: '600 13px/1.2 var(--font-mono)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{modelLabel(m)}</span>
          <span style={{ font: '400 11px/1.2 var(--font-ui)', color: 'var(--fg-muted)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{providerLabel(m)}</span>
        </span>
      ),
    },
    { key: 'sessions', header: 'Sessions', numeric: true, sortable: true, render: (m) => <span title={formatCount(m.sessions, 'session')}>{formatCompactInteger(m.sessions)}</span> },
    {
      key: 'messages',
      header: 'Messages',
      numeric: true,
      sortable: true,
      render: (m) => <span title={formatCount(m.messages, 'assistant message')}>{formatCompactInteger(m.messages)}</span>,
    },
    {
      key: 'tokens',
      header: 'Tokens',
      numeric: true,
      sortable: true,
      render: (m) => <span title={`${formatInteger(m.totalTokens)} tokens`}>{formatTokenCount(m.totalTokens)}</span>,
    },
    {
      key: 'cost',
      header: costLabel,
      numeric: true,
      sortable: true,
      render: (m) => (
        <span title={isMissingCost(m.cost_status, m.cost_provenance) ? undefined : formatCurrencyExact(m.cost)}>
          {formatCompactCurrencyWithProvenance(m.cost, m.cost_status, m.cost_provenance)}
        </span>
      ),
    },
    {
      key: 'avgCostPerMessage',
      header: 'Cost / msg',
      numeric: true,
      sortable: true,
      render: (m) => formatCurrencyWithProvenance(m.avgCostPerMessage, m.cost_status, m.cost_provenance),
    },
    {
      key: 'share',
      header: shareLabel,
      numeric: true,
      sortable: true,
      width: 150,
      render: (m) => <ShareBar share={m.metricShare} color={shareColor} />,
    },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && (
        <Notice tone="warning" title="Couldn't refresh models" action={<Button size="sm" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}>
          Showing the last loaded data. {error}
        </Notice>
      )}

      <StatGrid>
        <StatCard
          accent
          label={costLabel}
          value={formatCurrencyWithProvenance(summary.totalCost, data.cost_status, data.cost_provenance)}
          title={isMissingCost(data.cost_status, data.cost_provenance) ? undefined : [formatCurrencyExact(summary.totalCost), formatCostProvenance(data.cost_status, data.cost_provenance)].filter(Boolean).join(' · ')}
          hint={(isMissingCost(data.cost_status, data.cost_provenance) ? formatCostProvenance(data.cost_status, data.cost_provenance) : null) ?? 'All models in range'}
        />
        <StatCard
          label="Models"
          value={formatInteger(summary.rows.length)}
          hint={summary.empty ? 'No models in range' : `from ${formatCount(summary.providers, 'provider')}`}
        />
        <StatCard
          label="Messages"
          value={formatCompactInteger(summary.totalMessages)}
          title={formatCount(summary.totalMessages, 'assistant message')}
          hint="Assistant messages"
        />
        <StatCard
          label="Tokens"
          value={formatTokenCount(summary.totalTokens)}
          title={`${formatInteger(summary.totalTokens)} tokens`}
          hint={`${formatTokenCount(Math.round(safeDivide(summary.totalTokens, summary.totalMessages)))} per message`}
        />
        <StatCard
          label="Cost / message"
          value={formatCurrencyWithProvenance(safeDivide(summary.totalCost, summary.totalMessages), data.cost_status, data.cost_provenance)}
          hint="Average across all models"
        />
      </StatGrid>

      {summary.empty ? (
        <Card>
          <EmptyState
            icon="cpu"
            title="No model usage in this range"
            description={
              isTranscriptSource(selectedSourceInfo)
                ? `No ${sourceLabel} model usage was found in local transcripts for this range.`
                : `${sourceLabel} recorded no assistant messages with model information in this range.`
            }
          />
        </Card>
      ) : (
        <>
          <Card flush>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))', gap: 1, background: 'var(--border-subtle)' }}>
              <Highlight
                label="Highest cost"
                model={summary.costLeader}
                value={summary.costLeader ? formatCurrencyWithProvenance(summary.costLeader.cost, summary.costLeader.cost_status, summary.costLeader.cost_provenance) : '—'}
                detail={summary.costLeader ? `${formatShare(summary.costLeader.cost, summary.totalCost)} of total cost` : 'No cost recorded in this range'}
                onOpen={setSelectedKey}
              />
              <Highlight
                label="Most used"
                model={summary.usageLeader}
                value={summary.usageLeader ? formatCompactInteger(summary.usageLeader.messages) : '—'}
                detail={summary.usageLeader ? `messages · ${formatShare(summary.usageLeader.messages, summary.totalMessages)} of all` : 'No messages in this range'}
                onOpen={setSelectedKey}
              />
              <Highlight
                label="Lowest cost per message"
                model={summary.efficiencyLeader}
                value={summary.efficiencyLeader ? formatCurrencyWithProvenance(summary.efficiencyLeader.avgCostPerMessage, summary.efficiencyLeader.cost_status, summary.efficiencyLeader.cost_provenance) : '—'}
                detail={summary.efficiencyLeader ? `over ${formatCount(summary.efficiencyLeader.messages, 'message')}` : 'Needs a model with a known cost'}
                onOpen={setSelectedKey}
              />
            </div>
          </Card>

          <Card
            flush
            title="Model ranking"
            subtitle={description}
            action={
              <>
                {/* 180 is the input's own minimum: a wider basis makes the header wrap the controls while there is still room. */}
                <SearchInput value={filter} onChange={setFilter} placeholder="Filter models…" label="Filter models by name or provider" width={220} />
                <SegmentedControl size="sm" ariaLabel="Rank by" options={metricOptions} value={metric} onChange={setMetric} />
              </>
            }
          >
            {visibleRows.length === 0 ? (
              <EmptyState
                icon="search"
                title="No models match this filter"
                description={`No model or provider name contains “${filter.trim()}”.`}
                action={<Button size="sm" onClick={() => setFilter('')}>Clear filter</Button>}
              />
            ) : (
              <DataTable
                bare
                columns={cols}
                rows={visibleRows}
                sort={sortSpec}
                onSort={handleSort}
                onRowClick={(m) => setSelectedKey(m.key)}
                rowKey={(m) => m.key}
              />
            )}
          </Card>
        </>
      )}

      <Drawer
        open={selected !== null}
        onClose={() => setSelectedKey(null)}
        title={selected ? modelLabel(selected) : undefined}
        subtitle={selected ? providerLabel(selected) : undefined}
      >
        {selected && <ModelDetail model={selected} costLabel={costLabel} metricLabel={metricLabel} />}
      </Drawer>
    </div>
  )
}

const EYEBROW = { font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)' } as const

function Highlight({ label, model, value, detail, onOpen }: { label: string; model: ModelRow | null; value: string; detail: string; onOpen: (key: string) => void }) {
  return (
    <div style={{ minWidth: 0, padding: '14px 16px', background: 'var(--ink-800)' }}>
      <div style={EYEBROW}>{label}</div>
      {model ? (
        <button
          type="button"
          onClick={() => onOpen(model.key)}
          title={`${modelLabel(model)} · ${providerLabel(model)}`}
          style={{ display: 'block', maxWidth: '100%', marginTop: 8, padding: 0, background: 'none', border: 'none', borderRadius: 'var(--radius-sm)', cursor: 'pointer', font: '600 14px/1.3 var(--font-mono)', color: 'var(--fg-primary)', textAlign: 'left', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}
        >
          {modelLabel(model)}
        </button>
      ) : (
        <div style={{ marginTop: 8, font: '600 14px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>No model yet</div>
      )}
      <div style={{ display: 'flex', alignItems: 'baseline', flexWrap: 'wrap', gap: '2px 8px', marginTop: 8 }}>
        <span style={{ font: '600 18px/1.2 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{value}</span>
        <span style={{ font: '400 12px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>{detail}</span>
      </div>
    </div>
  )
}

function ModelDetail({ model, costLabel, metricLabel }: { model: ModelRow; costLabel: string; metricLabel: string }) {
  const provenance = formatCostProvenance(model.cost_status, model.cost_provenance)
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Card title="Usage" bodyStyle={{ padding: '2px 16px' }}>
        <KeyValueRow label={costLabel} value={formatCurrencyWithProvenance(model.cost, model.cost_status, model.cost_provenance)} title={isMissingCost(model.cost_status, model.cost_provenance) ? undefined : formatCurrencyExact(model.cost)} />
        <KeyValueRow label="Cost per message" value={formatCurrencyWithProvenance(model.avgCostPerMessage, model.cost_status, model.cost_provenance)} />
        <KeyValueRow label="Assistant messages" value={formatInteger(model.messages)} />
        <KeyValueRow label="Sessions" value={formatInteger(model.sessions)} />
        <KeyValueRow label="Tokens" value={formatTokenCount(model.totalTokens)} title={`${formatInteger(model.totalTokens)} tokens`} />
        <KeyValueRow label={`Share of ${metricLabel.toLowerCase()}`} value={formatShare(model.metricShare, 1)} last />
      </Card>

      {provenance && <div style={{ font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)', overflowWrap: 'anywhere' }}>Cost basis: {provenance.replace(/\.$/, '')}.</div>}

      <Card title="Token mix" pad={14}>
        <CompositionBar items={getTokenBreakdownItems(model.tokens)} format={formatTokenCount} ariaLabel="Token mix" />
      </Card>

      {model.avg_tokens_per_message && <AverageTokens title="Average tokens per message" stats={model.avg_tokens_per_message} />}
      {model.avg_tokens_per_session && <AverageTokens title="Average tokens per session" stats={model.avg_tokens_per_session} />}
    </div>
  )
}

function AverageTokens({ title, stats }: { title: string; stats: AvgTokenStats }) {
  const items: { label: string; value: number }[] = [
    { label: 'Input', value: stats.input },
    { label: 'Output', value: stats.output },
    { label: 'Reasoning', value: stats.reasoning },
    { label: 'Cache read', value: stats.cache_read },
    { label: 'Cache write', value: stats.cache_write },
  ]
  return (
    <Card title={title} subtitle={`${formatTokenCount(Math.round(getAvgTokenTotal(stats)))} in total`} bodyStyle={{ padding: '2px 16px' }}>
      {items.map((item, i) => (
        <KeyValueRow key={item.label} label={item.label} value={formatTokenCount(Math.round(item.value))} last={i === items.length - 1} />
      ))}
    </Card>
  )
}

/* Daily — single-source daily trends (Vael). Cost is per selected source, so
   summing days' cost is fine. No fabricated period-over-period deltas: KPIs and
   the chart only show real derived values. The message ledger is fetched and
   paginated separately (?page= in the URL) and drills into a Drawer detail. */
import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Card,
  StatCard,
  SectionTitle,
  DataTable,
  Badge,
  Button,
  SegmentedControl,
  Drawer,
  Skeleton,
  EmptyState,
  ErrorState,
  Notice,
  AreaChart,
  useWidth,
  type Column,
  type SortSpec,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { usePeriodControls } from '../lib/use-period-controls'
import { usePeriodResource } from '../lib/use-period-resource'
import { getDaily, getDailyDimension, getMessages, getMessageDetail } from '../lib/api'
import {
  formatCompactCurrency,
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCostProvenance,
  formatCurrencyWithProvenance,
  formatDateTime,
  formatInteger,
  formatShortDate,
  formatTokenCount,
  safeDivide,
} from '../lib/format'
import { getNextSortState, type SortState } from '../lib/table-sort'
import { groupModelDaysByDate } from '../lib/daily-models'
import { getTokenBreakdownItems, getTokenTotal } from '../lib/token-breakdown'
import {
  getDetailLoadingCopy,
  getDetailTitle,
  getEmptyHistoryCopy,
  getHistoryTitle,
} from '../lib/message-display'
import type {
  DayStats,
  MessageDetail,
  MessageEntry,
  MessageList,
  SourceID,
  ToolPart,
} from '../types/api'

const REQUESTS_PAGE_SIZE = 12
const DAILY_MODELS_SHOWN = 3

// Period-resource fetcher for the per-day model breakdown (daily?dimension=model).
function getDailyModels(period: string, signal?: AbortSignal, sourceId?: SourceID) {
  return getDailyDimension('model', period, signal, sourceId)
}

// ── Daily metric lens (inlined from components/daily/daily-metrics.ts) ──
type DailyMetric = 'cost' | 'requests' | 'tokens'

const METRIC_OPTS: { value: DailyMetric; label: string }[] = [
  { value: 'cost', label: 'Cost' },
  { value: 'requests', label: 'Messages' },
  { value: 'tokens', label: 'Tokens' },
]

interface DailyMetricMeta {
  label: string
  color: string
  cardTitle: string
  cardSubtitle: string
  yFormat: (value: number) => string
}

function getDailyMetricMeta(metric: DailyMetric): DailyMetricMeta {
  switch (metric) {
    case 'cost':
      return {
        label: 'Cost',
        color: 'var(--cat-1)',
        cardTitle: 'Daily spend',
        cardSubtitle: 'Zero-filled buckets stay visible so gaps in the window are honest.',
        yFormat: (v) => formatCompactCurrency(v),
      }
    case 'requests':
      return {
        label: 'Messages',
        color: 'var(--cat-1)',
        cardTitle: 'Daily messages',
        cardSubtitle: 'Message count per bucket — the available daily throughput signal.',
        yFormat: (v) => formatCompactInteger(v),
      }
    case 'tokens':
      return {
        label: 'Tokens',
        color: 'var(--cat-1)',
        cardTitle: 'Daily tokens',
        cardSubtitle: 'Total token volume per bucket across input, cache, output, and reasoning.',
        yFormat: (v) => formatTokenCount(v),
      }
  }
}

function getDailyMetricValue(day: DayStats, metric: DailyMetric): number {
  switch (metric) {
    case 'requests':
      return day.messages
    case 'tokens':
      return getTokenTotal(day.tokens)
    default:
      return day.cost
  }
}

function hasActivity(day: DayStats): boolean {
  return day.sessions > 0 || day.messages > 0 || day.cost > 0 || getTokenTotal(day.tokens) > 0
}

// ── Requests-history ledger sorting ──
type RequestsSortKey = 'time' | 'cost' | 'tokens'

const REQUESTS_SORT_DEFAULTS: Record<RequestsSortKey, 'asc' | 'desc'> = {
  time: 'desc',
  cost: 'desc',
  tokens: 'desc',
}

function getRoleTone(role: string) {
  switch (role) {
    case 'assistant':
      return 'accent' as const
    case 'user':
      return 'success' as const
    case 'system':
      return 'warning' as const
    default:
      return 'neutral' as const
  }
}

function getMessageSessionLabel(message: Pick<MessageEntry, 'session_title'>) {
  return message.session_title || 'Untitled session'
}

export function DailyView() {
  const { requestRefresh, refreshNonce, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const sourceLabel = selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  const [searchParams, setSearchParams] = useSearchParams()
  const [metric, setMetric] = useState<DailyMetric>('cost')

  // ── Daily stats (period resource) ──
  const { cacheKey } = usePeriodControls()
  const period = cacheKey
  const { data, loading, error } = usePeriodResource(getDaily, cacheKey)

  // Per-day model message counts for the breakdown table. Loads independently;
  // the table renders without the Models column data until it arrives.
  const { data: modelDaily } = usePeriodResource(getDailyModels, cacheKey)

  const modelsByDate = useMemo(() => groupModelDaysByDate(modelDaily?.days), [modelDaily])

  // ── Message ledger (own page/sort state; page mirrors the URL) ──
  const [messages, setMessages] = useState<MessageList | null>(null)
  const [messagesLoading, setMessagesLoading] = useState(true)
  const [messagesError, setMessagesError] = useState<string | null>(null)
  const [messagesSort, setMessagesSort] = useState<SortState<RequestsSortKey> | null>(null)
  const [selectedMessageId, setSelectedMessageId] = useState<string | null>(null)

  const pageFromUrl = parseInt(searchParams.get('page') ?? '1', 10)
  const messagesPage = isNaN(pageFromUrl) || pageFromUrl < 1 ? 1 : pageFromUrl

  const setMessagesPage = (next: number) => {
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev)
      n.set('page', String(next))
      return n
    })
  }

  const resetLedger = () => {
    setMessagesSort(null)
    setSelectedMessageId(null)
    setMessages(null)
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev)
      n.set('page', '1')
      return n
    }, { replace: true })
  }

  // Reset pagination/sort/selection when the source changes.
  const previousSourceRef = useRef(selectedSourceId)
  useEffect(() => {
    if (previousSourceRef.current === selectedSourceId) return
    previousSourceRef.current = selectedSourceId
    resetLedger()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedSourceId])

  // The period picker is global — reset local ledger state on range change.
  const previousPeriodRef = useRef(period)
  useEffect(() => {
    if (previousPeriodRef.current === period) return
    previousPeriodRef.current = period
    resetLedger()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [period])

  // Fetch the message ledger (separate shape from daily stats).
  useEffect(() => {
    const controller = new AbortController()

    async function loadMessages() {
      setMessagesError(null)
      setMessagesLoading(true)
      try {
        const sortParam = messagesSort ? `${messagesSort.key}:${messagesSort.direction}` : undefined
        const next = await getMessages(period, messagesPage, REQUESTS_PAGE_SIZE, sortParam, controller.signal, selectedSourceId)
        setMessages(next)
      } catch (caught) {
        if (controller.signal.aborted) return
        setMessagesError(caught instanceof Error ? caught.message : 'Failed to load messages history')
      } finally {
        if (!controller.signal.aborted) setMessagesLoading(false)
      }
    }

    void loadMessages()
    return () => controller.abort()
  }, [messagesPage, messagesSort, period, refreshNonce, selectedSourceId])

  const handleSortChange = (key: string) => {
    const k = key as RequestsSortKey
    setMessagesSort((current) => getNextSortState(current, k, REQUESTS_SORT_DEFAULTS[k]))
    setMessagesPage(1)
  }

  // ── Daily summary derived from real data ──
  const summary = useMemo(() => {
    if (!data) return null

    const isHourly = data.granularity === 'hour'
    const totals = data.days.reduce(
      (acc, day) => {
        acc.sessions += day.sessions
        acc.messages += day.messages
        acc.cost += day.cost
        acc.tokens += getTokenTotal(day.tokens)
        if (hasActivity(day)) acc.activeDays += 1
        return acc
      },
      { sessions: 0, messages: 0, cost: 0, tokens: 0, activeDays: 0 },
    )

    return {
      ...totals,
      isHourly,
      bucketCount: data.days.length,
      averageMessagesPerSession: safeDivide(totals.messages, totals.sessions),
      averageTokensPerBucket: safeDivide(totals.tokens, data.days.length),
      recentDays: [...data.days].reverse(),
      empty: totals.activeDays === 0,
    }
  }, [data])

  const metricMeta = getDailyMetricMeta(metric)

  // ── Chart series (labels respect hourly granularity) ──
  const chart = useMemo(() => {
    const days = data?.days ?? []
    return {
      labels: days.map((d) => formatShortDate(d.date)),
      values: days.map((d) => getDailyMetricValue(d, metric)),
    }
  }, [data?.days, metric])

  const [chartRef, chartWidth] = useWidth(720)

  const handleRetry = () => requestRefresh()

  // ── Loading skeleton (mirrors overview-view) ──
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
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 280 }} />
      </div>
    )
  }

  if (!data) {
    return <Card><ErrorState title="Daily trends failed to load" message={error ?? undefined} onRetry={handleRetry} /></Card>
  }

  const bucketUnit = summary?.isHourly ? 'hour' : 'day'
  const bucketUnitPlural = summary?.isHourly ? 'hours' : 'days'

  // ── Daily breakdown columns ──
  const dailyCols: Column<DayStats>[] = [
    {
      key: 'date',
      header: 'Date',
      render: (d) => <span style={{ color: hasActivity(d) ? 'var(--fg-primary)' : 'var(--fg-faint)' }}>{formatShortDate(d.date)}</span>,
    },
    { key: 'sessions', header: 'Sessions', numeric: true, render: (d) => formatInteger(d.sessions) },
    { key: 'messages', header: 'Messages', numeric: true, render: (d) => formatInteger(d.messages) },
    // Per-model message history — only at day granularity since the
    // dimension endpoint buckets by day, not hour.
    ...(data.granularity !== 'hour'
      ? [
          {
            key: 'models',
            header: 'Models · messages',
            wrap: true,
            render: (d: DayStats) => {
              const rows = modelsByDate.get(d.date) ?? []
              if (rows.length === 0) return <span style={{ color: 'var(--fg-faint)' }}>—</span>
              const shown = rows.slice(0, DAILY_MODELS_SHOWN)
              const hidden = rows.length - shown.length
              const fullBreakdown = rows.map((r) => `${r.dimension_key}: ${formatInteger(r.messages)} messages`).join('\n')
              return (
                <span title={fullBreakdown} style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
                  {shown.map((r) => (
                    <span key={r.dimension_key} style={{ display: 'flex', alignItems: 'baseline', gap: 6, minWidth: 0 }}>
                      <span style={{ font: '400 11px/1.3 var(--font-mono)', color: 'var(--fg-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{r.dimension_key}</span>
                      <span style={{ font: '600 11px/1.3 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{formatInteger(r.messages)}</span>
                    </span>
                  ))}
                  {hidden > 0 && <span style={{ font: '400 11px/1.3 var(--font-ui)', color: 'var(--fg-faint)' }}>+{hidden} more</span>}
                </span>
              )
            },
          } satisfies Column<DayStats>,
        ]
      : []),
    {
      key: 'tokens',
      header: 'Tokens',
      numeric: true,
      render: (d) => {
        const total = getTokenTotal(d.tokens)
        return <span title={total > 0 ? `${formatInteger(total)} tokens` : undefined}>{formatTokenCount(total)}</span>
      },
    },
    {
      key: 'cost',
      header: 'Est. cost',
      numeric: true,
      render: (d) => formatCompactCurrencyWithProvenance(d.cost, d.cost_status, d.cost_provenance),
    },
  ]

  // ── Requests-history columns ──
  const requestCols: Column<MessageEntry>[] = [
    {
      key: 'role',
      header: 'Role',
      width: 96,
      render: (m) => <Badge tone={getRoleTone(m.role)}>{m.role || 'unknown'}</Badge>,
    },
    {
      key: 'time',
      header: 'Time',
      sortable: true,
      render: (m) => <span style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-secondary)', fontVariantNumeric: 'tabular-nums' }}>{formatDateTime(m.time_created)}</span>,
    },
    {
      key: 'session',
      header: 'Session',
      wrap: true,
      render: (m) => (
        <span style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
          <span style={{ font: '500 13px/1.3 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{getMessageSessionLabel(m)}</span>
          {m.model_id && <span style={{ font: '400 11px/1 var(--font-mono)', color: 'var(--fg-faint)' }}>{m.model_id}</span>}
        </span>
      ),
    },
    {
      key: 'cost',
      header: 'Cost',
      numeric: true,
      sortable: true,
      render: (m) => formatCompactCurrencyWithProvenance(m.cost ?? 0, m.cost_status, m.cost_provenance),
    },
    {
      key: 'tokens',
      header: 'Tokens',
      numeric: true,
      sortable: true,
      render: (m) => {
        const total = m.tokens ? getTokenTotal(m.tokens) : 0
        return total > 0 ? <span title={`${formatInteger(total)} tokens`}>{formatTokenCount(total)}</span> : <span style={{ color: 'var(--fg-faint)' }}>—</span>
      },
    },
  ]

  const requestSort: SortSpec | null = messagesSort ? { key: messagesSort.key, dir: messagesSort.direction } : null

  const total = messages?.total ?? 0
  const currentPage = messages?.page ?? messagesPage
  const pageSize = messages?.page_size ?? REQUESTS_PAGE_SIZE
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const firstVisible = total === 0 ? 0 : (currentPage - 1) * pageSize + 1
  const lastVisible = total === 0 ? 0 : firstVisible + (messages?.messages.length ?? 0) - 1
  const emptyLedgerCopy = getEmptyHistoryCopy(selectedSourceId, sourceLabel)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && <Notice tone="warning" title="Daily trends partially loaded">{error}</Notice>}

      {/* KPI row */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
        <StatCard
          accent
          label="Total spend"
          value={formatCurrencyWithProvenance(summary?.cost ?? 0, data.cost_status, data.cost_provenance)}
          hint={formatCostProvenance(data.cost_status, data.cost_provenance) ?? `${formatCompactInteger(summary?.bucketCount ?? 0)} ${bucketUnitPlural} in window`}
        />
        <StatCard
          label="Sessions"
          value={formatInteger(summary?.sessions ?? 0)}
          hint={`${formatCompactInteger(summary?.activeDays ?? 0)} active ${bucketUnitPlural} in window`}
        />
        <StatCard
          label="Messages"
          value={formatInteger(summary?.messages ?? 0)}
          hint={`${(summary?.averageMessagesPerSession ?? 0).toFixed(1)} avg / session`}
        />
        <StatCard
          label="Tokens"
          value={formatTokenCount(summary?.tokens ?? 0)}
          title={summary ? `${formatInteger(summary.tokens)} tokens` : undefined}
          hint={`${formatTokenCount(Math.round(summary?.averageTokensPerBucket ?? 0))} avg / ${bucketUnit}`}
        />
      </div>

      {/* Primary trend chart */}
      <Card
        title={metricMeta.cardTitle}
        subtitle={metricMeta.cardSubtitle}
        action={<SegmentedControl size="sm" options={METRIC_OPTS} value={metric} onChange={setMetric} />}
      >
        <div ref={chartRef} style={{ minWidth: 0 }}>
          {chart.labels.length > 0 ? (
            <AreaChart
              labels={chart.labels}
              series={[{ name: metricMeta.label, color: metricMeta.color, data: chart.values, fmt: metricMeta.yFormat }]}
              width={Math.max(320, chartWidth)}
              height={260}
              yFormat={metricMeta.yFormat}
            />
          ) : (
            <div style={{ height: 260, display: 'flex', alignItems: 'center', justifyContent: 'center', font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>No trend data for this range.</div>
          )}
        </div>
      </Card>

      {/* Daily breakdown table */}
      <div>
        <SectionTitle sub={`${formatCompactInteger(summary?.bucketCount ?? 0)} ${bucketUnitPlural} · most recent first`}>Daily breakdown</SectionTitle>
        {summary?.empty ? (
          <Card>
            <EmptyState
              icon="calendar"
              title={`No ${bucketUnit} activity yet`}
              description={
                selectedSourceId === 'claude_code'
                  ? 'No persisted Claude Code transcript activity was found in this selected window.'
                  : `Zero-filled window until ${sourceLabel} records sessions and messages in this period.`
              }
            />
          </Card>
        ) : (
          <DataTable columns={dailyCols} rows={summary?.recentDays ?? []} rowKey={(d) => d.date} dense />
        )}
      </div>

      {/* Requests history ledger */}
      <div>
        <SectionTitle sub="Always-on drill-down — open any row for verbose request content.">{getHistoryTitle(selectedSourceId)}</SectionTitle>

        {messagesError && (
          <div style={{ marginBottom: 12 }}>
            <Notice tone="danger" title="Messages history failed to load">
              {messagesError}
              <div style={{ marginTop: 8 }}>
                <Button variant="secondary" size="sm" iconLeft="refresh" onClick={handleRetry}>Retry messages</Button>
              </div>
            </Notice>
          </div>
        )}

        {messagesLoading && !messages ? (
          <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', padding: 16 }}>
            {Array.from({ length: 5 }).map((_, i) => (
              <div key={i} style={{ display: 'flex', gap: 16, padding: '10px 0', borderBottom: i < 4 ? '1px solid var(--border-subtle)' : 'none' }}>
                <Skeleton width={64} height={18} />
                <Skeleton width={120} height={18} />
                <Skeleton width={200} height={18} />
                <Skeleton width={60} height={18} />
              </div>
            ))}
          </div>
        ) : (messages?.messages.length ?? 0) === 0 ? (
          <Card><EmptyState icon="message-square" title="No requests in window" description={emptyLedgerCopy} /></Card>
        ) : (
          <>
            <DataTable
              columns={requestCols}
              rows={messages?.messages ?? []}
              rowKey={(m) => m.id}
              sort={requestSort}
              onSort={handleSortChange}
              onRowClick={(m) => setSelectedMessageId(m.id)}
            />
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', flexWrap: 'wrap', gap: 12, marginTop: 12, padding: '10px 14px', background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)' }}>
              <span style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>
                Showing <span style={{ color: 'var(--fg-primary)', fontFamily: 'var(--font-mono)' }}>{firstVisible}–{lastVisible}</span> of <span style={{ color: 'var(--fg-primary)', fontFamily: 'var(--font-mono)' }}>{formatInteger(total)}</span> messages
              </span>
              <div style={{ display: 'flex', gap: 8 }}>
                <Button variant="secondary" size="sm" iconLeft="chevron-left" disabled={currentPage <= 1 || messagesLoading} onClick={() => setMessagesPage(Math.max(1, currentPage - 1))}>Previous</Button>
                <Button variant="secondary" size="sm" disabled={currentPage >= totalPages || messagesLoading} onClick={() => setMessagesPage(currentPage + 1)}>Next</Button>
              </div>
            </div>
          </>
        )}
      </div>

      <MessageDetailDrawer
        messageId={selectedMessageId}
        onClose={() => setSelectedMessageId(null)}
      />
    </div>
  )
}

// ── Message detail Drawer (source-scoped detail; manages its own fetch) ──
function MessageDetailDrawer({ messageId, onClose }: { messageId: string | null; onClose: () => void }) {
  const { selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [detail, setDetail] = useState<MessageDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)
  const [detailNonce, setDetailNonce] = useState(0)

  useEffect(() => {
    if (!messageId) {
      setDetail(null)
      setDetailError(null)
      setDetailLoading(false)
      return
    }

    const activeId = messageId
    const controller = new AbortController()

    async function load() {
      setDetail(null)
      setDetailError(null)
      setDetailLoading(true)
      try {
        setDetail(await getMessageDetail(activeId, controller.signal, selectedSourceId))
      } catch (caught) {
        if (controller.signal.aborted) return
        setDetailError(caught instanceof Error ? caught.message : 'Failed to load request detail')
      } finally {
        if (!controller.signal.aborted) setDetailLoading(false)
      }
    }

    void load()
    return () => controller.abort()
  }, [detailNonce, messageId, selectedSourceId])

  const tokenTotal = detail?.tokens ? getTokenTotal(detail.tokens) : 0
  const textParts = detail?.content.text_parts ?? []
  const reasoningParts = detail?.content.reasoning_parts ?? []
  const toolParts = detail?.content.tool_parts ?? []
  const sourceLabel = selectedSourceInfo?.label ?? selectedSourceId

  return (
    <Drawer
      open={messageId !== null}
      onClose={onClose}
      width={680}
      title={getDetailTitle(selectedSourceId, !detail)}
      subtitle={detail ? `id ${detail.id.slice(0, 16)}` : undefined}
    >
      {detailLoading ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Skeleton width="60%" height={20} />
          <Skeleton width="100%" height={80} />
          <Skeleton width="100%" height={120} />
        </div>
      ) : detailError ? (
        <ErrorState title="Request detail failed to load" message={detailError} onRetry={() => setDetailNonce((n) => n + 1)} />
      ) : detail ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {/* Header facts */}
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
            <Badge tone={getRoleTone(detail.role)}>{detail.role || 'unknown'}</Badge>
            <Badge>{detail.session_title || 'Untitled session'}</Badge>
            <Badge>{sourceLabel}</Badge>
          </div>

          {/* Detail metrics */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 10 }}>
            <DetailMetric
              label="Request spend"
              value={formatCurrencyWithProvenance(detail.cost ?? 0, detail.cost_status, detail.cost_provenance)}
              hint={formatCostProvenance(detail.cost_status, detail.cost_provenance) ?? (detail.model_id || 'model unavailable')}
            />
            <DetailMetric
              label="Token load"
              value={formatTokenCount(tokenTotal)}
              title={`${formatInteger(tokenTotal)} tokens`}
              hint={detail.tokens ? `${formatTokenCount(detail.tokens.input)} in · ${formatTokenCount(detail.tokens.output)} out` : 'No token telemetry'}
            />
            <DetailMetric
              label="Recorded at"
              value={formatDateTime(detail.time_created)}
              hint={`Session ${detail.session_id.slice(0, 12)}`}
            />
          </div>

          {/* Token mix */}
          {detail.tokens && tokenTotal > 0 && (
            <Card title="Token mix" pad={14}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                {getTokenBreakdownItems(detail.tokens).filter((i) => i.value > 0).map((i) => (
                  <div key={i.key} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, font: '400 12px/1 var(--font-ui)', color: 'var(--fg-secondary)' }}>
                      <span style={{ width: 9, height: 9, borderRadius: 2, background: i.color }} />
                      {i.label}
                    </span>
                    <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{formatTokenCount(i.value)}</span>
                  </div>
                ))}
              </div>
            </Card>
          )}

          {/* Content sections */}
          <ContentSection title="Message text" emptyCopy="No normal text preview was returned." parts={textParts.map((p) => p.text)} />
          <ContentSection title="Reasoning" emptyCopy="No reasoning preview was returned." parts={reasoningParts.map((p) => p.text)} tone="accent" />
          <ToolSection parts={toolParts} />

          <div style={{ font: '400 11px/1.5 var(--font-ui)', color: 'var(--fg-faint)' }}>{getDetailLoadingCopy(selectedSourceId)}</div>
        </div>
      ) : null}
    </Drawer>
  )
}

function DetailMetric({ label, value, hint, title }: { label: string; value: string; hint: string; title?: string }) {
  return (
    <div title={title} style={{ background: 'var(--ink-850)', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '10px 12px' }}>
      <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)' }}>{label}</div>
      <div style={{ font: '600 15px/1.2 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums', marginTop: 6 }}>{value}</div>
      <div style={{ font: '400 11px/1.4 var(--font-ui)', color: 'var(--fg-faint)', marginTop: 5 }}>{hint}</div>
    </div>
  )
}

function ContentSection({ title, parts, emptyCopy, tone }: { title: string; parts: string[]; emptyCopy: string; tone?: 'accent' }) {
  return (
    <Card title={title} subtitle={`${formatInteger(parts.length)} parts`} pad={14}>
      {parts.length === 0 ? (
        <div style={{ font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)' }}>{emptyCopy}</div>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {parts.map((text, i) => (
            <div
              key={i}
              style={{
                background: 'var(--ink-850)',
                border: `1px solid ${tone === 'accent' ? 'var(--border-accent)' : 'var(--border-subtle)'}`,
                borderRadius: 'var(--radius-md)',
                padding: '10px 12px',
                font: '400 12px/1.6 var(--font-ui)',
                color: 'var(--fg-secondary)',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
              }}
            >
              {text}
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}

function ToolSection({ parts }: { parts: ToolPart[] }) {
  if (parts.length === 0) return null

  const toolTone = (status?: string) => {
    switch (status) {
      case 'completed':
        return 'success' as const
      case 'error':
        return 'danger' as const
      case 'running':
        return 'accent' as const
      case 'pending':
        return 'warning' as const
      default:
        return 'neutral' as const
    }
  }

  const summarize = (part: ToolPart): string | null => {
    const s = part.state
    if (s.status === 'error' && s.error) return s.error.slice(0, 240)
    if (s.status === 'completed' && s.output) return s.output.slice(0, 240)
    if (s.title) return s.title.slice(0, 240)
    return null
  }

  return (
    <Card title="Tool activity" subtitle={`${formatInteger(parts.length)} parts`} pad={14}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {parts.map((part, i) => {
          const summary = summarize(part)
          return (
            <div key={part.call_id || `${part.tool}-${i}`} style={{ background: 'var(--ink-850)', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: '10px 12px' }}>
              <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
                <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-primary)' }}>{part.tool || 'unknown-tool'}</span>
                <Badge tone={toolTone(part.state.status)}>{part.state.status || 'unknown'}</Badge>
              </div>
              {summary && (
                <div style={{ marginTop: 8, font: '400 12px/1.5 var(--font-ui)', color: part.state.status === 'error' ? 'var(--danger)' : 'var(--fg-secondary)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{summary}</div>
              )}
            </div>
          )
        })}
      </div>
    </Card>
  )
}

/* Sessions — list + full-page detail (Vael). The old Radix Sheet drawer is replaced
   by an in-view full-page swap keyed on a ?session=<id> URL param for deep-linking.
   No fabricated data: duration is time_updated − time_created, cache-hit is derived
   from total_tokens, and costs always use the *WithProvenance helpers. */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Card,
  StatCard,
  DataTable,
  VendorChip,
  Badge,
  Legend,
  Button,
  Icon,
  Tabs,
  Skeleton,
  EmptyState,
  ErrorState,
  Notice,
  type Column,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { getSessionDetail, getSessionsWithFilter } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { usePeriodControls } from '../lib/use-period-controls'
import { getTokenTotal } from '../lib/token-breakdown'
import {
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCurrencyWithProvenance,
  formatDateTime,
  formatInteger,
  formatRelativeTime,
  formatTokenCount,
  safeDivide,
} from '../lib/format'
import type {
  SessionDetail,
  SessionEntry,
  SessionList,
  SessionMessage,
  SourceID,
} from '../types/api'

const PAGE_SIZE = 12
const SEARCH_DEBOUNCE_MS = 300

function getSessionLabel(session: Pick<SessionEntry, 'title'>) {
  return session.title || 'Untitled session'
}

function getSessionProjectLabel(
  session: Pick<SessionEntry, 'project_name' | 'project_id'>,
) {
  return session.project_name || session.project_id || 'No linked project'
}

// Duration between created/updated, e.g. "4m 12s" / "2h 30m" / "3d 4h".
function fmtDur(createdAt: string, updatedAt: string): string {
  const ms = new Date(updatedAt).getTime() - new Date(createdAt).getTime()
  const totalSec = Math.max(0, Math.round(ms / 1000))
  if (totalSec < 1) return '0s'
  const d = Math.floor(totalSec / 86400)
  const h = Math.floor((totalSec % 86400) / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const s = totalSec % 60
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

export function SessionsView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const sourceLabel =
    selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  const { cacheKey } = usePeriodControls()
  const rawFilter = searchParams.get('filter') ?? ''
  const projectId = searchParams.get('project_id') ?? undefined
  const selectedSessionId = searchParams.get('session')

  const pageFromUrl = parseInt(searchParams.get('page') ?? '1', 10)
  const page = isNaN(pageFromUrl) || pageFromUrl < 1 ? 1 : pageFromUrl

  // ── Reset list + close detail when the source changes ──
  const previousSourceRef = useRef(selectedSourceId)
  useEffect(() => {
    if (previousSourceRef.current === selectedSourceId) return
    previousSourceRef.current = selectedSourceId
    setSearchParams(
      (prev) => {
        const n = new URLSearchParams(prev)
        n.set('page', '1')
        n.delete('session')
        return n
      },
      { replace: true },
    )
  }, [selectedSourceId, setSearchParams])

  // Normalize URL params on mount
  useEffect(() => {
    if (!searchParams.has('page')) {
      setSearchParams((prev) => {
        const n = new URLSearchParams(prev)
        n.set('page', '1')
        return n
      })
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const setPage = (updater: number | ((prev: number) => number)) => {
    const np = typeof updater === 'function' ? updater(page) : updater
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev)
      n.set('page', String(np))
      return n
    })
  }

  const openSession = (id: string) =>
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev)
      n.set('session', id)
      return n
    })

  const closeSession = () =>
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev)
      n.delete('session')
      return n
    })

  // ── Sessions list via usePeriodResource ──
  // Sessions has extra query dimensions (page, filter, projectId) beyond period.
  // cachePeriods: false + a stable fetcher reading latest values via ref; a version
  // string triggers requestRefresh when those dimensions change.
  const sessionQueryRef = useRef({ page, filter: rawFilter, projectId })
  sessionQueryRef.current = { page, filter: rawFilter, projectId }

  const fetcher = useCallback((p: string, signal?: AbortSignal, sourceId?: SourceID) => {
    const q = sessionQueryRef.current
    return getSessionsWithFilter(q.page, PAGE_SIZE, p, q.filter || undefined, q.projectId, signal, sourceId)
  }, [])

  const { data, loading, error } = usePeriodResource<SessionList>(fetcher, cacheKey, { cachePeriods: false })

  const sessionVersion = `${page}:${rawFilter}:${projectId ?? ''}`
  const lastVersionRef = useRef(sessionVersion)
  useEffect(() => {
    if (sessionVersion !== lastVersionRef.current) {
      lastVersionRef.current = sessionVersion
      requestRefresh()
    }
  }, [sessionVersion, requestRefresh])

  // ── Search/filter with 300ms debounce ──
  const [searchText, setSearchText] = useState(rawFilter)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const handleSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const v = e.target.value
    setSearchText(v)
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    debounceTimerRef.current = setTimeout(() => {
      setSearchParams((prev) => {
        const n = new URLSearchParams(prev)
        n.set('page', '1')
        if (v) n.set('filter', v)
        else n.delete('filter')
        return n
      })
    }, SEARCH_DEBOUNCE_MS)
  }
  useEffect(() => () => { if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current) }, [])

  // ── Page summary ──
  const summary = useMemo(() => {
    if (!data) return null
    const totalPages = Math.max(1, Math.ceil(data.total / data.page_size))
    const firstVisible = data.total === 0 ? 0 : (data.page - 1) * data.page_size + 1
    const lastVisible = data.total === 0 ? 0 : firstVisible + data.sessions.length - 1
    const visibleCost = data.sessions.reduce((a, s) => a + s.cost, 0)
    const visibleMessages = data.sessions.reduce((a, s) => a + s.message_count, 0)
    const visibleProjects = new Set(data.sessions.map((s) => getSessionProjectLabel(s))).size
    return {
      totalPages,
      firstVisible,
      lastVisible,
      visibleCost,
      visibleMessages,
      visibleProjects,
      total: data.total,
      pageSize: data.page_size,
      page: data.page,
      empty: data.sessions.length === 0,
      costStatus: data.cost_status,
      costProvenance: data.cost_provenance,
    }
  }, [data])

  // ── Detail full-page swap ──
  if (selectedSessionId) {
    return (
      <SessionDetailScreen
        id={selectedSessionId}
        sourceId={selectedSourceId}
        sourceLabel={sourceLabel}
        onBack={closeSession}
      />
    )
  }

  // ── List loading ──
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
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 340 }} />
      </div>
    )
  }

  if (!data && error) {
    return <Card><ErrorState title="Sessions failed to load" message={error} onRetry={requestRefresh} /></Card>
  }

  const columns: Column<SessionEntry>[] = [
    {
      key: 'id',
      header: 'Session',
      width: 240,
      render: (s) => (
        <span style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 0 }}>
          <span style={{ font: '600 13px/1.3 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {getSessionLabel(s)}
          </span>
          <span style={{ font: '400 11px/1 var(--font-mono)', color: 'var(--fg-faint)' }} title={s.id}>
            id {s.id.slice(0, 12)}
          </span>
        </span>
      ),
    },
    {
      key: 'source',
      header: 'Source',
      render: (s) => <VendorChip id={s.source_id ?? selectedSourceId} />,
    },
    {
      key: 'project',
      header: 'Project',
      render: (s) => (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, font: '400 12px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>
          <Icon name="folder" size={13} />
          <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 200 }}>
            {getSessionProjectLabel(s)}
          </span>
        </span>
      ),
    },
    {
      key: 'started',
      header: 'Started',
      render: (s) => (
        <span title={formatDateTime(s.time_created)} style={{ color: 'var(--fg-muted)', font: '400 12px/1 var(--font-mono)' }}>
          {formatRelativeTime(new Date(s.time_created))}
        </span>
      ),
    },
    {
      key: 'duration',
      header: 'Duration',
      numeric: true,
      render: (s) => fmtDur(s.time_created, s.time_updated),
    },
    {
      key: 'messages',
      header: 'Messages',
      numeric: true,
      render: (s) => formatCompactInteger(s.message_count),
    },
    {
      key: 'cost',
      header: 'Est. cost',
      numeric: true,
      render: (s) => formatCompactCurrencyWithProvenance(s.cost, s.cost_status ?? data?.cost_status, s.cost_provenance ?? data?.cost_provenance),
    },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && <Notice tone="warning" title="Sessions partially loaded">{error}</Notice>}

      {/* Search */}
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
        <div style={{ position: 'relative', flex: '0 1 320px', minWidth: 200 }}>
          <span style={{ position: 'absolute', left: 12, top: '50%', transform: 'translateY(-50%)', display: 'inline-flex', color: 'var(--fg-faint)', pointerEvents: 'none' }}>
            <Icon name="search" size={15} />
          </span>
          <input
            type="text"
            placeholder="Search sessions…"
            value={searchText}
            onChange={handleSearchChange}
            style={{
              width: '100%',
              height: 36,
              padding: '0 12px 0 34px',
              background: 'var(--ink-800)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-md)',
              color: 'var(--fg-primary)',
              font: '400 13px/1 var(--font-ui)',
              outline: 'none',
            }}
          />
        </div>
        {rawFilter && (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>
            Filtering
            <code style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-secondary)', background: 'var(--ink-700)', padding: '3px 6px', borderRadius: 'var(--radius-sm)' }}>{rawFilter}</code>
          </span>
        )}
      </div>

      {/* KPI summary */}
      {summary && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
          <StatCard
            accent
            label="Total sessions"
            value={formatInteger(summary.total)}
            hint={summary.empty ? 'No sessions in range' : `${formatInteger(summary.totalPages)} pages · ${formatInteger(summary.pageSize)} / page`}
          />
          <StatCard
            label="Visible window"
            value={summary.empty ? '0' : `${summary.firstVisible}–${summary.lastVisible}`}
            hint={summary.empty ? 'Nothing to paginate yet' : `Page ${formatInteger(summary.page)} of ${formatInteger(summary.totalPages)}`}
          />
          <StatCard
            label="Visible cost"
            value={formatCurrencyWithProvenance(summary.visibleCost, summary.costStatus, summary.costProvenance)}
            hint={`${formatCompactInteger(summary.visibleMessages)} messages on this page`}
          />
          <StatCard
            label="Projects on page"
            value={formatInteger(summary.visibleProjects)}
            hint={summary.empty ? 'Awaiting session activity' : 'distinct projects visible'}
          />
        </div>
      )}

      {/* Table / empty */}
      {summary?.empty ? (
        <Card>
          <EmptyState
            icon="folder"
            title="No sessions recorded yet"
            description={
              selectedSourceId === 'claude_code'
                ? 'No persisted Claude Code sessions were found in readable local transcripts for this window.'
                : `This view stays empty until ${sourceLabel} contains session rows for this range.`
            }
          />
        </Card>
      ) : (
        <Card
          title="Session index"
          subtitle={summary ? `${formatInteger(summary.total)} total · click a row to inspect` : undefined}
          action={summary ? <Badge>Page {data?.page ?? 1}</Badge> : undefined}
          pad={0}
        >
          <DataTable<SessionEntry>
            columns={columns}
            rows={data?.sessions ?? []}
            rowKey={(s) => s.id}
            onRowClick={(s) => openSession(s.id)}
          />
          {summary && (
            <Pagination
              page={data?.page ?? 1}
              total={data?.total ?? 0}
              pageSize={data?.page_size ?? PAGE_SIZE}
              totalPages={summary.totalPages}
              firstVisible={summary.firstVisible}
              lastVisible={summary.lastVisible}
              onPageChange={setPage}
            />
          )}
        </Card>
      )}
    </div>
  )
}

// ── Pagination ────────────────────────────────────────────────
function Pagination({
  page,
  total,
  pageSize,
  totalPages,
  firstVisible,
  lastVisible,
  onPageChange,
}: {
  page: number
  total: number
  pageSize: number
  totalPages: number
  firstVisible: number
  lastVisible: number
  onPageChange: (updater: number | ((prev: number) => number)) => void
}) {
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 12, padding: '12px 16px', borderTop: '1px solid var(--border-default)' }}>
      <div style={{ font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)' }}>
        <span style={{ color: 'var(--fg-secondary)' }}>Showing {firstVisible}–{lastVisible}</span> of{' '}
        <span style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-secondary)', fontVariantNumeric: 'tabular-nums' }}>{formatInteger(total)}</span> sessions
        <span style={{ marginLeft: 8, font: '400 11px/1 var(--font-mono)', color: 'var(--fg-faint)' }}>
          page_size={pageSize}
        </span>
      </div>
      <div style={{ display: 'flex', gap: 8 }}>
        <Button variant="secondary" size="sm" iconLeft="chevron-left" disabled={page <= 1} onClick={() => onPageChange((c) => Math.max(1, c - 1))}>
          Previous
        </Button>
        <Button variant="secondary" size="sm" disabled={page >= totalPages} onClick={() => onPageChange((c) => c + 1)}>
          Next
        </Button>
      </div>
    </div>
  )
}

// ── Detail screen (full-page swap) ────────────────────────────
function SessionDetailScreen({
  id,
  sourceId,
  sourceLabel,
  onBack,
}: {
  id: string
  sourceId: SourceID
  sourceLabel: string
  onBack: () => void
}) {
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)
  const [tab, setTab] = useState<'timeline' | 'raw'>('timeline')

  useEffect(() => {
    const ctrl = new AbortController()
    async function load() {
      setDetail(null); setError(null); setLoading(true)
      try {
        setDetail(await getSessionDetail(id, ctrl.signal, sourceId))
      } catch (caught) {
        if (ctrl.signal.aborted) return
        setError(caught instanceof Error ? caught.message : 'Failed to load session detail')
      } finally {
        if (!ctrl.signal.aborted) setLoading(false)
      }
    }
    void load()
    return () => ctrl.abort()
  }, [id, sourceId, nonce])

  const stats = useMemo(() => {
    if (!detail) return null
    const t = detail.total_tokens
    const totalTokens = getTokenTotal(t)
    const cacheRead = t.cache.read
    const cacheDenom = t.input + cacheRead
    const cacheHit = cacheDenom > 0 ? Math.round(safeDivide(cacheRead, cacheDenom) * 100) : 0
    // Token composition: distinct buckets of total_tokens (no double-counting input/cache.read).
    const inputOnly = Math.max(0, t.input)
    const segments = [
      { key: 'input', label: 'Input', color: 'var(--cat-1)', value: inputOnly },
      { key: 'cache', label: 'Cached (read)', color: 'var(--cat-2)', value: Math.max(0, cacheRead) },
      { key: 'output', label: 'Output', color: 'var(--cat-3)', value: Math.max(0, t.output) },
    ].filter((s) => s.value > 0)
    const segTotal = segments.reduce((a, s) => a + s.value, 0) || 1
    return { totalTokens, cacheHit, segments, segTotal }
  }, [detail])

  const backButton = (
    <button
      type="button"
      onClick={onBack}
      style={{ display: 'inline-flex', alignItems: 'center', gap: 6, marginBottom: 14, background: 'transparent', border: 'none', color: 'var(--fg-muted)', font: '500 13px/1 var(--font-ui)', cursor: 'pointer', padding: 0 }}
    >
      <Icon name="chevron-left" size={16} /> All sessions
    </button>
  )

  if (loading) {
    return (
      <div>
        {backButton}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12, marginBottom: 12 }}>
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)', padding: 16 }}>
              <Skeleton width={90} height={11} />
              <Skeleton width={120} height={28} style={{ marginTop: 12 }} />
            </div>
          ))}
        </div>
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 320 }} />
      </div>
    )
  }

  if (error || !detail || !stats) {
    return (
      <div>
        {backButton}
        <Card>
          <ErrorState title="Session detail failed to load" message={error ?? undefined} onRetry={() => setNonce((c) => c + 1)} />
        </Card>
      </div>
    )
  }

  return (
    <div>
      {backButton}

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12, marginBottom: 16, flexWrap: 'wrap' }}>
        <VendorChip id={detail.source_id ?? sourceId} label={false} size={34} />
        <div style={{ minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
            <h2 style={{ margin: 0, font: '700 18px/1.2 var(--font-ui)', color: 'var(--fg-primary)' }}>
              {getSessionLabel(detail)}
            </h2>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 7, flexWrap: 'wrap', font: '400 12px/1 var(--font-mono)', color: 'var(--fg-muted)' }}>
            <span title={detail.id} style={{ color: 'var(--fg-faint)' }}>id {detail.id.slice(0, 14)}</span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}><Icon name="folder" size={13} />{getSessionProjectLabel(detail)}</span>
            <span>{sourceLabel}</span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}><Icon name="clock" size={13} />{formatDateTime(detail.time_created)}</span>
            <span>{fmtDur(detail.time_created, detail.time_updated)}</span>
          </div>
        </div>
      </div>

      {/* KPI */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12, marginBottom: 16 }}>
        <StatCard
          accent
          label="Total tokens"
          value={formatTokenCount(stats.totalTokens)}
          title={`${formatInteger(stats.totalTokens)} tokens`}
          hint={`${formatTokenCount(detail.total_tokens.input)} in · ${formatTokenCount(detail.total_tokens.output)} out`}
        />
        <StatCard
          label="Est. cost"
          value={formatCurrencyWithProvenance(detail.total_cost, detail.cost_status, detail.cost_provenance)}
          hint={detail.messages.length > 0 ? `${formatCurrencyWithProvenance(safeDivide(detail.total_cost, detail.messages.length), detail.cost_status, detail.cost_provenance)} / message` : 'this session'}
        />
        <StatCard
          label="Messages"
          value={formatInteger(detail.message_count)}
          hint={`${formatInteger(detail.messages.length)} recorded rows`}
        />
        <StatCard
          label="Cache hit"
          value={`${stats.cacheHit}%`}
          hint="read / (input + read)"
        />
      </div>

      {/* Token composition */}
      <Card title="Token composition" subtitle="Distinct buckets of total tokens" style={{ marginBottom: 16 }}>
        {stats.segments.length > 0 ? (
          <>
            <div style={{ display: 'flex', width: '100%', height: 12, borderRadius: 6, overflow: 'hidden', background: 'var(--ink-700)', gap: 1 }}>
              {stats.segments.map((seg) => (
                <div
                  key={seg.key}
                  title={`${seg.label}: ${formatInteger(seg.value)}`}
                  style={{ width: `${(seg.value / stats.segTotal) * 100}%`, background: seg.color }}
                />
              ))}
            </div>
            <div style={{ marginTop: 14 }}>
              <Legend items={stats.segments.map((seg) => ({ label: seg.label, color: seg.color, value: formatTokenCount(seg.value) }))} />
            </div>
          </>
        ) : (
          <div style={{ font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>No token activity recorded for this session.</div>
        )}
      </Card>

      {/* Tabs: Timeline / Raw JSON */}
      <Card pad={0}>
        <div style={{ padding: '0 16px' }}>
          <Tabs
            tabs={[
              { value: 'timeline', label: 'Timeline', count: detail.messages.length },
              { value: 'raw', label: 'Raw JSON' },
            ]}
            value={tab}
            onChange={setTab}
          />
        </div>
        <div style={{ padding: 16 }}>
          {tab === 'timeline' && (
            detail.messages.length === 0 ? (
              <Notice tone="info" title="No message rows">This session exists, but the detail endpoint returned no message rows.</Notice>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column' }}>
                {detail.messages.map((m, i) => (
                  <MessageRow key={m.id} message={m} last={i === detail.messages.length - 1} />
                ))}
              </div>
            )
          )}
          {tab === 'raw' && (
            <pre style={{ margin: 0, font: '400 12px/1.7 var(--font-mono)', color: 'var(--fg-secondary)', background: 'var(--ink-850)', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: 14, overflowX: 'auto', maxHeight: 520, overflowY: 'auto' }}>
              {JSON.stringify(detail, null, 2)}
            </pre>
          )}
        </div>
      </Card>
    </div>
  )
}

function roleTone(role: string): { color: string; dot: string } {
  switch (role) {
    case 'assistant':
      return { color: 'var(--blue-300)', dot: 'var(--accent)' }
    case 'user':
      return { color: 'var(--fg-muted)', dot: 'var(--fg-muted)' }
    case 'system':
      return { color: 'var(--amber-300, var(--cat-4))', dot: 'var(--cat-4)' }
    default:
      return { color: 'var(--fg-muted)', dot: 'var(--fg-faint)' }
  }
}

function MessageRow({ message, last }: { message: SessionMessage; last: boolean }) {
  const tone = roleTone(message.role)
  const tokens = message.tokens ? getTokenTotal(message.tokens) : 0
  const cost = message.cost ?? 0
  const meta = [message.model_id, message.provider_id, message.agent].filter(Boolean).join(' · ')

  return (
    <div style={{ display: 'grid', gridTemplateColumns: '16px minmax(0,1fr) auto', gap: 12, padding: '10px 0', borderBottom: last ? 'none' : '1px solid var(--border-subtle)' }}>
      <div style={{ display: 'flex', justifyContent: 'center' }}>
        <span style={{ width: 9, height: 9, borderRadius: '50%', background: tone.dot, marginTop: 5 }} />
      </div>
      <div style={{ minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <span style={{ font: '600 11px/1 var(--font-ui)', letterSpacing: '0.06em', textTransform: 'uppercase', color: tone.color }}>
            {message.role || 'unknown'}
          </span>
          {message.is_subagent && <Badge tone="accent">subagent</Badge>}
          <span style={{ font: '400 11px/1 var(--font-mono)', color: 'var(--fg-faint)' }} title={message.id}>
            {formatDateTime(message.time_created)}
          </span>
        </div>
        {meta && (
          <div style={{ marginTop: 4, font: '400 11px/1.4 var(--font-mono)', color: 'var(--fg-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '40ch' }}>
            {meta}
          </div>
        )}
      </div>
      <div style={{ textAlign: 'right', whiteSpace: 'nowrap', display: 'flex', flexDirection: 'column', gap: 4, alignItems: 'flex-end' }}>
        <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }} title={`${formatInteger(tokens)} tokens`}>
          {formatTokenCount(tokens)}
        </span>
        <span style={{ font: '400 11px/1 var(--font-mono)', color: 'var(--fg-muted)' }}>
          {formatCurrencyWithProvenance(cost, message.cost_status, message.cost_provenance)}
        </span>
      </div>
    </div>
  )
}

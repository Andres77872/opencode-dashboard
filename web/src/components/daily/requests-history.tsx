/* Message history — the paged list of individual messages under the Daily
   charts. Opening a row shows its detail. The page number is owned by the
   view (useHistoryPage) and mirrored in the URL. */
import { useEffect, useState } from 'react'
import {
  Badge,
  Button,
  Card,
  DataTable,
  EmptyState,
  ErrorState,
  Notice,
  Pagination,
  Skeleton,
  type Column,
  type SortSpec,
} from '../vael'
import { useDashboardContext } from '../layout/dashboard-context'
import { getMessages } from '../../lib/api'
import { useCachedResource } from '../../lib/use-cached-resource'
import {
  formatCompactCurrencyWithProvenance,
  formatCurrencyExact,
  formatDateTime,
  formatInteger,
  formatTokenCount,
  getCostLabel,
  isMissingCost,
} from '../../lib/format'
import { getEmptyHistoryCopy, getHistoryTitle, getRoleTone, getTotalRowLabel } from '../../lib/message-display'
import { usageUnavailableReasonDetail } from '../../lib/request-accounting'
import { getNextSortState, type SortState } from '../../lib/table-sort'
import { getTokenTotal } from '../../lib/token-breakdown'
import type { MessageEntry, MessageList } from '../../types/api'
import { RequestedTierBadge, UsageStatusBadge } from './request-badges'
import { RequestDetailDrawer } from './request-detail-drawer'
import type { HistoryPage } from './use-history-page'

const PAGE_SIZE = 12

type SortKey = 'time' | 'cost' | 'tokens'

const SORT_DEFAULTS: Record<SortKey, 'asc' | 'desc'> = { time: 'desc', cost: 'desc', tokens: 'desc' }
// What the API returns when no sort is sent; shown as the active column.
const DEFAULT_SORT: SortState<SortKey> = { key: 'time', direction: 'desc' }

const FAINT = { color: 'var(--fg-faint)' } as const

export interface RequestsHistoryProps {
  period: string
  sourceLabel: string
  /** Page state from useHistoryPage, owned by the view so it outlives this list. */
  history: HistoryPage
}

/**
 * Mount it with a `key` naming the source and range: sort and the open row
 * belong to one result set and start over with the next.
 */
export function RequestsHistory({ period, sourceLabel, history }: RequestsHistoryProps) {
  const { requestRefresh, selectedSourceId, sourceAvailable, sourceMetadata, sourceMetadataLoading, sourceStateError } = useDashboardContext()
  const { page, goToPage, correctPage } = history
  const [sort, setSort] = useState<SortState<SortKey> | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  const sortParam = sort ? `${sort.key}:${sort.direction}` : undefined
  const { data: messages, loading, error } = useCachedResource<MessageList>({
    key: `daily-requests|${sourceMetadata?.sources_revision ?? 0}:${selectedSourceId}::${period}`,
    ready: !sourceMetadataLoading,
    blocked: sourceAvailable ? null : (sourceStateError?.message ?? 'Selected source is unavailable'),
    // One page of one ordering: nothing here is worth replaying for another.
    cache: false,
    depsKey: `${page}|${sortParam ?? ''}`,
    load: (signal) => getMessages(period, page, PAGE_SIZE, sortParam, signal, selectedSourceId),
    fallbackError: 'Failed to load message history',
  })

  const total = messages?.total ?? 0
  const pageSize = messages?.page_size || PAGE_SIZE
  const lastPage = Math.max(1, Math.ceil(total / pageSize))
  // An old link or a shrunken range can point past the last page, which would
  // otherwise render as an empty list with no way back.
  const pastEnd = messages != null && page > lastPage
  useEffect(() => {
    if (pastEnd) correctPage(lastPage)
  }, [pastEnd, lastPage, correctPage])

  const rows = messages?.messages ?? []
  const effectiveSort = sort ?? DEFAULT_SORT
  const sortSpec: SortSpec = { key: effectiveSort.key, dir: effectiveSort.direction }
  const noun = getTotalRowLabel(selectedSourceId)

  const handleSort = (key: string) => {
    const sortKey = key as SortKey
    setSort((current) => getNextSortState(current ?? DEFAULT_SORT, sortKey, SORT_DEFAULTS[sortKey]))
    // A new ordering starts from its first page.
    correctPage(1)
  }

  const columns: Column<MessageEntry>[] = [
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
      render: (m) => (
        <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0, maxWidth: 420 }}>
          <span title={m.session_title || undefined} style={{ font: '500 13px/1.3 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{m.session_title || 'Untitled session'}</span>
          {m.model_id && <span style={{ font: '400 11px/1.2 var(--font-mono)', color: 'var(--fg-muted)', overflow: 'hidden', textOverflow: 'ellipsis' }}>{m.model_id}</span>}
        </span>
      ),
    },
    ...(selectedSourceId === 'codex'
      ? [
          {
            key: 'processing_mode',
            header: 'Requested tier',
            render: (m: MessageEntry) =>
              m.role === 'assistant' ? <RequestedTierBadge processingMode={m.processing_mode} serviceTier={m.service_tier} /> : <span style={FAINT}>—</span>,
          } satisfies Column<MessageEntry>,
        ]
      : []),
    ...(selectedSourceId === 'kimi_code'
      ? [
          {
            key: 'usage_status',
            header: 'Usage evidence',
            render: (m: MessageEntry) =>
              m.role === 'assistant' && m.usage_status ? <UsageStatusBadge status={m.usage_status} reason={m.usage_unavailable_reason} /> : <span style={FAINT}>—</span>,
          } satisfies Column<MessageEntry>,
        ]
      : []),
    {
      key: 'tokens',
      header: 'Tokens',
      numeric: true,
      sortable: true,
      render: (m) => {
        if (m.usage_status === 'unavailable') return <span title={usageUnavailableReasonDetail(m.usage_unavailable_reason)} style={FAINT}>Unavailable</span>
        const tokens = m.tokens ? getTokenTotal(m.tokens) : 0
        return tokens > 0 ? <span title={`${formatInteger(tokens)} tokens`}>{formatTokenCount(tokens)}</span> : <span style={FAINT}>—</span>
      },
    },
    {
      key: 'cost',
      header: getCostLabel(messages?.cost_status, messages?.cost_provenance),
      numeric: true,
      sortable: true,
      render: (m) => {
        if (m.usage_status === 'unavailable') return <span title="No token or cost usage was recorded for this request." style={FAINT}>Unavailable</span>
        const cost = m.cost ?? 0
        // A prompt is not billed on its own; a zero there is "not applicable", not "free".
        if (cost === 0 && m.role === 'user') return <span style={FAINT}>—</span>
        return (
          <span title={isMissingCost(m.cost_status, m.cost_provenance) ? undefined : formatCurrencyExact(cost)}>
            {formatCompactCurrencyWithProvenance(cost, m.cost_status, m.cost_provenance)}
          </span>
        )
      },
    },
  ]

  return (
    <>
      <Card flush title={getHistoryTitle(selectedSourceId)} subtitle="Open a row to see what was sent, the tokens it used and what it cost.">
        {!messages ? (
          error ? (
            <ErrorState title="Message history failed to load" message={error} onRetry={requestRefresh} />
          ) : (
            <HistorySkeleton />
          )
        ) : total === 0 ? (
          <EmptyState icon="message-square" title="No messages in this range" description={getEmptyHistoryCopy(selectedSourceId, sourceLabel)} />
        ) : (
          <>
            {error && (
              <div style={{ padding: '12px 14px 0' }}>
                <Notice tone="warning" title="Couldn't refresh the message history" action={<Button size="sm" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}>
                  Showing the last loaded page. {error}
                </Notice>
              </div>
            )}
            {rows.length === 0 ? (
              loading || pastEnd ? (
                <HistorySkeleton />
              ) : (
                <EmptyState
                  icon="message-square"
                  title="Nothing on this page"
                  action={<Button size="sm" onClick={() => goToPage(1)}>Go to the first page</Button>}
                />
              )
            ) : (
              <div aria-busy={loading} style={{ opacity: loading ? 0.6 : 1, transition: 'opacity var(--dur-fast) var(--ease-out)' }}>
                <DataTable
                  bare
                  columns={columns}
                  rows={rows}
                  // Ids repeat when a transcript is recorded twice, so the row position disambiguates.
                  rowKey={(m, i) => `${i}:${m.id}`}
                  sort={sortSpec}
                  onSort={handleSort}
                  onRowClick={(m) => setSelectedId(m.id)}
                />
              </div>
            )}
            <Pagination page={page} pageSize={pageSize} total={total} noun={noun} onPageChange={goToPage} disabled={loading || pastEnd} />
          </>
        )}
      </Card>

      <RequestDetailDrawer messageId={selectedId} onClose={() => setSelectedId(null)} />
    </>
  )
}

function HistorySkeleton() {
  return (
    <div role="status" aria-label="Loading message history" style={{ padding: '6px 16px' }}>
      {Array.from({ length: 6 }).map((_, i) => (
        <div key={i} style={{ display: 'flex', gap: 16, padding: '12px 0', borderBottom: i < 5 ? '1px solid var(--border-subtle)' : 'none' }}>
          <Skeleton width={64} height={16} />
          <Skeleton width={120} height={16} />
          <Skeleton width="40%" height={16} />
          <Skeleton width={60} height={16} style={{ marginLeft: 'auto' }} />
        </div>
      ))}
    </div>
  )
}

/* Sessions — paged session list (Vael) with a full-page detail swap keyed on
   ?session=<id> for deep-linking. The list is driven entirely by the URL:
   ?page, ?sort, ?filter (search text) and ?project_id (project scope), so
   back/forward and shared links restore exactly what was on screen. */
import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  Button,
  Card,
  DataTable,
  EmptyState,
  ErrorState,
  Icon,
  Notice,
  Pagination,
  SearchInput,
  StatCard,
  StatGrid,
  VendorChip,
  ViewSkeleton,
  vendorMeta,
  type Column,
  type SortSpec,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { SessionDetailScreen } from '../components/sessions/session-detail'
import { getSessionLabel, getSessionProjectLabel } from '../components/sessions/session-labels'
import { useHeldValue } from '../lib/use-held-value'
import { getSessionsWithFilter } from '../lib/api'
import { useCachedResource } from '../lib/use-cached-resource'
import { useMediaQuery } from '../lib/use-media-query'
import { usePeriodControls } from '../lib/use-period-controls'
import { periodTriggerLabel } from '../lib/period-label'
import { isTranscriptSource } from '../lib/message-display'
import {
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCount,
  formatCurrencyExact,
  formatCurrencyWithProvenance,
  formatDateTime,
  formatDuration,
  formatInteger,
  formatRelativeTime,
  formatShortId,
  getCostLabel,
  isMissingCost,
  safeDivide,
} from '../lib/format'
import type { SessionEntry, SessionList } from '../types/api'

const PAGE_SIZE = 12
const SEARCH_DEBOUNCE_MS = 250

/**
 * Sorting is server-side (`GET /api/v1/sessions?sort=`), because the API pages
 * the result set — sorting only the twelve rows on screen would be a lie.
 *
 * The backend orders by cost and by message count in one direction only, so
 * those columns are single-direction here. Offering a toggle whose ascending
 * arrow the server ignores would show an indicator that does not match the rows.
 */
const SESSION_SORTS = ['newest', 'oldest', 'cost', 'messages'] as const
type SessionSort = (typeof SESSION_SORTS)[number]
const DEFAULT_SESSION_SORT: SessionSort = 'newest'

function parseSessionSort(value: string | null): SessionSort {
  return SESSION_SORTS.includes(value as SessionSort) ? (value as SessionSort) : DEFAULT_SESSION_SORT
}

/** The column + arrow direction each backend sort mode corresponds to. */
const SORT_INDICATOR: Record<SessionSort, SortSpec> = {
  newest: { key: 'started', dir: 'desc' },
  oldest: { key: 'started', dir: 'asc' },
  cost: { key: 'cost', dir: 'desc' },
  messages: { key: 'messages', dir: 'desc' },
}

const SORT_DESCRIPTION: Record<SessionSort, string> = {
  newest: 'newest first',
  oldest: 'oldest first',
  cost: 'highest cost first',
  messages: 'most messages first',
}

/** Clicking a header: Started toggles newest/oldest; cost and messages are one-way. */
function nextSessionSort(current: SessionSort, columnKey: string): SessionSort {
  switch (columnKey) {
    case 'started':
      return current === 'newest' ? 'oldest' : 'newest'
    case 'cost':
      return 'cost'
    case 'messages':
      return 'messages'
    default:
      return current
  }
}

function parsePage(value: string | null) {
  const page = parseInt(value ?? '1', 10)
  return Number.isNaN(page) || page < 1 ? 1 : page
}

interface SearchDraft {
  /** What the box shows. */
  text: string
  /** The URL value this draft was typed over. */
  base: string
  /** The value this draft last wrote to the URL, once it has written one. */
  committed?: string
}

/**
 * Text for a search box that writes to a URL param after a pause in typing.
 *
 * The URL stays the source of truth; the draft only covers the gap between a
 * keystroke and the URL catching up. A draft is live while the URL holds the
 * value it was typed over or the value it wrote. Any other URL value arrived
 * from elsewhere (back/forward, a link) and is shown instead — the box's text
 * is derived on each render, so there is nothing to copy back into state.
 */
function useSearchDraft(urlValue: string, write: (value: string, replace: boolean) => void) {
  const [draft, setDraft] = useState<SearchDraft | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Once the URL shows what the draft wrote, that is the value it sits on. The
  // one it was typed over then stops counting, so Back to it clears the box.
  if (draft && draft.committed !== undefined && urlValue === draft.committed && draft.base !== draft.committed) {
    setDraft({ ...draft, base: draft.committed })
  }
  const live = draft !== null && (urlValue === draft.base || urlValue === draft.committed)

  const cancel = () => {
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = null
  }

  // A pending write belongs to a draft the URL has since moved away from.
  useEffect(() => {
    if (!live) cancel()
  }, [live])
  useEffect(() => cancel, [])

  const commit = (value: string) => {
    setDraft((current) => current && { ...current, base: current.committed ?? current.base, committed: value })
    if (value === urlValue) return
    // Refining a search rewrites the current history entry, so Back leaves the
    // search in one step instead of replaying every pause in the typing.
    write(value, urlValue !== '' && value !== '')
  }

  return {
    text: live && draft ? draft.text : urlValue,
    onChange: (text: string) => {
      cancel()
      setDraft({ text, base: live && draft ? draft.base : urlValue, committed: live && draft ? draft.committed : undefined })
      const value = text.trim()
      // Clearing the box applies at once; typing waits for a pause.
      if (value === '') commit('')
      else timerRef.current = setTimeout(() => commit(value), SEARCH_DEBOUNCE_MS)
    },
    /** Empties the box now; the caller clears the URL param itself. */
    reset: () => {
      cancel()
      setDraft({ text: '', base: urlValue, committed: '' })
    },
  }
}

export function SessionsView() {
  const {
    requestRefresh,
    selectedSourceId,
    selectedSourceInfo,
    sourceMetadata,
    sourceMetadataLoading,
    sourceAvailable,
    sourceStateError,
  } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const { cacheKey, state: periodState } = usePeriodControls()
  const compact = useMediaQuery('(max-width: 760px)')
  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name

  const rawFilter = searchParams.get('filter') ?? ''
  const projectId = searchParams.get('project_id') ?? ''
  const sort = parseSessionSort(searchParams.get('sort'))

  // ── ?page and ?session belong to the source they were chosen under ──
  // The source picker changes only ?source, so the page number and open session
  // of the previous source would carry over to one they mean nothing in.
  // Switching therefore restarts the list at page 1 with no session open. That
  // is decided while rendering, so the first request already asks for page 1;
  // the effect below then brings the URL in line. Two things are not a switch:
  // a source that only resolves once the source list has loaded, and a history
  // step that arrives with its own page and session.
  const urlPosition = `${searchParams.get('page') ?? ''}|${searchParams.get('session') ?? ''}`
  const [urlOwner, setUrlOwner] = useState({ sourceId: selectedSourceId, position: urlPosition })
  const [wasLoadingSources, setWasLoadingSources] = useState(sourceMetadataLoading)
  if (wasLoadingSources !== sourceMetadataLoading) setWasLoadingSources(sourceMetadataLoading)
  let sourceSwitched = false
  if (urlOwner.sourceId !== selectedSourceId) {
    const carriedOver = urlOwner.position === urlPosition && urlPosition !== '|'
    if (carriedOver && !wasLoadingSources) sourceSwitched = true
    else setUrlOwner({ sourceId: selectedSourceId, position: urlPosition })
  } else if (urlOwner.position !== urlPosition) {
    setUrlOwner({ sourceId: selectedSourceId, position: urlPosition })
  }
  useEffect(() => {
    if (!sourceSwitched) return
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.delete('page')
        next.delete('session')
        return next
      },
      { replace: true },
    )
  }, [sourceSwitched, setSearchParams])

  const page = sourceSwitched ? 1 : parsePage(searchParams.get('page'))
  const selectedSessionId = sourceSwitched ? null : searchParams.get('session')

  /** Edits the list's query. Anything that changes the result set restarts at page 1. */
  const updateQuery = (mutate: (params: URLSearchParams) => void, options?: { replace?: boolean }) =>
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev)
      mutate(next)
      return next
    }, options)

  const setPage = (next: number) =>
    updateQuery((params) => {
      if (next <= 1) params.delete('page')
      else params.set('page', String(next))
    })

  const handleSort = (columnKey: string) => {
    const next = nextSessionSort(sort, columnKey)
    if (next === sort) return
    updateQuery((params) => {
      params.delete('page')
      if (next === DEFAULT_SESSION_SORT) params.delete('sort')
      else params.set('sort', next)
    })
  }

  const openSession = (id: string) => updateQuery((params) => params.set('session', id))
  const closeSession = () => updateQuery((params) => params.delete('session'))

  const filterByProject = (id: string) =>
    updateQuery((params) => {
      params.set('project_id', id)
      params.delete('page')
      params.delete('session')
    })

  // ── Search: typed text, written to ?filter after a pause ──
  const search = useSearchDraft(rawFilter, (value, replace) =>
    updateQuery(
      (params) => {
        params.delete('page')
        if (value) params.set('filter', value)
        else params.delete('filter')
      },
      { replace },
    ),
  )

  const clearFilters = () => {
    search.reset()
    updateQuery((params) => {
      params.delete('filter')
      params.delete('project_id')
      params.delete('page')
    })
  }

  const clearProject = () =>
    updateQuery((params) => {
      params.delete('project_id')
      params.delete('page')
    })

  // ── Session list ──
  // Every query dimension is part of the key, so each page is cached on its own:
  // paging back, or returning from another view, shows it at once. `useHeldValue`
  // keeps the previous page on screen while the next one loads.
  const scope = `${sourceMetadata?.sources_revision ?? 0}:${selectedSourceId}::${cacheKey}`
  const { data: loaded, loading, error } = useCachedResource<SessionList>({
    key: `sessions|${scope}|${projectId}|${sort}|${page}|${rawFilter}`,
    ready: !sourceMetadataLoading,
    blocked: sourceAvailable ? null : (sourceStateError?.message ?? 'Selected source is unavailable'),
    load: (signal) => getSessionsWithFilter(page, PAGE_SIZE, cacheKey, rawFilter || undefined, projectId || undefined, signal, selectedSourceId, sort),
    fallbackError: 'Failed to load sessions',
  })
  const data = useHeldValue(scope, loaded)
  // The rows on screen belong to another page or query until `loaded` arrives.
  const stale = loading && loaded === null && data !== null

  // A bookmarked or shared ?page= can point past the end of the result set (the
  // range shrank, or the link was hand-edited). The backend answers with an empty
  // page; snap back to the last real page instead of stranding the reader there.
  const pageCount = loaded ? Math.max(1, Math.ceil(loaded.total / Math.max(1, loaded.page_size))) : null
  useEffect(() => {
    if (pageCount === null || page <= pageCount) return
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set('page', String(pageCount))
        return next
      },
      { replace: true },
    )
  }, [page, pageCount, setSearchParams])

  const summary = useMemo(() => {
    if (!data) return null
    const sessions = data.sessions
    return {
      total: data.total,
      shown: sessions.length,
      pageCost: sessions.reduce((sum, s) => sum + s.cost, 0),
      pageMessages: sessions.reduce((sum, s) => sum + s.message_count, 0),
      pageProjects: new Set(sessions.map((s) => s.project_id || s.project_name)).size,
      // Rows carry their own source only when a list mixes several.
      mixedSources: sessions.some((s) => s.source_id && s.source_id !== selectedSourceId),
      projectName: projectId ? sessions.find((s) => s.project_id === projectId)?.project_name : undefined,
    }
  }, [data, projectId, selectedSourceId])

  // ── Detail full-page swap ──
  if (selectedSessionId) {
    return <SessionDetailScreen key={selectedSessionId} id={selectedSessionId} onBack={closeSession} onFilterProject={filterByProject} />
  }

  if (!data || !summary) {
    if (loading) return <ViewSkeleton stats={4} panels={[420]} label="Loading sessions" />
    return <Card><ErrorState title="Sessions failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  const costStatus = data.cost_status
  const costProvenance = data.cost_provenance
  const costLabel = getCostLabel(costStatus, costProvenance)
  const filtered = rawFilter !== '' || projectId !== ''
  const projectLabel = summary.projectName || formatShortId(projectId, 24)
  const scopeHint = rawFilter ? `matching “${rawFilter}”` : projectId ? `in ${projectLabel}` : periodTriggerLabel(periodState)

  // The title column takes whatever width the fixed columns leave. Inline-size
  // containment keeps a long title from widening the table: the text truncates
  // to the cell instead of the cell growing to the text.
  const titleCell = (s: SessionEntry, sub: string, subTitle?: string) => {
    const label = getSessionLabel(s)
    return (
      <span style={{ display: 'flex', flexDirection: 'column', gap: 4, contain: 'inline-size', minWidth: compact ? 128 : 200 }}>
        <span title={label} style={{ font: '600 13px/1.3 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {label}
        </span>
        <span title={subTitle} style={{ font: '400 11px/1.2 var(--font-mono)', color: 'var(--fg-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {sub}
        </span>
      </span>
    )
  }

  const sessionColumn: Column<SessionEntry> = {
    key: 'id',
    header: 'Session',
    width: '100%',
    render: compact
      // On a phone the project and start time fold into the title cell.
      ? (s) => titleCell(s, `${getSessionProjectLabel(s)} · ${formatRelativeTime(new Date(s.time_created))}`, `${formatDateTime(s.time_created)} · id ${s.id}`)
      : (s) => titleCell(s, `id ${formatShortId(s.id)}`, s.id),
  }

  const sourceColumn: Column<SessionEntry> = {
    key: 'source',
    header: 'Source',
    render: (s) => <VendorChip id={s.source_id ?? selectedSourceId} />,
  }

  const wideColumns: Column<SessionEntry>[] = [
    {
      key: 'project',
      header: 'Project',
      render: (s) => {
        const label = getSessionProjectLabel(s)
        return (
          <span title={label} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, maxWidth: 'clamp(110px, 16vw, 240px)', verticalAlign: 'middle', font: '400 12px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>
            <Icon name="folder" size={13} style={{ flexShrink: 0 }} />
            <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
          </span>
        )
      },
    },
    {
      key: 'started',
      header: 'Started',
      sortable: true,
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
      render: (s) => <span title="Time between the first and last activity">{formatDuration(s.time_created, s.time_updated)}</span>,
    },
  ]

  const columns: Column<SessionEntry>[] = [
    sessionColumn,
    ...(summary.mixedSources ? [sourceColumn] : []),
    ...(compact ? [] : wideColumns),
    {
      key: 'messages',
      header: compact ? 'Msgs' : 'Messages',
      sortable: true,
      numeric: true,
      render: (s) => <span title={`${formatInteger(s.message_count)} messages`}>{formatCompactInteger(s.message_count)}</span>,
    },
    {
      key: 'cost',
      header: costLabel,
      sortable: true,
      numeric: true,
      render: (s) => {
        const status = s.cost_status ?? costStatus
        const provenance = s.cost_provenance ?? costProvenance
        return (
          <span title={isMissingCost(status, provenance) ? undefined : formatCurrencyExact(s.cost)}>
            {formatCompactCurrencyWithProvenance(s.cost, status, provenance)}
          </span>
        )
      },
    },
  ]

  const refreshNotice = error && (
    <Notice
      tone="warning"
      title="Couldn't refresh — showing the last loaded data"
      action={<Button size="sm" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}
    >
      {error}
    </Notice>
  )

  const toolbar = (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: compact ? 'flex-start' : 'flex-end', gap: 8, flexWrap: 'wrap', width: compact ? '100%' : undefined, minWidth: 0 }}>
      {projectId && <FilterChip label="Project" value={projectLabel} title={projectId} onClear={clearProject} />}
      <SearchInput
        value={search.text}
        onChange={search.onChange}
        placeholder="Search title, project or id…"
        label="Search sessions by title, project or session id"
        width={compact ? '100%' : 280}
      />
    </div>
  )

  // Nothing recorded at all: there is no list to search, so no toolbar either.
  if (summary.total === 0 && !filtered) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {refreshNotice}
        <Card>
          <EmptyState
            icon="clock"
            title="No sessions in this range"
            description={
              isTranscriptSource(selectedSourceInfo)
                ? `No ${sourceLabel} sessions were found in readable local transcripts for this range. Try a longer range.`
                : `${sourceLabel} recorded no sessions in this range. Try a longer range.`
            }
          />
        </Card>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {refreshNotice}

      <StatGrid>
        <StatCard accent label="Sessions" value={formatInteger(summary.total)} hint={scopeHint} />
        <StatCard
          label={`${costLabel} on page`}
          value={formatCurrencyWithProvenance(summary.pageCost, costStatus, costProvenance)}
          title={isMissingCost(costStatus, costProvenance) ? undefined : formatCurrencyExact(summary.pageCost)}
          hint={`${formatCount(summary.shown, 'session')} shown`}
        />
        <StatCard
          label="Messages on page"
          value={formatCompactInteger(summary.pageMessages)}
          title={`${formatInteger(summary.pageMessages)} messages`}
          hint={summary.shown > 0 ? `${formatCompactInteger(Math.round(safeDivide(summary.pageMessages, summary.shown)))} per session` : 'no sessions shown'}
        />
        <StatCard
          label="Projects on page"
          value={formatInteger(summary.pageProjects)}
          hint={summary.pageProjects === 1 ? 'distinct project shown' : 'distinct projects shown'}
        />
      </StatGrid>

      <Card
        flush
        title="Session index"
        subtitle={`${formatCount(summary.total, 'session')} · ${SORT_DESCRIPTION[sort]}`}
        action={compact ? undefined : toolbar}
      >
        {/* On a phone the search box gets a full-width row of its own. */}
        {compact && (
          <div style={{ padding: '12px 16px', borderBottom: '1px solid var(--border-subtle)' }}>{toolbar}</div>
        )}
        {summary.total === 0 ? (
          <EmptyState
            icon="search"
            title="No sessions match"
            description={
              rawFilter && projectId
                ? `No session in ${projectLabel} matches “${rawFilter}” in this range.`
                : rawFilter
                  ? `No session title, project or id contains “${rawFilter}” in this range.`
                  : `${projectLabel} has no sessions in this range.`
            }
            action={<Button size="sm" onClick={clearFilters}>{rawFilter && projectId ? 'Clear filters' : rawFilter ? 'Clear search' : 'Clear project filter'}</Button>}
          />
        ) : (
          <>
            <div aria-busy={stale} style={{ opacity: stale ? 0.55 : 1, transition: 'opacity var(--dur-fast) var(--ease-out)' }}>
              <DataTable<SessionEntry>
                bare
                columns={columns}
                rows={data.sessions}
                rowKey={(s) => s.id}
                sort={SORT_INDICATOR[sort]}
                onSort={handleSort}
                onRowClick={(s) => openSession(s.id)}
              />
            </div>
            <Pagination page={data.page} pageSize={data.page_size} total={data.total} noun="sessions" onPageChange={setPage} disabled={loading && loaded === null} />
          </>
        )}
      </Card>
    </div>
  )
}

/** A removable scope filter shown beside the search box. */
function FilterChip({ label, value, title, onClear }: { label: string; value: string; title?: string; onClear: () => void }) {
  return (
    <span
      title={title}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        height: 36,
        maxWidth: '100%',
        padding: '0 4px 0 10px',
        background: 'var(--accent-soft)',
        border: '1px solid var(--border-accent)',
        borderRadius: 'var(--radius-md)',
        font: '500 12px/1 var(--font-ui)',
        color: 'var(--fg-secondary)',
        minWidth: 0,
      }}
    >
      <Icon name="folder" size={13} color="var(--blue-300)" style={{ flexShrink: 0 }} />
      <span style={{ color: 'var(--fg-muted)', flexShrink: 0 }}>{label}</span>
      <span style={{ color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '22ch' }}>{value}</span>
      <button
        type="button"
        aria-label={`Remove ${label.toLowerCase()} filter ${value}`}
        onClick={onClear}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 26,
          height: 26,
          flexShrink: 0,
          background: 'transparent',
          border: 'none',
          borderRadius: 'var(--radius-sm)',
          color: 'var(--fg-muted)',
          cursor: 'pointer',
        }}
      >
        <Icon name="x" size={13} />
      </button>
    </span>
  )
}

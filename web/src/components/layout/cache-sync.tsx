/* Database-cache control for the top bar: a status button that opens the sync
   panel (overall state, per-source freshness, manual resync/rebuild, log).

   It also keeps the page honest about background work: whenever a sync
   finishes or the cache's last-updated time advances — whichever poll notices
   first — the views are refreshed. */
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { Badge, type BadgeTone } from '../vael/atoms'
import { Button, IconButton, Popover } from '../vael/controls'
import { ConfirmDialog } from '../vael/dialog'
import { Notice } from '../vael/feedback'
import { useDashboardContext } from './dashboard-context'
import { formatInteger, formatRelativeTime } from '../../lib/format'
import { getCacheStatus, syncCache } from '../../lib/api'
import type { CacheLogEntry, CacheSourceStatus, CacheStatusResponse, CacheSyncMode, SourceID } from '../../types/api'

// How often the cache status is re-read: fast while a job runs, moderate while
// the panel is open (freshness and progress are on screen), slow otherwise.
const POLL_RUNNING_MS = 900
const POLL_PANEL_MS = 2500
const POLL_IDLE_MS = 60_000

const PHASE_LABELS: Record<string, string> = {
  sessions: 'scanning sessions',
  messages: 'consolidating messages',
  write: 'writing aggregates',
}

export function CacheSyncControl() {
  const { pathname } = useLocation()
  const { requestRefresh, selectedSourceId } = useDashboardContext()
  const [status, setStatus] = useState<CacheStatusResponse | null>(null)
  const [fetchError, setFetchError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [pendingSourceId, setPendingSourceId] = useState<SourceID | null>(null)
  const [panelOpen, setPanelOpen] = useState(false)
  const [confirmRebuild, setConfirmRebuild] = useState(false)

  const running = Boolean(status?.sync?.running)

  // One poller, three speeds. The initial read happens right away.
  useEffect(() => {
    let cancelled = false
    const read = (quiet: boolean) => {
      getCacheStatus()
        .then((next) => {
          if (cancelled) return
          setStatus(next)
          setFetchError(null)
        })
        .catch((caught) => {
          // A background poll failing is not worth an alert; the next explicit
          // interaction (opening the panel, a manual sync) reports it.
          if (cancelled || quiet) return
          setFetchError(caught instanceof Error ? caught.message : 'Cache status unavailable')
        })
    }
    read(false)
    const interval = running ? POLL_RUNNING_MS : panelOpen ? POLL_PANEL_MS : POLL_IDLE_MS
    const id = window.setInterval(() => {
      if (!running && !panelOpen && document.visibilityState !== 'visible') return
      read(!running && !panelOpen)
    }, interval)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [running, panelOpen])

  // Refresh the views when a sync finishes or the cache moved forward without
  // one (periodic auto-sync, read-triggered fill). Keyed on what the status
  // says rather than on which request observed it, so no poll can miss it.
  const seenRef = useRef<{ running: boolean; updated: number } | null>(null)
  useEffect(() => {
    if (!status) return
    const next = { running: Boolean(status.sync?.running), updated: status.last_updated_ms ?? 0 }
    const seen = seenRef.current
    seenRef.current = next
    if (!seen) return
    const finished = seen.running && !next.running
    const advanced = !next.running && next.updated > seen.updated
    if (finished || advanced) requestRefresh()
  }, [status, requestRefresh])

  const selectedCache = useMemo(
    () => status?.sources?.find((source) => source.source_id === selectedSourceId) ?? null,
    [status?.sources, selectedSourceId],
  )
  const allSources = pathname.startsWith('/overview')
  const enabled = status?.enabled === true
  const sync = status?.sync
  const error = fetchError ?? (sync?.status === 'error' ? sync.error ?? null : null)
  const syncing = running || submitting
  const needsSync = allSources
    ? Boolean(status?.sources?.some((source) => source.available && (!source.cached || source.needs_sync)))
    : Boolean(selectedCache && selectedCache.available && (!selectedCache.cached || selectedCache.needs_sync))
  const relevant = allSources ? Boolean(status?.sources?.some((source) => source.available)) : Boolean(selectedCache?.available)
  const targetLabel = allSources ? 'all sources' : selectedCache?.label ?? selectedSourceId
  const buttonLabel = error
    ? 'Database sync failed'
    : syncing
      ? 'Syncing database'
      : needsSync
        ? `Database needs a sync for ${targetLabel}`
        : `Database cache for ${targetLabel}`
  const progress = (() => {
    if (!sync?.total) return 0
    const itemsTotal = sync.items_total ?? 0
    const itemsFraction = itemsTotal > 0 ? Math.min(sync.items_done ?? 0, itemsTotal) / itemsTotal : 0
    return Math.min(100, Math.round(((sync.completed + itemsFraction) / sync.total) * 100))
  })()

  // Header actions are global: Resync covers every source, Rebuild clears and
  // rebuilds the whole database (the server enforces this too). A single
  // source is resynced only through its own row.
  const startSync = useCallback(async (mode: CacheSyncMode, sourceId?: SourceID) => {
    setSubmitting(true)
    setPendingSourceId(mode === 'rebuild' ? null : sourceId ?? null)
    setFetchError(null)
    try {
      setStatus(await syncCache(mode === 'rebuild' ? undefined : sourceId, mode))
    } catch (caught) {
      setFetchError(caught instanceof Error ? caught.message : 'Database sync failed')
    } finally {
      setSubmitting(false)
      setPendingSourceId(null)
    }
  }, [])

  const requestSync = (mode: CacheSyncMode, sourceId?: SourceID) => {
    if (syncing) return
    // Block only when the cache is known to be off. If the status read failed
    // we cannot know, so the attempt goes through and the server answers.
    if (status && !enabled) return
    if (mode === 'rebuild') {
      setConfirmRebuild(true)
      return
    }
    void startSync(mode, sourceId)
  }

  // Shown whenever the cache is usable or the status read itself failed: with
  // no status there is no `enabled`, and hiding the control would also hide
  // the only place the failure is reported.
  if (!((enabled && relevant) || (!status && fetchError))) return null

  return (
    <>
      <Popover
        portal
        align="right"
        width={420}
        closeOnClick={false}
        label="Database sync"
        onOpenChange={setPanelOpen}
        trigger={(open, toggle) => (
          <IconButton
            name="database"
            label={buttonLabel}
            onClick={toggle}
            expanded={open}
            active={open || needsSync || Boolean(error) || running}
            spinning={syncing}
          />
        )}
      >
        <CacheSyncPanel
          status={status}
          error={error}
          needsSync={needsSync}
          syncing={syncing}
          pendingSourceId={pendingSourceId}
          progress={progress}
          onSync={requestSync}
        />
      </Popover>
      <ConfirmDialog
        open={confirmRebuild}
        danger
        title="Rebuild the whole database?"
        confirmLabel="Rebuild"
        onCancel={() => setConfirmRebuild(false)}
        onConfirm={() => {
          setConfirmRebuild(false)
          void startSync('rebuild')
        }}
      >
        This clears the cached metrics for every source and rebuilds them from the source data that is still on this machine.
        Cached data for sources that are no longer detected is deleted and cannot be rebuilt.
      </ConfirmDialog>
    </>
  )
}

function CacheSyncPanel({
  status,
  error,
  needsSync,
  syncing,
  pendingSourceId,
  progress,
  onSync,
}: {
  status: CacheStatusResponse | null
  error: string | null
  needsSync: boolean
  syncing: boolean
  pendingSourceId: SourceID | null
  progress: number
  onSync: (mode: CacheSyncMode, sourceId?: SourceID) => void
}) {
  const sync = status?.sync
  const sources = status?.sources ?? []
  const lastUpdated = status?.last_updated_ms ? new Date(status.last_updated_ms) : null
  const safeCutoff = sync?.safe_cutoff_ms ? new Date(sync.safe_cutoff_ms) : latestOf(sources, (source) => source.safe_cutoff_ms)
  const freshThrough = latestOf(sources, (source) => source.fresh_through_ms)
  const state: { label: string; tone: BadgeTone } = error
    ? { label: 'Failed', tone: 'danger' }
    : syncing
      ? { label: `Syncing ${progress}%`, tone: 'accent' }
      : needsSync
        ? { label: 'Needs sync', tone: 'warning' }
        : { label: 'Up to date', tone: 'success' }
  const runningSourceLabel = sync?.current_source_id
    ? sources.find((source) => source.source_id === sync.current_source_id)?.label ?? sync.current_source_id
    : null
  const runningDetail = sync?.running && runningSourceLabel
    ? `${runningSourceLabel} — ${PHASE_LABELS[sync.current_phase ?? ''] ?? 'starting'}${sync.items_total ? ` ${formatInteger(sync.items_done ?? 0)} / ${formatInteger(sync.items_total)}` : ''}`
    : null

  return (
    <div style={{ padding: 9, display: 'grid', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', flexWrap: 'wrap', gap: 10 }}>
        <div style={{ minWidth: 0, flex: '1 1 180px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
            <span style={{ font: '700 14px/1.2 var(--font-ui)', color: 'var(--fg-primary)' }}>Database sync</span>
            <Badge tone={state.tone} dot>{state.label}</Badge>
          </div>
          <div style={{ marginTop: 6, font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>
            Updated {lastUpdated ? formatRelativeTime(lastUpdated) : 'never'}
            {freshThrough ? ` · live through ${formatRelativeTime(freshThrough)}` : ''}
            {safeCutoff ? ` · finalized through ${formatRelativeTime(safeCutoff)}` : ''}
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
          <Button size="sm" variant={needsSync ? 'primary' : 'secondary'} iconLeft="refresh" busy={syncing} onClick={() => onSync('incremental')} title="Bring every source up to date">
            {syncing ? 'Syncing…' : 'Resync'}
          </Button>
          <Button size="sm" variant="ghost" disabled={syncing} onClick={() => onSync('rebuild')} title="Clear the database cache for all sources and rebuild it">
            Rebuild…
          </Button>
        </div>
      </div>

      {syncing && (
        <div style={{ display: 'grid', gap: 6 }}>
          <div
            role="progressbar"
            aria-label="Sync progress"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={progress}
            style={{ height: 6, borderRadius: 'var(--radius-pill)', background: 'var(--ink-850)', overflow: 'hidden' }}
          >
            <div style={{ height: '100%', width: `${Math.max(progress, 4)}%`, borderRadius: 'var(--radius-pill)', background: 'var(--accent)', transition: 'width var(--dur-slow) var(--ease-out)' }} />
          </div>
          {runningDetail && <div style={{ font: '400 12px/1.35 var(--font-ui)', color: 'var(--fg-secondary)' }}>{runningDetail}</div>}
        </div>
      )}

      {error && <Notice tone="danger">{error}</Notice>}

      <SourceStatusList
        sources={sources}
        syncing={syncing}
        currentSourceId={sync?.running ? sync.current_source_id : undefined}
        pendingSourceId={pendingSourceId}
        onResync={(sourceId) => onSync('incremental', sourceId)}
      />
      <SyncLogList logs={sync?.logs ?? []} mode={sync?.mode} />
    </div>
  )
}

const groupLabelStyle = { font: '600 11px/1 var(--font-ui)', color: 'var(--fg-muted)' } as const

function SourceStatusList({
  sources,
  syncing,
  currentSourceId,
  pendingSourceId,
  onResync,
}: {
  sources: CacheSourceStatus[]
  syncing: boolean
  currentSourceId?: string
  pendingSourceId: SourceID | null
  onResync: (sourceId: SourceID) => void
}) {
  if (sources.length === 0) return null
  return (
    <div style={{ display: 'grid', gap: 6 }}>
      <div style={groupLabelStyle}>Sources</div>
      {sources.map((source) => {
        const fillError = source.fill_error || null
        const recentError = source.recent_error || null
        const syncError = source.status === 'error' && source.reason ? source.reason : null
        const errorText = fillError ?? recentError ?? syncError
        const infoReason = !syncError && source.reason && (source.needs_sync || !source.available) ? source.reason : null
        const rowSyncing = pendingSourceId === source.source_id || (syncing && currentSourceId === source.source_id)
        const dotColor = errorText
          ? 'var(--danger)'
          : source.cached && !source.needs_sync
            ? 'var(--success)'
            : source.available
              ? 'var(--warning)'
              : 'var(--fg-faint)'
        const detail = rowSyncing
          ? 'Syncing…'
          : fillError
            ? 'Auto-refresh failed'
            : recentError
              ? 'Recent activity unreadable'
              : syncError
                ? 'Sync failed'
                : source.cached && source.fresh_through_ms
                  ? `Live ${formatRelativeTime(new Date(source.fresh_through_ms))}`
                  : source.cached && !source.needs_sync
                    ? 'Up to date'
                    : source.available
                      ? 'Needs sync'
                      : 'Unavailable'
        const meta = [
          fillError && source.fill_attempt_ms ? `Last attempt ${formatRelativeTime(new Date(source.fill_attempt_ms))}` : null,
          recentError && source.recent_attempt_ms ? `Last read ${formatRelativeTime(new Date(source.recent_attempt_ms))}` : null,
          source.last_synced_ms ? `Last synced ${formatRelativeTime(new Date(source.last_synced_ms))}` : null,
        ].filter(Boolean)
        return (
          <div key={source.source_id} style={{ display: 'grid', gap: 6, padding: '6px 6px 6px 10px', borderRadius: 'var(--radius-md)', background: 'var(--ink-750)', border: '1px solid var(--border-subtle)' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                <span aria-hidden="true" style={{ width: 7, height: 7, borderRadius: '50%', background: dotColor, flexShrink: 0 }} />
                <span style={{ font: '600 12px/1 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{source.label}</span>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
                <span style={{ font: '400 11px/1 var(--font-ui)', color: rowSyncing ? 'var(--blue-300)' : errorText ? 'var(--danger)' : 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
                  {detail}
                </span>
                <IconButton
                  name="refresh"
                  size={26}
                  label={`Resync ${source.label}`}
                  disabled={syncing || !source.available}
                  spinning={rowSyncing}
                  onClick={() => onResync(source.source_id)}
                />
              </div>
            </div>
            {(errorText || infoReason) && (
              <div style={{ marginRight: 4, padding: '6px 8px', borderRadius: 'var(--radius-sm)', background: errorText ? 'var(--danger-soft)' : 'var(--ink-850)', color: errorText ? 'var(--danger)' : 'var(--fg-muted)', font: '400 11px/1.45 var(--font-ui)', overflowWrap: 'anywhere', maxHeight: 72, overflowY: 'auto' }}>
                {errorText ?? infoReason}
              </div>
            )}
            {(errorText || infoReason) && meta.length > 0 && (
              <div style={{ font: '400 11px/1.3 var(--font-ui)', color: 'var(--fg-faint)' }}>{meta.join(' · ')}</div>
            )}
          </div>
        )
      })}
    </div>
  )
}

function SyncLogList({ logs, mode }: { logs: CacheLogEntry[]; mode?: CacheSyncMode }) {
  return (
    <div style={{ display: 'grid', gap: 6 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
        <div style={groupLabelStyle}>Activity</div>
        {mode && logs.length > 0 && <div style={{ font: '400 11px/1 var(--font-ui)', color: 'var(--fg-faint)' }}>{mode === 'rebuild' ? 'Last run: rebuild' : 'Last run: resync'}</div>}
      </div>
      <div style={{ maxHeight: 150, overflowY: 'auto', display: 'grid', gap: 5, padding: 8, borderRadius: 'var(--radius-md)', background: 'var(--ink-850)', border: '1px solid var(--border-subtle)' }}>
        {logs.length === 0 ? (
          <div style={{ font: '400 12px/1.35 var(--font-ui)', color: 'var(--fg-muted)' }}>No sync has run since the dashboard started.</div>
        ) : logs.map((entry, index) => (
          <div key={`${entry.time_ms}-${index}`} style={{ display: 'grid', gridTemplateColumns: '44px minmax(0, 1fr)', gap: 8, alignItems: 'baseline' }}>
            <span style={{ font: '400 11px/1.35 var(--font-mono)', color: entry.level === 'error' ? 'var(--danger)' : 'var(--fg-faint)' }}>{formatLogTime(entry.time_ms)}</span>
            <span style={{ font: '400 12px/1.35 var(--font-ui)', color: entry.level === 'error' ? 'var(--danger)' : 'var(--fg-secondary)', overflowWrap: 'anywhere' }}>{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function latestOf(sources: CacheSourceStatus[], pick: (source: CacheSourceStatus) => number | undefined) {
  const latest = sources.reduce((max, source) => Math.max(max, pick(source) ?? 0), 0)
  return latest ? new Date(latest) : null
}

function formatLogTime(ms: number) {
  if (!ms) return '--:--'
  return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })
}

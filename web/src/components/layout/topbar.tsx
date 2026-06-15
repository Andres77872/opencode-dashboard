/* Vael top bar — per-route title/subtitle, mobile nav toggle, live sync status
   + refresh. Sticky-fixed flex item (the in-page scroll lives in PageBody). */
import { useEffect, useMemo, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { routeMeta } from './nav-items'
import { Button, IconButton, Popover } from '../vael/controls'
import { Icon } from '../vael/icon'
import { useDashboardContext } from './dashboard-context'
import { useSidebar } from './sidebar-context'
import { formatRelativeTime } from '../../lib/format'
import { getCacheStatus, syncCache } from '../../lib/api'
import type { CacheLogEntry, CacheSourceStatus, CacheStatusResponse, CacheSyncMode } from '../../types/api'

export function TopBar() {
  const location = useLocation()
  const { title, sub } = routeMeta(location.pathname)
  const { lastUpdatedAt, isRefreshing, requestRefresh, selectedSourceId } = useDashboardContext()
  const { toggleMobile } = useSidebar()
  const [cacheStatus, setCacheStatus] = useState<CacheStatusResponse | null>(null)
  const [cacheSyncing, setCacheSyncing] = useState(false)
  const [cacheError, setCacheError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    getCacheStatus(controller.signal)
      .then((next) => {
        if (!controller.signal.aborted) {
          setCacheStatus(next)
          setCacheError(null)
        }
      })
      .catch((caught) => {
        if (!controller.signal.aborted) {
          setCacheError(caught instanceof Error ? caught.message : 'Cache status unavailable')
        }
      })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (!cacheStatus?.sync?.running) return
    const id = window.setInterval(() => {
      getCacheStatus()
        .then((next) => {
          setCacheStatus(next)
          if (next.sync?.status === 'error' && next.sync.error) {
            setCacheError(next.sync.error)
          } else {
            setCacheError(null)
          }
          if (!next.sync?.running) {
            requestRefresh()
          }
        })
        .catch((caught) => {
          setCacheError(caught instanceof Error ? caught.message : 'Cache status unavailable')
        })
    }, 900)
    return () => window.clearInterval(id)
  }, [cacheStatus?.sync?.running, requestRefresh])

  const selectedCache = useMemo(
    () => cacheStatus?.sources?.find((source) => source.source_id === selectedSourceId) ?? null,
    [cacheStatus?.sources, selectedSourceId],
  )
  const isOverview = location.pathname.startsWith('/overview')
  const cacheEnabled = cacheStatus?.enabled === true
  const syncState = cacheStatus?.sync
  const displayError = cacheError ?? syncState?.error ?? null
  const syncRunning = Boolean(syncState?.running) || cacheSyncing
  const syncProgress = (() => {
    if (!syncState?.total) return 0
    const itemsTotal = syncState.items_total ?? 0
    const itemsFraction = itemsTotal > 0 ? Math.min(syncState.items_done ?? 0, itemsTotal) / itemsTotal : 0
    return Math.min(100, Math.round(((syncState.completed + itemsFraction) / syncState.total) * 100))
  })()
  const cacheNeedsSync = isOverview
    ? Boolean(cacheStatus?.sources?.some((source) => source.available && (!source.cached || source.needs_sync)))
    : Boolean(selectedCache && selectedCache.available && (!selectedCache.cached || selectedCache.needs_sync))
  const cacheVisible = isOverview
    ? Boolean(cacheStatus?.sources?.some((source) => source.available))
    : Boolean(selectedCache?.available)
  const cacheTargetLabel = isOverview ? 'all sources' : selectedCache?.label ?? selectedSourceId
  const cacheLabel = displayError
    ? `Database sync failed: ${displayError}`
    : syncRunning
      ? 'Syncing database'
      : cacheNeedsSync
        ? `Resync database for ${cacheTargetLabel}`
        : `Database cache for ${cacheTargetLabel}`

  const handleCacheSync = async (mode: CacheSyncMode) => {
    if (!cacheEnabled || syncRunning) return
    if (mode === 'rebuild') {
      const confirmed = window.confirm(`Clear cached database metrics for ${cacheTargetLabel} and rebuild from eligible source data?`)
      if (!confirmed) return
    }
    setCacheSyncing(true)
    setCacheError(null)
    try {
      const next = await syncCache(isOverview ? undefined : selectedSourceId, mode)
      setCacheStatus(next)
      if (!next.sync?.running) requestRefresh()
    } catch (caught) {
      setCacheError(caught instanceof Error ? caught.message : 'Database sync failed')
    } finally {
      setCacheSyncing(false)
    }
  }

  return (
    <header
      style={{
        height: 'var(--topbar-height)',
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: 12,
        padding: '0 24px',
        borderBottom: '1px solid var(--border-default)',
        background: 'color-mix(in srgb, var(--ink-900) 80%, transparent)',
        backdropFilter: 'blur(8px)',
        // backdrop-filter makes this header its own stacking context; without an
        // explicit z-index its popover (e.g. the database-sync panel) would paint
        // under the later page-body sibling. Elevate the chrome above page content.
        position: 'relative',
        zIndex: 40,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
        <button
          type="button"
          aria-label="Open navigation"
          onClick={toggleMobile}
          className="xl:hidden"
          style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 32, height: 32, marginLeft: -6, color: 'var(--fg-muted)', background: 'transparent', border: '1px solid transparent', borderRadius: 'var(--radius-md)', cursor: 'pointer', flexShrink: 0 }}
        >
          <Icon name="menu" size={18} />
        </button>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, minWidth: 0 }}>
          <h1 style={{ margin: 0, font: '700 18px/1.2 var(--font-ui)', letterSpacing: '-0.015em', color: 'var(--fg-primary)', whiteSpace: 'nowrap' }}>{title}</h1>
          {sub && <span className="hidden sm:inline" style={{ font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{sub}</span>}
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
        <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
          <span className="hidden sm:inline">Last refresh </span>
          {isRefreshing ? 'refreshing...' : formatRelativeTime(lastUpdatedAt)}
        </span>
        {cacheEnabled && cacheVisible && (
          <Popover
            align="right"
            width={420}
            closeOnClick={false}
            trigger={(open, toggle) => (
              <IconButton
                name="database"
                label={cacheLabel}
                onClick={toggle}
                active={open || cacheNeedsSync || Boolean(cacheError) || Boolean(syncState?.running)}
                spinning={syncRunning}
              />
            )}
          >
            <CacheSyncPanel
              status={cacheStatus}
              error={displayError}
              targetLabel={cacheTargetLabel}
              needsSync={cacheNeedsSync}
              syncing={syncRunning}
              progress={syncProgress}
              onSync={handleCacheSync}
              onStatus={setCacheStatus}
            />
          </Popover>
        )}
        <IconButton name="refresh" label="Refresh data" onClick={requestRefresh} spinning={isRefreshing} />
      </div>
    </header>
  )
}

function CacheSyncPanel({
  status,
  error,
  targetLabel,
  needsSync,
  syncing,
  progress,
  onSync,
  onStatus,
}: {
  status: CacheStatusResponse | null
  error: string | null
  targetLabel: string
  needsSync: boolean
  syncing: boolean
  progress: number
  onSync: (mode: CacheSyncMode) => void
  onStatus: (next: CacheStatusResponse) => void
}) {
  // The panel only mounts while the popover is open: poll so the freshness and
  // progress displays stay live even when no manual sync is running.
  useEffect(() => {
    const id = window.setInterval(() => {
      getCacheStatus()
        .then(onStatus)
        .catch(() => {})
    }, 2500)
    return () => window.clearInterval(id)
  }, [onStatus])

  const sync = status?.sync
  const sources = status?.sources ?? []
  const logs = sync?.logs ?? []
  const lastUpdated = status?.last_updated_ms ? new Date(status.last_updated_ms) : null
  const safeCutoff = sync?.safe_cutoff_ms ? new Date(sync.safe_cutoff_ms) : latestSafeCutoff(sources)
  const freshThrough = latestFreshThrough(sources)
  const syncStatus = error ? 'Error' : syncing ? 'Running' : sync?.status === 'complete' ? 'Complete' : needsSync ? 'Stale' : 'Ready'
  const actionDisabled = syncing
  const runningSourceLabel = sync?.current_source_id
    ? sources.find((source) => source.source_id === sync.current_source_id)?.label ?? sync.current_source_id
    : null
  const phaseLabels: Record<string, string> = { sessions: 'scanning sessions', messages: 'consolidating messages', write: 'writing aggregates' }
  const runningDetail = sync?.running && runningSourceLabel
    ? `${runningSourceLabel} — ${phaseLabels[sync.current_phase ?? ''] ?? 'starting'}${sync.items_total ? ` ${(sync.items_done ?? 0).toLocaleString()} / ${sync.items_total.toLocaleString()}` : ''}`
    : null

  return (
    <div style={{ padding: 10, display: 'grid', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
        <div style={{ minWidth: 0 }}>
          <div style={{ font: '700 13px/1.2 var(--font-ui)', color: 'var(--fg-primary)' }}>Database sync</div>
          <div style={{ marginTop: 5, font: '500 12px/1.35 var(--font-ui)', color: 'var(--fg-muted)' }}>
            {syncStatus} · Last update {lastUpdated ? formatRelativeTime(lastUpdated) : 'never'}
          </div>
          <div style={{ marginTop: 4, font: '500 11px/1.35 var(--font-ui)', color: 'var(--fg-faint)' }}>
            {freshThrough ? `Live through ${formatRelativeTime(freshThrough)}` : 'Recent activity loads from raw data on refresh'}
            {safeCutoff ? ` · finalized through ${formatRelativeTime(safeCutoff)}` : ''}
          </div>
          {runningDetail && (
            <div style={{ marginTop: 4, font: '500 11px/1.35 var(--font-ui)', color: 'var(--accent)' }}>
              {runningDetail}
            </div>
          )}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
          <Button size="sm" variant={needsSync ? 'primary' : 'secondary'} iconLeft="database" disabled={actionDisabled} onClick={() => onSync('incremental')}>
            {syncing ? 'Syncing' : 'Resync'}
          </Button>
          <Button size="sm" variant="ghost" iconLeft="refresh" disabled={actionDisabled} onClick={() => onSync('rebuild')} title="Clear dashboard cache rows and rebuild eligible metrics">
            Rebuild
          </Button>
        </div>
      </div>

      <div style={{ display: 'grid', gap: 6 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
          <span style={{ font: '600 11px/1 var(--font-ui)', color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: 0 }}>{targetLabel}</span>
          <span style={{ font: '600 12px/1 var(--font-ui)', color: syncing ? 'var(--accent)' : 'var(--fg-muted)' }}>{syncing ? `${progress}%` : syncStatus}</span>
        </div>
        <div style={{ height: 7, borderRadius: 999, background: 'var(--ink-850)', border: '1px solid var(--border-default)', overflow: 'hidden' }}>
          <div style={{ height: '100%', width: `${syncing ? progress : needsSync ? 8 : 100}%`, background: error ? 'var(--danger)' : syncing || needsSync ? 'var(--accent)' : 'var(--success)', transition: 'width var(--dur-med) var(--ease-out)' }} />
        </div>
      </div>

      {error && (
        <div style={{ padding: '8px 9px', borderRadius: 'var(--radius-md)', border: '1px solid var(--danger)', color: 'var(--danger)', background: 'var(--danger-soft)', font: '500 12px/1.35 var(--font-ui)' }}>
          {error}
        </div>
      )}

      <SourceStatusList sources={sources} />
      <SyncLogList logs={logs} mode={sync?.mode} />
    </div>
  )
}

function SourceStatusList({ sources }: { sources: CacheSourceStatus[] }) {
  if (sources.length === 0) return null
  return (
    <div style={{ display: 'grid', gap: 5 }}>
      {sources.map((source) => {
        const hasFillError = Boolean(source.fill_error)
        const dotColor = hasFillError
          ? 'var(--danger)'
          : source.cached && !source.needs_sync
            ? 'var(--success)'
            : source.available
              ? 'var(--accent)'
              : 'var(--fg-faint)'
        const detail = hasFillError
          ? 'auto-refresh failed'
          : source.cached && source.fresh_through_ms
            ? `live ${formatRelativeTime(new Date(source.fresh_through_ms))}`
            : source.cached && !source.needs_sync
              ? 'Current'
              : source.available
                ? 'Needs sync'
                : 'Unavailable'
        return (
          <div key={source.source_id} title={source.fill_error || undefined} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10, padding: '7px 8px', borderRadius: 'var(--radius-md)', background: 'var(--ink-800)', border: '1px solid var(--border-default)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
              <span style={{ width: 7, height: 7, borderRadius: 999, background: dotColor, flexShrink: 0 }} />
              <span style={{ font: '600 12px/1 var(--font-ui)', color: 'var(--fg-secondary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{source.label}</span>
            </div>
            <span style={{ font: '500 11px/1 var(--font-ui)', color: hasFillError ? 'var(--danger)' : 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
              {detail}
            </span>
          </div>
        )
      })}
    </div>
  )
}

function SyncLogList({ logs, mode }: { logs: CacheLogEntry[]; mode?: CacheSyncMode }) {
  return (
    <div style={{ display: 'grid', gap: 7 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
        <div style={{ font: '600 11px/1 var(--font-ui)', color: 'var(--fg-muted)', textTransform: 'uppercase', letterSpacing: 0 }}>Log</div>
        {mode && <div style={{ font: '600 11px/1 var(--font-ui)', color: 'var(--fg-faint)', textTransform: 'uppercase', letterSpacing: 0 }}>{mode}</div>}
      </div>
      <div style={{ maxHeight: 150, overflow: 'auto', display: 'grid', gap: 4, padding: 8, borderRadius: 'var(--radius-md)', background: 'var(--ink-850)', border: '1px solid var(--border-default)' }}>
        {logs.length === 0 ? (
          <div style={{ font: '500 12px/1.35 var(--font-ui)', color: 'var(--fg-muted)' }}>No database sync activity</div>
        ) : logs.map((entry, index) => (
          <div key={`${entry.time_ms}-${index}`} style={{ display: 'grid', gridTemplateColumns: '54px 1fr', gap: 8, alignItems: 'baseline' }}>
            <span style={{ font: '500 11px/1 var(--font-mono)', color: entry.level === 'error' ? 'var(--danger)' : 'var(--fg-faint)' }}>{formatLogTime(entry.time_ms)}</span>
            <span style={{ font: '500 12px/1.35 var(--font-ui)', color: entry.level === 'error' ? 'var(--danger)' : 'var(--fg-secondary)' }}>{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function latestSafeCutoff(sources: CacheSourceStatus[]) {
  const latest = sources.reduce((max, source) => Math.max(max, source.safe_cutoff_ms ?? 0), 0)
  return latest ? new Date(latest) : null
}

function latestFreshThrough(sources: CacheSourceStatus[]) {
  const latest = sources.reduce((max, source) => Math.max(max, source.fresh_through_ms ?? 0), 0)
  return latest ? new Date(latest) : null
}

function formatLogTime(ms: number) {
  if (!ms) return '--:--'
  return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

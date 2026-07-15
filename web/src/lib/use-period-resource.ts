import { useCallback, useEffect, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context.ts'
import { setBypassCache } from './api.ts'
import type { SourceID } from '../types/api.ts'

export interface UsePeriodResourceOptions {
  /** When false, always fetch even if the period is already cached. Default: true. */
  cachePeriods?: boolean
  /**
   * Extra query dimensions beyond period + source (e.g. page, filter, projectId).
   * A change here re-runs the fetch. Without this, a resource with extra
   * dimensions has no way to re-trigger — its `fetcher` is deliberately not an
   * effect dep — and would have to bump the app-wide refresh nonce instead,
   * which cache-busts every other mounted resource on every page turn.
   *
   * Values must be JSON-serializable; they are compared by value, not identity.
   */
  deps?: readonly unknown[]
}

export interface UsePeriodResourceResult<T> {
  data: T | null
  loading: boolean
  error: string | null
  /** Returns cached data for ANY period key without triggering a fetch. Returns null if uncached. */
  dataForPeriod: (period: string) => T | null
  /** Silently prefetches a period in the background. Loading state is NOT affected. */
  prefetch: (period: string) => void
}

type Fetcher<T> = (period: string, signal?: AbortSignal, sourceId?: SourceID) => Promise<T>

export function getSourceScopedCacheKey(sourceId: SourceID, period: string) {
  return `${sourceId}::${period}`
}

/**
 * Generic per-period fetch + cache + refresh hook.
 *
 * - Caches results keyed by string (supports both preset periods and serialized custom range keys).
 * - Aborts in-flight requests when period changes.
 * - Re-fetches when `refreshNonce` changes (from DashboardContext).
 * - `cachePeriods: false` skips the cache — always fetches.
 * - `dataForPeriod(key)` returns cached data for any key without triggering a fetch.
 * - `prefetch(key)` silently fetches in the background. Skips keys starting with "from_" (custom ranges).
 *
 * @example
 * ```tsx
 * const { data, loading, error } = usePeriodResource(getOverview, period)
 * ```
 */
export function usePeriodResource<T>(
  fetcher: Fetcher<T>,
  period: string,
  options?: UsePeriodResourceOptions,
): UsePeriodResourceResult<T> {
  const {
    refreshNonce,
    selectedSourceId,
    sourceAvailable,
    sourceMetadataLoading,
    sourceStateError,
    setLastUpdatedAt,
    setRefreshing,
  } = useDashboardContext()
  const { cachePeriods = true, deps } = options ?? {}
  // Compared by value: callers pass a fresh array literal every render.
  const depsKey = JSON.stringify(deps ?? [])

  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const cacheRef = useRef<Map<string, T>>(new Map())
  const activeControllerRef = useRef<AbortController | null>(null)
  const mountedRef = useRef(true)
  const lastCacheKeyRef = useRef<string | null>(null)
  /**
   * Tracks the refreshNonce value that was current when the last successful
   * fetch completed. Used to skip the cache short-circuit when refreshNonce
   * changes (user clicked Refresh) — even for already-cached periods.
   * Initialized as a Symbol so it never matches the first render's refreshNonce (0),
   * ensuring the first fetch always happens (cache is empty anyway, but the
   * nonce-ref comparison serves as an additional guard).
   */
  const lastRefreshNonceRef = useRef<number | symbol>(Symbol('initial'))

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  const dataForPeriod = useCallback((p: string): T | null => {
    return cacheRef.current.get(getSourceScopedCacheKey(selectedSourceId, p)) ?? null
  }, [selectedSourceId])

  // Main fetch effect — triggers on period change or refreshNonce change
  useEffect(() => {
    if (sourceMetadataLoading) {
      setLoading(true)
      setError(null)
      setData(null)
      return
    }

    if (!sourceAvailable) {
      if (activeControllerRef.current) {
        activeControllerRef.current.abort()
        activeControllerRef.current = null
      }
      setData(null)
      setLoading(false)
      setRefreshing(false)
      setError(sourceStateError?.message ?? 'Selected source is unavailable')
      return
    }

    const cacheKey = getSourceScopedCacheKey(selectedSourceId, period)

    // Determine whether this effect re-ran because refreshNonce changed.
    // On first render lastRefreshNonceRef is a Symbol so the !== check is true
    // (the cache will be empty, so the short-circuit below won't match anyway).
    const isRefreshTriggered = lastRefreshNonceRef.current !== refreshNonce

    // Cache short-circuit: use cached data only when NOT a refresh trigger.
    // When the user clicked Refresh (refreshNonce changed), force a real fetch
    // even if this period already has cached data.
    if (cachePeriods && !isRefreshTriggered && cacheRef.current.has(cacheKey)) {
      setData(cacheRef.current.get(cacheKey) ?? null)
      setLoading(false)
      setError(null)
      lastCacheKeyRef.current = cacheKey
      return
    }

    // Blank the rows only when the *scope* changed (period or source): showing
    // one source's rows under another's label would be wrong.
    //
    // When merely a `deps` dimension changed (a page turn, a filter keystroke),
    // keep the previous rows on screen while the next set loads. Nulling them
    // here would drop the view back to its first-load skeleton — which unmounts
    // the whole page including the search input, losing focus and the caret
    // mid-typing on every debounced keystroke.
    const scopeChanged = lastCacheKeyRef.current !== cacheKey
    if (scopeChanged) {
      setData(cachePeriods ? (cacheRef.current.get(cacheKey) ?? null) : null)
      lastCacheKeyRef.current = cacheKey
    }

    const controller = new AbortController()

    // Abort any in-flight request from a previous period
    if (activeControllerRef.current) {
      activeControllerRef.current.abort()
    }
    activeControllerRef.current = controller

    const doFetch = async () => {
      setRefreshing(true)
      setError(null)
      setLoading(true)

      try {
        // When this fetch was triggered by a user-initiated refresh (nonce change),
        // set the HTTP cache bypass flag so the underlying fetch() uses
        // cache: 'no-cache'. The flag is consumed and reset by the `request()`
        // function in api.ts.
        if (isRefreshTriggered) {
          setBypassCache(true)
        }

        const next = await fetcher(period, controller.signal, selectedSourceId)

        if (controller.signal.aborted || !mountedRef.current) return

        // The cache is keyed by source+period only, so a result that also depends
        // on `deps` (page, filter, …) must never enter it — it would be replayed
        // for a different page. Those resources set cachePeriods: false.
        if (cachePeriods) {
          cacheRef.current.set(cacheKey, next)
        }
        lastRefreshNonceRef.current = refreshNonce
        setData(next)
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted) return
        setError(caught instanceof Error ? caught.message : 'Failed to load data')
      } finally {
        if (!controller.signal.aborted && mountedRef.current) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void doFetch()

    return () => {
      controller.abort()
    }
    // Period, source, availability, refreshNonce, and the caller's extra query
    // dimensions (depsKey) are the reactive inputs. `fetcher` is deliberately
    // excluded — callers pass an inline closure that reads the latest dimensions.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [period, refreshNonce, selectedSourceId, sourceAvailable, sourceMetadataLoading, sourceStateError?.message, depsKey])

  const prefetch = useCallback(
    (p: string) => {
      // Skip prefetch for custom ranges (unbounded key space)
      if (p.startsWith('from_')) return
      if (!sourceAvailable) return
      const cacheKey = getSourceScopedCacheKey(selectedSourceId, p)
      if (cacheRef.current.has(cacheKey)) return
      const controller = new AbortController()
      fetcher(p, controller.signal, selectedSourceId)
        .then((next) => {
          if (controller.signal.aborted || !mountedRef.current) return
          cacheRef.current.set(cacheKey, next)
        })
        .catch(() => {
          // Prefetch failures are intentionally silent
        })
    },
    // fetcher is intentionally omitted — the ref pattern avoids stale closure issues
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [selectedSourceId, sourceAvailable],
  )

  return {
    data,
    loading,
    error,
    dataForPeriod,
    prefetch,
  }
}

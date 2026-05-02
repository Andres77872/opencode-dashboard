import { useCallback, useEffect, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { setBypassCache } from './api'
import type { DailyPeriod } from '../types/api'

export interface UsePeriodResourceOptions {
  /** When false, always fetch even if the period is already cached. Default: true. */
  cachePeriods?: boolean
}

export interface UsePeriodResourceResult<T> {
  data: T | null
  loading: boolean
  error: string | null
  /** Returns cached data for ANY period without triggering a fetch. Returns null if uncached. */
  dataForPeriod: (period: DailyPeriod) => T | null
  /** Silently prefetches a period in the background. Loading state is NOT affected. */
  prefetch: (period: DailyPeriod) => void
}

type Fetcher<T> = (period: DailyPeriod, signal?: AbortSignal) => Promise<T>

/**
 * Generic per-period fetch + cache + refresh hook.
 *
 * - Caches results keyed by DailyPeriod.
 * - Aborts in-flight requests when period changes.
 * - Re-fetches when `refreshNonce` changes (from DashboardContext).
 * - `cachePeriods: false` skips the cache — always fetches.
 * - `dataForPeriod(period)` returns cached data for any period without triggering a fetch.
 * - `prefetch(period)` silently fetches in the background.
 *
 * For periodless endpoints (e.g. config), wrap the fetcher:
 * ```
 * usePeriodResource((_p, signal) => getConfig(signal), '7d', { cachePeriods: false })
 * ```
 *
 * @example
 * ```tsx
 * const period = searchParams.get('period') ?? '7d'
 * const { data, loading, error } = usePeriodResource(getOverview, period)
 * ```
 */
export function usePeriodResource<T>(
  fetcher: Fetcher<T>,
  period: DailyPeriod,
  options?: UsePeriodResourceOptions,
): UsePeriodResourceResult<T> {
  const { refreshNonce, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const { cachePeriods = true } = options ?? {}

  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const cacheRef = useRef<Map<DailyPeriod, T>>(new Map())
  const activeControllerRef = useRef<AbortController | null>(null)
  const mountedRef = useRef(true)
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

  const dataForPeriod = useCallback((p: DailyPeriod): T | null => {
    return cacheRef.current.get(p) ?? null
  }, [])

  // Main fetch effect — triggers on period change or refreshNonce change
  useEffect(() => {
    // Determine whether this effect re-ran because refreshNonce changed.
    // On first render lastRefreshNonceRef is a Symbol so the !== check is true
    // (the cache will be empty, so the short-circuit below won't match anyway).
    const isRefreshTriggered = lastRefreshNonceRef.current !== refreshNonce

    // Cache short-circuit: use cached data only when NOT a refresh trigger.
    // When the user clicked Refresh (refreshNonce changed), force a real fetch
    // even if this period already has cached data.
    if (cachePeriods && !isRefreshTriggered && cacheRef.current.has(period)) {
      setData(cacheRef.current.get(period) ?? null)
      setLoading(false)
      setError(null)
      return
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

        const next = await fetcher(period, controller.signal)

        if (controller.signal.aborted || !mountedRef.current) return

        cacheRef.current.set(period, next)
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
    // Period and refreshNonce are the reactive inputs
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [period, refreshNonce])

  const prefetch = useCallback(
    (p: DailyPeriod) => {
      if (cacheRef.current.has(p)) return
      const controller = new AbortController()
      fetcher(p, controller.signal)
        .then((next) => {
          if (controller.signal.aborted || !mountedRef.current) return
          cacheRef.current.set(p, next)
        })
        .catch(() => {
          // Prefetch failures are intentionally silent
        })
    },
    // fetcher is intentionally omitted — the ref pattern avoids stale closure issues
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  )

  return {
    data,
    loading,
    error,
    dataForPeriod,
    prefetch,
  }
}

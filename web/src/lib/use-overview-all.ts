import { useEffect, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context.ts'
import { getOverviewAll, withBypassCache } from './api.ts'
import type { AllSourcesOverview } from '../types/api.ts'

export interface UseOverviewAllResult {
  data: AllSourcesOverview | null
  loading: boolean
  error: string | null
}

/**
 * Source-independent fetch + cache + refresh hook for the all-sources Overview.
 *
 * This hook is keyed by source revision and period and spans the enabled,
 * available sources independently of the selected source.
 * It still participates in the global refresh (refreshNonce) and "last sync"
 * (setLastUpdatedAt) machinery so the header controls keep working.
 */
export function useOverviewAll(period: string): UseOverviewAllResult {
  const { sources, sourceMetadata, sourceMetadataLoading, refreshNonce, setLastUpdatedAt, setRefreshing } = useDashboardContext()

  const revision = sourceMetadata?.sources_revision ?? 0
  const hasSources = sources.some(source => source.enabled !== false && source.available)
  const [dataRevision, setDataRevision] = useState(revision)
  const [data, setData] = useState<AllSourcesOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const cacheRef = useRef<Map<string, AllSourcesOverview>>(new Map())
  const activeControllerRef = useRef<AbortController | null>(null)
  const mountedRef = useRef(true)
  const lastCacheKeyRef = useRef<string | null>(null)
  // Symbol initial value so the first effect run never matches refreshNonce (0).
  const lastRefreshNonceRef = useRef<number | symbol>(Symbol('initial'))

  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
    }
  }, [])

  useEffect(() => {
    if (sourceMetadataLoading) return
    if (sourceMetadata && !hasSources) { setData(null); setLoading(false); setRefreshing(false); setError('No sources are enabled and available. Enable a source in Config.'); return }
    const cacheKey = `${revision}:${period}`
    const isRefreshTriggered = lastRefreshNonceRef.current !== refreshNonce

    // Cache short-circuit (skip on user-initiated refresh).
    if (!isRefreshTriggered && cacheRef.current.has(cacheKey)) {
      setData(cacheRef.current.get(cacheKey) ?? null)
      setDataRevision(revision)
      setLoading(false)
      setError(null)
      lastCacheKeyRef.current = cacheKey
      return
    }

    if (lastCacheKeyRef.current !== cacheKey) {
      setData(cacheRef.current.get(cacheKey) ?? null)
      lastCacheKeyRef.current = cacheKey
    }

    const controller = new AbortController()
    if (activeControllerRef.current) {
      activeControllerRef.current.abort()
    }
    activeControllerRef.current = controller

    const doFetch = async () => {
      setRefreshing(true)
      setError(null)
      setLoading(true)

      try {
        const next = isRefreshTriggered
          ? await withBypassCache(() => getOverviewAll(period, controller.signal))
          : await getOverviewAll(period, controller.signal)
        if (controller.signal.aborted || !mountedRef.current) return

        cacheRef.current.set(cacheKey, next)
        lastRefreshNonceRef.current = refreshNonce
        setDataRevision(revision)
        setData(next)
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted) return
        setError(caught instanceof Error ? caught.message : 'Failed to load overview')
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
  }, [period, refreshNonce, setLastUpdatedAt, setRefreshing, revision, sourceMetadataLoading, hasSources, sourceMetadata])

  return { data: dataRevision === revision ? data : null, loading, error }
}

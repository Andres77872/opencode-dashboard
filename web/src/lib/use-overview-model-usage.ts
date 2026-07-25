import { useEffect, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context.ts'
import type { DimensionDayStats, ModelEntry, SourceDimensionError, SourceLoadError } from '../types/api.ts'
import { getOverviewAllModels, withBypassCache } from './api.ts'

export interface OverviewModelUsage {
  period: string
  models: ModelEntry[]
  trend: DimensionDayStats[]
  errors: SourceLoadError[]
  partialErrors: SourceDimensionError[]
}

export interface UseOverviewModelUsageResult {
  data: OverviewModelUsage | null
  loading: boolean
  error: string | null
}

/**
 * Lazily loads the Overview's complete per-model totals and daily trend.
 *
 * The default source-grouped view never calls the model endpoint. Once loaded,
 * a period is retained in a component-local cache so toggling Source/Model is
 * instant. A global refresh deliberately skips that cache.
 */
export function useOverviewModelUsage(period: string, enabled: boolean): UseOverviewModelUsageResult {
  const { refreshNonce } = useDashboardContext()
  const [data, setData] = useState<OverviewModelUsage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dataRefreshNonce, setDataRefreshNonce] = useState(refreshNonce)
  const [errorRefreshNonce, setErrorRefreshNonce] = useState(refreshNonce)
  const cacheRef = useRef<Map<string, OverviewModelUsage>>(new Map())
  const lastPeriodRef = useRef<string | null>(null)
  const lastRefreshNonceRef = useRef(refreshNonce)

  useEffect(() => {
    if (!enabled) {
      return
    }

    const refreshTriggered = lastRefreshNonceRef.current !== refreshNonce
    const cached = cacheRef.current.get(period)

    if (!refreshTriggered && cached) {
      setData(cached)
      setDataRefreshNonce(refreshNonce)
      setError(null)
      setLoading(false)
      lastPeriodRef.current = period
      return
    }

    if (lastPeriodRef.current !== period) {
      setData(cached ?? null)
      lastPeriodRef.current = period
    }

    const controller = new AbortController()

    async function load() {
      setLoading(true)
      setError(null)

      try {
        const response = refreshTriggered
          ? await withBypassCache(() => getOverviewAllModels(period, controller.signal))
          : await getOverviewAllModels(period, controller.signal)
        if (controller.signal.aborted) return

        const next: OverviewModelUsage = {
          period,
          models: response.model_usage ?? [],
          trend: response.model_trend ?? [],
          errors: response.errors ?? [],
          partialErrors: response.partial_errors ?? [],
        }
        cacheRef.current.set(period, next)
        lastRefreshNonceRef.current = refreshNonce
        setDataRefreshNonce(refreshNonce)
        setData(next)
      } catch (caught) {
        if (controller.signal.aborted) return
        setErrorRefreshNonce(refreshNonce)
        setError(caught instanceof Error ? caught.message : 'Failed to load model usage')
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }

    void load()
    return () => controller.abort()
  }, [enabled, period, refreshNonce])

  // A dashboard refresh also invalidates model data while Source is selected.
  // Keep the old entry in the period cache only as an implementation detail:
  // it must not keep the Top Models card visibly stale, and the next Model
  // selection will bypass it and fetch against the new refresh nonce.
  return {
    data: dataRefreshNonce === refreshNonce ? data : null,
    loading: enabled && loading,
    error: errorRefreshNonce === refreshNonce ? error : null,
  }
}

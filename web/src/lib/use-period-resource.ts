import { useId } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context.ts'
import type { SourceID } from '../types/api.ts'
import { useCachedResource, type CachedResourceResult } from './use-cached-resource.ts'

export interface UsePeriodResourceOptions {
  /**
   * Names the resource so its results outlive the view: coming back to a view
   * then shows the last data at once. Give each endpoint its own id. Without
   * one, results are cached only for this mounted component.
   */
  cacheId?: string
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

export type UsePeriodResourceResult<T> = CachedResourceResult<T>

type Fetcher<T> = (period: string, signal?: AbortSignal, sourceId?: SourceID) => Promise<T>

export function getSourceScopedCacheKey(sourceId: SourceID, period: string) {
  return `${sourceId}::${period}`
}

/**
 * Per-period, per-source resource for the selected data source.
 *
 * Keyed by source revision + source + period (supports preset periods and
 * serialized custom-range keys). Waits for source metadata, reports the
 * source-state error when the source is unavailable, and otherwise behaves
 * like any cached resource: aborts on scope change, revalidates on refresh.
 *
 * @example
 * ```tsx
 * const { data, loading, error } = usePeriodResource(getDaily, period, { cacheId: 'daily' })
 * ```
 */
export function usePeriodResource<T>(
  fetcher: Fetcher<T>,
  period: string,
  options?: UsePeriodResourceOptions,
): UsePeriodResourceResult<T> {
  const { selectedSourceId, sourceMetadata, sourceAvailable, sourceMetadataLoading, sourceStateError } = useDashboardContext()
  const { cacheId, cachePeriods = true, deps } = options ?? {}
  const instanceId = useId()
  const revision = sourceMetadata?.sources_revision ?? 0

  return useCachedResource<T>({
    key: `${cacheId ?? instanceId}|${revision}:${getSourceScopedCacheKey(selectedSourceId, period)}`,
    ready: !sourceMetadataLoading,
    blocked: sourceAvailable ? null : (sourceStateError?.message ?? 'Selected source is unavailable'),
    cache: cachePeriods,
    // Compared by value: callers pass a fresh array literal every render.
    depsKey: JSON.stringify(deps ?? []),
    // `fetcher` is deliberately read at call time — callers pass an inline
    // closure that reads the latest dimensions.
    load: (signal) => fetcher(period, signal, selectedSourceId),
  })
}

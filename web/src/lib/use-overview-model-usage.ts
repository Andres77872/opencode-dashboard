import { useDashboardContext } from '../components/layout/dashboard-context.ts'
import type { DimensionDayStats, ModelEntry, SourceDimensionError, SourceLoadError } from '../types/api.ts'
import { getOverviewAllModels } from './api.ts'
import { useCachedResource, type CachedResourceResult } from './use-cached-resource.ts'

export interface OverviewModelUsage {
  period: string
  models: ModelEntry[]
  trend: DimensionDayStats[]
  errors: SourceLoadError[]
  partialErrors: SourceDimensionError[]
}

export type UseOverviewModelUsageResult = CachedResourceResult<OverviewModelUsage>

/**
 * The Overview's complete per-model totals and daily trend.
 *
 * This is a separate, leaner request than the base overview so the page's
 * headline numbers never wait on it. Pass `enabled: false` to hold the request
 * back (the view enables it once the base overview has rendered).
 */
export function useOverviewModelUsage(period: string, enabled: boolean): UseOverviewModelUsageResult {
  const { sourceMetadata, sourceMetadataLoading, sources } = useDashboardContext()
  const revision = sourceMetadata?.sources_revision ?? 0
  const hasSources = sources.some((source) => source.enabled !== false && source.available)

  return useCachedResource<OverviewModelUsage>({
    key: `overview-models|${revision}:${period}`,
    ready: !sourceMetadataLoading,
    enabled: enabled && hasSources,
    load: async (signal) => {
      const response = await getOverviewAllModels(period, signal)
      return {
        period,
        models: response.model_usage ?? [],
        trend: response.model_trend ?? [],
        errors: response.errors ?? [],
        partialErrors: response.partial_errors ?? [],
      }
    },
    fallbackError: 'Failed to load model usage',
  })
}

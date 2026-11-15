import { useDashboardContext } from '../components/layout/dashboard-context.ts'
import { getOverviewAll } from './api.ts'
import type { AllSourcesOverview } from '../types/api.ts'
import { useCachedResource, type CachedResourceResult } from './use-cached-resource.ts'

export type UseOverviewAllResult = CachedResourceResult<AllSourcesOverview>

/**
 * The all-sources Overview aggregate.
 *
 * Keyed by source revision and period, and independent of the selected
 * source: it spans every enabled, available source. It makes no request at
 * all while no source can contribute.
 */
export function useOverviewAll(period: string): UseOverviewAllResult {
  const { sources, sourceMetadata, sourceMetadataLoading } = useDashboardContext()
  const revision = sourceMetadata?.sources_revision ?? 0
  const hasSources = sources.some((source) => source.enabled !== false && source.available)

  return useCachedResource<AllSourcesOverview>({
    key: `overview-all|${revision}:${period}`,
    ready: !sourceMetadataLoading,
    blocked: sourceMetadata && !hasSources ? 'No sources are enabled and available. Enable a source in Config.' : null,
    load: (signal) => getOverviewAll(period, signal),
    fallbackError: 'Failed to load overview',
  })
}

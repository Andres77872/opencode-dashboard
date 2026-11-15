import type { DashboardContextValue } from '../layout/dashboard-context'
import type { IconName } from '../vael/icon'

export interface SourceBlock {
  icon: IconName
  title: string
  description: string
}

/**
 * Why an analytics view has nothing it can query, or null when it can load.
 * The Overview spans every source, so only "no usable source at all" blocks it.
 */
export function getSourceBlock(
  context: Pick<DashboardContextValue, 'sources' | 'sourceMetadataLoading' | 'sourceStateError' | 'selectedSourceInfo'>,
  pathname: string,
): SourceBlock | null {
  if (pathname.startsWith('/config') || context.sourceMetadataLoading) return null
  const { sources, sourceStateError, selectedSourceInfo } = context

  if (sources.length > 0 && sources.every((source) => source.enabled === false)) {
    return {
      icon: 'layers',
      title: 'All data sources are turned off',
      description: 'Turn on at least one source in Config to see its usage here.',
    }
  }
  if (!sources.some((source) => source.enabled !== false && source.available)) {
    return {
      icon: 'database',
      title: 'No data source is available',
      description: 'None of the enabled sources could be read on this machine. Check where each one is looking in Config.',
    }
  }
  if (pathname.startsWith('/overview') || !sourceStateError) return null

  const label = selectedSourceInfo?.label ?? 'This source'
  const titles: Record<typeof sourceStateError.kind, string> = {
    disabled: `${label} is turned off`,
    unavailable: `${label} is unavailable`,
    invalid: 'Unknown data source',
    unsupported: 'Unsupported data source',
    metadata: 'Data source not found',
  }
  return { icon: 'alert-triangle', title: titles[sourceStateError.kind], description: sourceStateError.message }
}

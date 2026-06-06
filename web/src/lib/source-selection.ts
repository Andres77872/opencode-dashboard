import {
  DEFAULT_SOURCE_ID,
  isSourceID,
  type SourceID,
  type SourceListResponse,
} from '../types/api.ts'

export function getDefaultSourceId(sourceList: SourceListResponse | null): SourceID {
  const defaultSourceId = sourceList?.default_source_id ?? null
  return isSourceID(defaultSourceId) ? defaultSourceId : DEFAULT_SOURCE_ID
}

export function getStartupFallbackSourceId(sourceList: SourceListResponse | null): SourceID {
  const startupSourceId = sourceList?.startup_source_id ?? null
  return isSourceID(startupSourceId) ? startupSourceId : getDefaultSourceId(sourceList)
}

export function shouldOmitSourceParam(sourceId: SourceID, sourceList: SourceListResponse | null): boolean {
  const defaultSourceId = getDefaultSourceId(sourceList)
  const startupSourceId = getStartupFallbackSourceId(sourceList)
  return sourceId === defaultSourceId && sourceId === startupSourceId
}

export function resolveRequestedSourceId(rawSourceParam: string | null, sourceList: SourceListResponse | null): SourceID {
  const urlSourceId = rawSourceParam?.trim() || null
  if (urlSourceId && isSourceID(urlSourceId)) {
    return urlSourceId
  }

  return getStartupFallbackSourceId(sourceList)
}

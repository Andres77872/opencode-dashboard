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

/**
 * Whether the backend actually registered this source. A null list means the
 * metadata has not arrived yet — not that the source is absent — so callers must
 * not treat "unknown" as "missing".
 */
function isRegisteredSource(sourceId: SourceID, sourceList: SourceListResponse | null): boolean {
  if (!sourceList) {
    return true
  }
  return sourceList.sources.some((source) => source.id === sourceId)
}

export function resolveRequestedSourceId(
  rawSourceParam: string | null,
  sourceList: SourceListResponse | null,
  storedSourceId: SourceID | null = null,
): SourceID {
  // An explicit ?source= wins even when it is unknown to the backend: the user
  // asked for it by name, so useSourceState surfaces a diagnostic rather than
  // silently serving a different source's numbers under that label.
  const urlSourceId = rawSourceParam?.trim() || null
  if (urlSourceId && isSourceID(urlSourceId)) {
    return urlSourceId
  }

  // The persisted preference wins over the backend startup hint so the user's
  // last explicit choice survives reloads and bare-URL navigation — but only
  // while that source still exists. A source can disappear between runs (e.g.
  // the backend is restarted without --codex-home), and an implicit preference
  // for a now-missing source would otherwise pin every view to a permanent
  // "unavailable" error, with nothing in the URL to explain why.
  if (storedSourceId && isSourceID(storedSourceId) && isRegisteredSource(storedSourceId, sourceList)) {
    return storedSourceId
  }

  return getStartupFallbackSourceId(sourceList)
}

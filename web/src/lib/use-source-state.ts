import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { isSourceID, type SourceID, type SourceInfo, type SourceListResponse } from '../types/api.ts'
import { resolveRequestedSourceId, shouldOmitSourceParam } from './source-selection.ts'
import { getStoredSourceId, setStoredSourceId } from './persisted-prefs.ts'

export interface SourceStateError {
  kind: 'invalid' | 'unsupported' | 'unavailable' | 'metadata'
  message: string
}

export interface SourceState {
  selectedSourceId: SourceID
  selectedSourceInfo: SourceInfo | null
  sourceStateError: SourceStateError | null
  sourceAvailable: boolean
  setSelectedSourceId: (sourceId: SourceID) => void
}

const PENDING_SOURCE_INFO: Partial<Record<SourceID, SourceInfo>> = {
  claude_code: {
    id: 'claude_code',
    label: 'Claude Code',
    kind: 'jsonl',
    available: false,
    default: false,
    read_only: true,
    local_only: true,
    capabilities: [],
    warnings: ['Claude Code transcripts are plaintext local files and may contain sensitive content.'],
    diagnostics: {
      status: 'unavailable',
      reason: 'Claude Code source is not registered by this backend yet.',
    },
    cost_policy: {
      status: 'missing',
      currency: 'USD',
      note: 'Claude Code pricing support is pending adapter registration.',
    },
    privacy: {
      plaintext_transcripts: true,
      read_only: true,
      local_only: true,
      redaction: true,
    },
  },
  codex: {
    id: 'codex',
    label: 'Codex',
    kind: 'jsonl',
    available: false,
    default: false,
    read_only: true,
    local_only: true,
    capabilities: [],
    warnings: ['Codex transcripts are plaintext local files and may contain sensitive prompt, path, and tool-output content.'],
    diagnostics: {
      status: 'unavailable',
      reason: 'Codex source is not registered by this backend yet.',
    },
    cost_policy: {
      status: 'estimated_api_equivalent',
      currency: 'USD',
      note: 'Codex costs are estimated API-equivalent values, not actual subscription spend.',
    },
    privacy: {
      plaintext_transcripts: true,
      read_only: true,
      local_only: true,
      redaction: true,
    },
  },
}

export function getPendingSourceInfo(sourceId: SourceID): SourceInfo | null {
  return PENDING_SOURCE_INFO[sourceId] ?? null
}

function findSource(sourceList: SourceListResponse | null, sourceId: SourceID): SourceInfo | null {
  return sourceList?.sources.find((source) => source.id === sourceId) ?? null
}

function getSourceStateError(rawSourceParam: string | null, requestedSourceId: SourceID, sourceInfo: SourceInfo | null): SourceStateError | null {
  if (rawSourceParam === 'both') {
    return {
      kind: 'unsupported',
      message: 'The merged source “both” is not supported in v1. Select one source at a time.',
    }
  }

  if (rawSourceParam && !isSourceID(rawSourceParam)) {
    return {
      kind: 'invalid',
      message: `Unknown data source “${rawSourceParam}”. The dashboard will not silently fall back to OpenCode.`,
    }
  }

  if (!sourceInfo) {
    if (requestedSourceId === 'claude_code' || requestedSourceId === 'codex') {
      const label = requestedSourceId === 'codex' ? 'Codex' : 'Claude Code'
      return {
        kind: 'unavailable',
        message: `${label} is not registered by the backend yet. OpenCode remains available, but this selected ${label} view has no data source to query.`,
      }
    }

    return {
      kind: 'metadata',
      message: `Source metadata for “${requestedSourceId}” is unavailable.`,
    }
  }

  if (!sourceInfo.available) {
    return {
      kind: 'unavailable',
      message: sourceInfo.diagnostics?.reason
        ? `${sourceInfo.label} is unavailable: ${sourceInfo.diagnostics.reason}`
        : `${sourceInfo.label} is unavailable.`,
    }
  }

  return null
}

export function useSourceState(sourceList: SourceListResponse | null): SourceState {
  const [searchParams, setSearchParams] = useSearchParams()

  const rawSourceParam = searchParams.get('source')?.trim() || null
  const storedSourceId = getStoredSourceId()
  const requestedSourceId = resolveRequestedSourceId(rawSourceParam, sourceList, storedSourceId)

  const selectedSourceInfo = useMemo(() => {
    const sourceInfo = findSource(sourceList, requestedSourceId)
    if (sourceInfo) {
      return sourceInfo
    }
    return getPendingSourceInfo(requestedSourceId)
  }, [requestedSourceId, sourceList])

  const sourceStateError = useMemo(
    () => getSourceStateError(rawSourceParam, requestedSourceId, selectedSourceInfo),
    [rawSourceParam, requestedSourceId, selectedSourceInfo],
  )

  const setSelectedSourceId = (sourceId: SourceID) => {
    setStoredSourceId(sourceId)
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      if (shouldOmitSourceParam(sourceId, sourceList)) {
        next.delete('source')
      } else {
        next.set('source', sourceId)
      }

      return next
    })
  }

  return {
    selectedSourceId: requestedSourceId,
    selectedSourceInfo,
    sourceStateError,
    sourceAvailable: selectedSourceInfo?.available === true && sourceStateError === null,
    setSelectedSourceId,
  }
}

import type { MessageEntry, SourceID } from '../types/api.ts'

type SourceLike = Pick<MessageEntry, 'source_id' | 'folded_assistant_calls' | 'folded_tool_calls'>

export function getHistoryTitle(sourceId: SourceID): string {
  return sourceId === 'claude_code' ? 'Interactions history' : 'Messages history'
}

export function getSessionColumnLabel(sourceId: SourceID): string {
  return sourceId === 'claude_code' ? 'Prompt / session' : 'Session'
}

export function getTotalRowLabel(sourceId: SourceID): string {
  return sourceId === 'claude_code' ? 'interactions' : 'messages'
}

export function getEmptyHistoryCopy(sourceId: SourceID, sourceLabel: string): string {
  if (sourceId === 'claude_code') {
    return 'No Claude Code interactions were found in readable local transcripts for this Daily window.'
  }

  return `No ${sourceLabel} messages recorded for this Daily window yet.`
}

export function getDetailTitle(sourceId: SourceID, loading: boolean): string {
  if (sourceId === 'claude_code') {
    return loading ? 'Loading interaction detail' : 'Interaction detail'
  }

  return loading ? 'Loading request detail' : 'Request detail'
}

export function getDetailLoadingCopy(sourceId: SourceID): string {
  return sourceId === 'claude_code'
    ? 'Fetching grouped interaction content…'
    : 'Fetching verbose request content…'
}

export function hasFoldedProvenanceCounts(message: SourceLike): boolean {
  return positiveCount(message.folded_assistant_calls) > 0 || positiveCount(message.folded_tool_calls) > 0
}

export function getFoldedProvenanceText(message: SourceLike, selectedSourceId?: SourceID): string | null {
  const sourceId = message.source_id ?? selectedSourceId
  if (sourceId !== 'claude_code') {
    return null
  }

  const assistantCalls = positiveCount(message.folded_assistant_calls)
  const toolCalls = positiveCount(message.folded_tool_calls)
  if (assistantCalls === 0 && toolCalls === 0) {
    return 'Grouped Claude Code interaction; folded call counts unavailable.'
  }

  const parts: string[] = []
  if (assistantCalls > 0) {
    parts.push(`${assistantCalls} assistant ${assistantCalls === 1 ? 'call' : 'calls'}`)
  }
  if (toolCalls > 0) {
    parts.push(`${toolCalls} tool ${toolCalls === 1 ? 'call' : 'calls'}`)
  }

  return `Grouped Claude Code interaction with ${joinParts(parts)} folded into one row.`
}

function positiveCount(value: number | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? value : 0
}

function joinParts(parts: string[]): string {
  if (parts.length <= 1) {
    return parts[0] ?? ''
  }

  return `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}`
}

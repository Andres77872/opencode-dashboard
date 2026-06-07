import assert from 'node:assert/strict'
import test from 'node:test'
import {
  getDetailLoadingCopy,
  getDetailTitle,
  getEmptyHistoryCopy,
  getFoldedProvenanceText,
  getHistoryTitle,
  getSessionColumnLabel,
  getTotalRowLabel,
  hasFoldedProvenanceCounts,
} from './message-display.ts'
import { formatCostProvenance, formatCurrencyWithProvenance } from './format.ts'
import type { CostProvenance } from '../types/api.ts'

test('uses Claude interaction wording without overwriting OpenCode copy', () => {
  assert.equal(getHistoryTitle('claude_code'), 'Interactions history')
  assert.equal(getHistoryTitle('codex'), 'Interactions history')
  assert.equal(getSessionColumnLabel('claude_code'), 'Prompt / session')
  assert.equal(getSessionColumnLabel('codex'), 'Prompt / session')
  assert.equal(getTotalRowLabel('claude_code'), 'interactions')
  assert.equal(getTotalRowLabel('codex'), 'interactions')
  assert.equal(getDetailTitle('claude_code', false), 'Interaction detail')
  assert.equal(getDetailTitle('codex', false), 'Interaction detail')
  assert.equal(getDetailLoadingCopy('claude_code'), 'Fetching grouped interaction content…')
  assert.equal(getDetailLoadingCopy('codex'), 'Fetching grouped interaction content…')

  assert.equal(getHistoryTitle('opencode'), 'Messages history')
  assert.equal(getSessionColumnLabel('opencode'), 'Session')
  assert.equal(getTotalRowLabel('opencode'), 'messages')
  assert.equal(getDetailTitle('opencode', false), 'Request detail')
  assert.equal(getDetailLoadingCopy('opencode'), 'Fetching verbose request content…')
})

test('uses source-aware empty state copy', () => {
  assert.equal(
    getEmptyHistoryCopy('claude_code', 'Claude Code'),
    'No Claude Code interactions were found in readable local transcripts for this Daily window.',
  )
  assert.equal(
    getEmptyHistoryCopy('codex', 'Codex'),
    'No Codex interactions were found in readable local transcripts for this Daily window.',
  )
  assert.equal(getEmptyHistoryCopy('opencode', 'OpenCode'), 'No OpenCode messages recorded for this Daily window yet.')
})

test('formats folded assistant and tool provenance for Claude interactions', () => {
  assert.equal(
    getFoldedProvenanceText({ source_id: 'claude_code', folded_assistant_calls: 2, folded_tool_calls: 1 }),
    'Grouped Claude Code interaction with 2 assistant calls and 1 tool call folded into one row.',
  )
  assert.equal(
    getFoldedProvenanceText({ folded_assistant_calls: 1, folded_tool_calls: 2 }, 'claude_code'),
    'Grouped Claude Code interaction with 1 assistant call and 2 tool calls folded into one row.',
  )
  assert.equal(hasFoldedProvenanceCounts({ folded_assistant_calls: 2, folded_tool_calls: 0 }), true)
})

test('uses honest fallback when Claude folded counts are unavailable', () => {
  assert.equal(
    getFoldedProvenanceText({ source_id: 'claude_code' }),
    'Grouped Claude Code interaction; folded call counts unavailable.',
  )
  assert.equal(hasFoldedProvenanceCounts({ source_id: 'claude_code' }), false)
})

test('formats folded token assistant and tool provenance for Codex interactions', () => {
  assert.equal(
    getFoldedProvenanceText({ source_id: 'codex', folded_token_updates: 4, folded_assistant_calls: 1, folded_tool_calls: 3 }),
    'Grouped Codex interaction with 4 token updates, 1 assistant message and 3 tool calls folded into one row.',
  )
  assert.equal(
    getFoldedProvenanceText({ folded_token_updates: 1, folded_assistant_calls: 2, folded_tool_calls: 0 }, 'codex'),
    'Grouped Codex interaction with 1 token update and 2 assistant messages folded into one row.',
  )
  assert.equal(hasFoldedProvenanceCounts({ source_id: 'codex', folded_token_updates: 1 }), true)
})

test('uses honest fallback when Codex folded counts are unavailable', () => {
  assert.equal(
    getFoldedProvenanceText({ source_id: 'codex' }),
    'Grouped Codex interaction; folded activity counts unavailable.',
  )
})

test('formats estimated API-equivalent cost honestly', () => {
  const provenance: CostProvenance = {
    status: 'estimated_api_equivalent',
    currency: 'USD',
    note: 'Estimated using OpenAI API rates. This is not actual billed spend.',
  }
  assert.equal(formatCurrencyWithProvenance(0.123456, 'estimated_api_equivalent', provenance), '≈ $0.123456')
  assert.equal(formatCostProvenance('estimated_api_equivalent', provenance), provenance.note)
  assert.equal(formatCurrencyWithProvenance(0, 'missing'), 'Unknown')
})

test('does not apply Claude folded provenance to OpenCode rows', () => {
  assert.equal(getFoldedProvenanceText({ source_id: 'opencode', folded_assistant_calls: 2, folded_tool_calls: 1 }), null)
})

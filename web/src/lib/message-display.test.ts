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

test('uses non-grouped wording for every source now that nothing folds', () => {
  // Claude Code and Codex were the last folded-interaction sources; both now report
  // one message per API request, so all sources use the non-grouped wording.
  for (const source of ['claude_code', 'codex', 'opencode'] as const) {
    assert.equal(getHistoryTitle(source), 'Messages history')
    assert.equal(getSessionColumnLabel(source), 'Session')
    assert.equal(getTotalRowLabel(source), 'messages')
    assert.equal(getDetailTitle(source, false), 'Request detail')
    assert.equal(getDetailLoadingCopy(source), 'Fetching verbose request content…')
  }
})

test('uses source-aware empty state copy', () => {
  assert.equal(
    getEmptyHistoryCopy('claude_code', 'Claude Code'),
    'No Claude Code API requests were found in readable local transcripts for this Daily window.',
  )
  assert.equal(
    getEmptyHistoryCopy('codex', 'Codex'),
    'No Codex API requests were found in readable local transcripts for this Daily window.',
  )
  assert.equal(getEmptyHistoryCopy('opencode', 'OpenCode'), 'No OpenCode messages recorded for this Daily window yet.')
})

test('no source produces folded-interaction provenance text anymore', () => {
  // Neither Claude Code nor Codex folds multiple API requests into one row, so no
  // folded-interaction provenance text is produced even if legacy fields are present.
  assert.equal(
    getFoldedProvenanceText({ source_id: 'claude_code', folded_assistant_calls: 2, folded_tool_calls: 1 }),
    null,
  )
  assert.equal(
    getFoldedProvenanceText({ source_id: 'codex', folded_token_updates: 4, folded_assistant_calls: 1, folded_tool_calls: 3 }),
    null,
  )
  assert.equal(getFoldedProvenanceText({ source_id: 'codex' }), null)
  // hasFoldedProvenanceCounts is source-agnostic and only inspects the count fields.
  assert.equal(hasFoldedProvenanceCounts({ folded_assistant_calls: 2, folded_tool_calls: 0 }), true)
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

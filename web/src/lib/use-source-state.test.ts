import assert from 'node:assert/strict'
import test from 'node:test'
import { getPendingSourceInfo } from './use-source-state.ts'

test('provides generic pending source metadata for Claude Code and Codex', () => {
  const claude = getPendingSourceInfo('claude_code')
  const codex = getPendingSourceInfo('codex')

  assert.equal(claude?.id, 'claude_code')
  assert.equal(claude?.available, false)
  assert.equal(claude?.privacy?.plaintext_transcripts, true)

  assert.equal(codex?.id, 'codex')
  assert.equal(codex?.label, 'Codex')
  assert.equal(codex?.available, false)
  assert.equal(codex?.kind, 'jsonl')
  assert.equal(codex?.read_only, true)
  assert.equal(codex?.local_only, true)
  assert.equal(codex?.privacy?.plaintext_transcripts, true)
  assert.equal(codex?.privacy?.redaction, true)
  assert.equal(codex?.cost_policy?.status, 'estimated_api_equivalent')
  assert.match(codex?.cost_policy?.note ?? '', /not actual subscription spend/i)
})

test('does not fabricate pending metadata for OpenCode', () => {
  assert.equal(getPendingSourceInfo('opencode'), null)
})

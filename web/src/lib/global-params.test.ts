import assert from 'node:assert/strict'
import test from 'node:test'
import { globalSearch } from './global-params.ts'

test('keeps the global filters and drops view-specific params', () => {
  assert.equal(globalSearch('?source=codex&period=7d&page=3&q=fix'), '?source=codex&period=7d')
  assert.equal(globalSearch('?page=2'), '')
  assert.equal(globalSearch(''), '')
})

test('overrides replace or remove a filter', () => {
  assert.equal(globalSearch('?source=codex&period=7d', { source: 'claude_code' }), '?source=claude_code&period=7d')
  assert.equal(globalSearch('?source=codex&period=7d', { source: null }), '?period=7d')
  assert.equal(globalSearch('?period=30d', { source: 'opencode' }), '?source=opencode&period=30d')
})

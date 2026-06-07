import assert from 'node:assert/strict'
import test from 'node:test'
import { getSourceScopedCacheKey } from './use-period-resource.ts'
import { resolveRequestedSourceId, shouldOmitSourceParam } from './source-selection.ts'
import type { SourceListResponse } from '../types/api.ts'

function sourceList(startupSourceId: SourceListResponse['startup_source_id']): SourceListResponse {
  return {
    default_source_id: 'opencode',
    startup_source_id: startupSourceId,
    sources: [
      {
        id: 'opencode',
        label: 'OpenCode',
        kind: 'sqlite',
        available: true,
        default: true,
        read_only: true,
        local_only: true,
        capabilities: [],
      },
      {
        id: 'claude_code',
        label: 'Claude Code',
        kind: 'jsonl',
        available: true,
        default: false,
        read_only: true,
        local_only: true,
        capabilities: [],
      },
      {
        id: 'codex',
        label: 'Codex',
        kind: 'jsonl',
        available: true,
        default: false,
        read_only: true,
        local_only: true,
        capabilities: [],
      },
    ],
  }
}

test('uses backend startup source when URL source is absent', () => {
  assert.equal(resolveRequestedSourceId(null, sourceList('claude_code')), 'claude_code')
  assert.equal(resolveRequestedSourceId(null, sourceList('codex')), 'codex')
})

test('preserves URL source precedence over backend startup source', () => {
  assert.equal(resolveRequestedSourceId('opencode', sourceList('claude_code')), 'opencode')
  assert.equal(resolveRequestedSourceId('claude_code', sourceList('opencode')), 'claude_code')
  assert.equal(resolveRequestedSourceId('codex', sourceList('opencode')), 'codex')
  assert.equal(resolveRequestedSourceId('claude_code', sourceList('codex')), 'claude_code')
})

test('preserves OpenCode default behavior without URL or startup source', () => {
  assert.equal(resolveRequestedSourceId(null, sourceList(undefined)), 'opencode')
})

test('invalid URL source does not become selected and falls back to startup/default for rendering error state', () => {
  assert.equal(resolveRequestedSourceId('both', sourceList('claude_code')), 'claude_code')
  assert.equal(resolveRequestedSourceId('does_not_exist', sourceList(undefined)), 'opencode')
})

test('omits source param only when default and startup fallback are the same source', () => {
  assert.equal(shouldOmitSourceParam('opencode', sourceList(undefined)), true)
  assert.equal(shouldOmitSourceParam('opencode', sourceList('opencode')), true)
  assert.equal(shouldOmitSourceParam('opencode', sourceList('claude_code')), false)
  assert.equal(shouldOmitSourceParam('claude_code', sourceList('claude_code')), false)
  assert.equal(shouldOmitSourceParam('codex', sourceList('codex')), false)
})

test('source-scoped period cache keys isolate OpenCode Claude and Codex payloads', () => {
  assert.equal(getSourceScopedCacheKey('opencode', '7d'), 'opencode::7d')
  assert.equal(getSourceScopedCacheKey('claude_code', '7d'), 'claude_code::7d')
  assert.equal(getSourceScopedCacheKey('codex', '7d'), 'codex::7d')
  assert.notEqual(getSourceScopedCacheKey('opencode', '7d'), getSourceScopedCacheKey('claude_code', '7d'))
  assert.notEqual(getSourceScopedCacheKey('opencode', '7d'), getSourceScopedCacheKey('codex', '7d'))
  assert.notEqual(getSourceScopedCacheKey('claude_code', '7d'), getSourceScopedCacheKey('codex', '7d'))
})

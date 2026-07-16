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
      {
        id: 'kimi_code',
        label: 'Kimi Code',
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
  assert.equal(resolveRequestedSourceId(null, sourceList('kimi_code')), 'kimi_code')
})

test('preserves URL source precedence over backend startup source', () => {
  assert.equal(resolveRequestedSourceId('opencode', sourceList('claude_code')), 'opencode')
  assert.equal(resolveRequestedSourceId('claude_code', sourceList('opencode')), 'claude_code')
  assert.equal(resolveRequestedSourceId('codex', sourceList('opencode')), 'codex')
  assert.equal(resolveRequestedSourceId('kimi_code', sourceList('opencode')), 'kimi_code')
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
  assert.equal(shouldOmitSourceParam('kimi_code', sourceList('kimi_code')), false)
})

test('source-scoped period cache keys isolate every source payload', () => {
  assert.equal(getSourceScopedCacheKey('opencode', '7d'), 'opencode::7d')
  assert.equal(getSourceScopedCacheKey('claude_code', '7d'), 'claude_code::7d')
  assert.equal(getSourceScopedCacheKey('codex', '7d'), 'codex::7d')
  assert.equal(getSourceScopedCacheKey('kimi_code', '7d'), 'kimi_code::7d')
  assert.notEqual(getSourceScopedCacheKey('opencode', '7d'), getSourceScopedCacheKey('claude_code', '7d'))
  assert.notEqual(getSourceScopedCacheKey('opencode', '7d'), getSourceScopedCacheKey('codex', '7d'))
  assert.notEqual(getSourceScopedCacheKey('claude_code', '7d'), getSourceScopedCacheKey('codex', '7d'))
  assert.notEqual(getSourceScopedCacheKey('codex', '7d'), getSourceScopedCacheKey('kimi_code', '7d'))
})

// A source can vanish between runs — e.g. the backend is restarted without
// --codex-home, so /sources no longer lists Codex. The persisted preference for
// it must not win, or every view is pinned to a permanent "unavailable" error
// with nothing in the URL to explain why.
test('a stored source that the backend no longer registers falls back to the startup source', () => {
  const withoutCodex: SourceListResponse = {
    default_source_id: 'opencode',
    startup_source_id: 'opencode',
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
    ],
  }

  assert.equal(resolveRequestedSourceId(null, withoutCodex, 'codex'), 'opencode')
})

test('a stored source that the backend still registers is honored', () => {
  assert.equal(resolveRequestedSourceId(null, sourceList('opencode'), 'codex'), 'codex')
})

// An explicit ?source= is the user naming it, so it still wins and gets a
// diagnostic — we must not silently serve another source's numbers under it.
test('an explicit unregistered ?source= still wins over the startup fallback', () => {
  const withoutCodex: SourceListResponse = {
    default_source_id: 'opencode',
    startup_source_id: 'opencode',
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
    ],
  }

  assert.equal(resolveRequestedSourceId('codex', withoutCodex, null), 'codex')
})

// Metadata not yet loaded must not be mistaken for "the source is gone".
test('a stored source survives while source metadata is still loading', () => {
  assert.equal(resolveRequestedSourceId(null, null, 'codex'), 'codex')
})

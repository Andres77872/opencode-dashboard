import assert from 'node:assert/strict'
import test from 'node:test'
import {
  ALL_SECTIONS,
  buildConfigSummary,
  isChipArray,
  isRedactedValue,
  normalizeConfigStats,
  resolveConfigDocument,
} from './config-utils.ts'
import type { ConfigStats } from '../types/api.ts'

function stats(overrides: Partial<ConfigStats>): ConfigStats {
  return { path: '/tmp/config.toml', exists: true, ...overrides }
}

/* ---------- resolveConfigDocument ---------- */

test('resolveConfigDocument passes through modern payloads', () => {
  const doc = resolveConfigDocument(
    stats({ format: 'toml', raw: 'model = "x"', content: { model: 'x' } }),
  )
  assert.equal(doc?.format, 'toml')
  assert.equal(doc?.raw, 'model = "x"')
  assert.equal(doc?.rawSynthesized, false)
  assert.equal(doc?.parseError, null)
  assert.equal(doc?.hasStructured, true)
})

test('resolveConfigDocument defaults format to json and synthesizes raw', () => {
  const doc = resolveConfigDocument(stats({ content: { model: 'x' } }))
  assert.equal(doc?.format, 'json')
  assert.equal(doc?.rawSynthesized, true)
  assert.equal(doc?.raw, JSON.stringify({ model: 'x' }, null, 2))
})

test('resolveConfigDocument passes through parse_error without synthesizing raw', () => {
  const doc = resolveConfigDocument(stats({ format: 'toml', parse_error: 'toml: line 3' }))
  assert.equal(doc?.parseError, 'toml: line 3')
  assert.equal(doc?.raw, '')
  assert.equal(doc?.hasStructured, false)
})

test('resolveConfigDocument returns null for null input', () => {
  assert.equal(resolveConfigDocument(null), null)
})

/* ---------- normalizeConfigStats (legacy string content) ---------- */

test('normalizeConfigStats parses legacy string content', () => {
  const legacy = { path: '/x', exists: true, content: '{"a": 1}' } as unknown as ConfigStats
  const normalized = normalizeConfigStats(legacy)
  assert.deepEqual(normalized?.content, { a: 1 })
})

test('normalizeConfigStats keeps unparseable string content as-is', () => {
  const legacy = { path: '/x', exists: true, content: 'not json' } as unknown as ConfigStats
  const normalized = normalizeConfigStats(legacy)
  assert.equal(normalized?.content, 'not json')
})

/* ---------- isRedactedValue extension ---------- */

test('isRedactedValue matches marker and embedded redacted paths', () => {
  assert.equal(isRedactedValue('[REDACTED]'), true)
  assert.equal(isRedactedValue('[REDACTED_PATH]/proj-x'), true)
  assert.equal(isRedactedValue('https:/[REDACTED_PATH]/v1'), true)
  assert.equal(isRedactedValue('plain value'), false)
  assert.equal(isRedactedValue(42), false)
  assert.equal(isRedactedValue(null), false)
})

/* ---------- isChipArray ---------- */

test('isChipArray boundaries', () => {
  assert.equal(isChipArray(['a', 'b']), true)
  assert.equal(isChipArray([]), false)
  assert.equal(isChipArray(Array.from({ length: 13 }, (_, i) => `${i}`)), false)
  assert.equal(isChipArray(['x'.repeat(41)]), false)
  assert.equal(isChipArray(['a', { nested: true }]), false)
  assert.equal(isChipArray([1, true, null]), true)
})

/* ---------- buildConfigSummary semantics ---------- */

test('buildConfigSummary: server parse_error takes precedence', () => {
  const data = stats({ parse_error: 'boom', raw: 'raw text' })
  const summary = buildConfigSummary(data, resolveConfigDocument(data))
  assert.equal(summary?.parseError, 'boom')
  assert.equal(summary?.sections.length, 0)
  assert.equal(summary?.emptyObject, false)
})

test('buildConfigSummary: missing content with raw is source-only, not an error', () => {
  const data = stats({ raw: '# comments only' })
  const summary = buildConfigSummary(data, resolveConfigDocument(data))
  assert.equal(summary?.parseError, null)
  assert.equal(summary?.sections.length, 0)
  assert.equal(summary?.emptyObject, true)
})

test('buildConfigSummary: structured content builds sections and insights', () => {
  const data = stats({ content: { model: 'x', providers: { openai: { api_key: '[REDACTED]' } } } })
  const summary = buildConfigSummary(data, resolveConfigDocument(data))
  assert.equal(summary?.sections.length, 2)
  assert.equal(summary?.insights.redactedValues, 1)
  assert.equal(summary?.parseError, null)
})

test('ALL_SECTIONS constant is stable', () => {
  assert.equal(ALL_SECTIONS, '__all__')
})

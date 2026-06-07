import assert from 'node:assert/strict'
import test, { beforeEach } from 'node:test'
import type { SourceListResponse } from '../types/api.ts'
import { resolveRequestedSourceId } from './source-selection.ts'

// Minimal in-memory localStorage so the (lazily-read) persistence layer runs under
// `node --test`, which has no DOM. Installed before importing the module under test.
function createMemoryStorage(): Storage {
  const store = new Map<string, string>()
  return {
    get length() {
      return store.size
    },
    clear: () => store.clear(),
    getItem: (key: string) => (store.has(key) ? (store.get(key) as string) : null),
    setItem: (key: string, value: string) => {
      store.set(key, String(value))
    },
    removeItem: (key: string) => {
      store.delete(key)
    },
    key: (index: number) => Array.from(store.keys())[index] ?? null,
  }
}

;(globalThis as { localStorage?: Storage }).localStorage = createMemoryStorage()

const {
  getStoredSourceId,
  setStoredSourceId,
  getStoredPeriod,
  setStoredPeriod,
} = await import('./persisted-prefs.ts')

beforeEach(() => {
  globalThis.localStorage.clear()
})

function sourceList(startupSourceId: SourceListResponse['startup_source_id']): SourceListResponse {
  return { default_source_id: 'opencode', startup_source_id: startupSourceId, sources: [] }
}

test('source id round-trips and rejects unknown values', () => {
  assert.equal(getStoredSourceId(), null)
  setStoredSourceId('codex')
  assert.equal(getStoredSourceId(), 'codex')

  // A stale / hand-edited value that is not a known source id is ignored.
  globalThis.localStorage.setItem('ocd:source', 'nonsense')
  assert.equal(getStoredSourceId(), null)
})

test('stored source id takes precedence over startup but not over the URL', () => {
  setStoredSourceId('codex')
  // No URL param → persisted preference wins over the backend startup hint.
  assert.equal(resolveRequestedSourceId(null, sourceList('claude_code'), getStoredSourceId()), 'codex')
  // An explicit URL param (shared link / back-forward) still wins over storage.
  assert.equal(resolveRequestedSourceId('opencode', sourceList('claude_code'), getStoredSourceId()), 'opencode')
})

test('resolver without a stored id is unchanged (backward compatible)', () => {
  assert.equal(resolveRequestedSourceId(null, sourceList('claude_code')), 'claude_code')
  assert.equal(resolveRequestedSourceId(null, sourceList(undefined)), 'opencode')
})

test('preset period round-trips and rejects unknown presets', () => {
  assert.equal(getStoredPeriod(), null)
  setStoredPeriod({ period: '30d' })
  assert.deepEqual(getStoredPeriod(), { period: '30d' })

  setStoredPeriod({ period: 'not-a-period' })
  assert.equal(getStoredPeriod(), null)
})

test('custom range round-trips and rejects an inverted range', () => {
  setStoredPeriod({ from: '2026-01-01', to: '2026-01-31' })
  assert.deepEqual(getStoredPeriod(), { from: '2026-01-01', to: '2026-01-31' })

  // from > to is invalid and must not be restored.
  setStoredPeriod({ from: '2026-02-10', to: '2026-02-01' })
  assert.equal(getStoredPeriod(), null)
})

test('malformed stored period JSON is ignored', () => {
  globalThis.localStorage.setItem('ocd:period', '{ not json')
  assert.equal(getStoredPeriod(), null)
})

import assert from 'node:assert/strict'
import test from 'node:test'
import { RESOURCE_STALE_AFTER_MS, createResourceCache, isResourceFresh } from './resource-cache.ts'

test('stores a value with the generation and time it was fetched under', () => {
  const cache = createResourceCache()
  cache.set('daily|7d', { rows: 3 }, 4, 1_000)
  assert.deepEqual(cache.get('daily|7d'), { value: { rows: 3 }, nonce: 4, storedAt: 1_000 })
  assert.equal(cache.get('missing'), undefined)
})

test('an entry is fresh only for its own generation and within the age budget', () => {
  const cache = createResourceCache()
  cache.set('k', 'v', 2, 1_000)
  const entry = cache.get('k')
  assert.equal(isResourceFresh(entry, 2, 1_000), true)
  assert.equal(isResourceFresh(entry, 2, 1_000 + RESOURCE_STALE_AFTER_MS - 1), true)
  // A refresh bumps the generation: the value may still be shown, but must be refetched.
  assert.equal(isResourceFresh(entry, 3, 1_000), false)
  assert.equal(isResourceFresh(entry, 2, 1_000 + RESOURCE_STALE_AFTER_MS), false)
  assert.equal(isResourceFresh(undefined, 2, 1_000), false)
})

test('evicts the least recently used entry once full', () => {
  const cache = createResourceCache(2)
  cache.set('a', 1, 0)
  cache.set('b', 2, 0)
  // Reading "a" makes "b" the oldest.
  cache.get('a')
  cache.set('c', 3, 0)
  assert.equal(cache.size, 2)
  assert.equal(cache.get('b'), undefined)
  assert.equal(cache.get<number>('a')?.value, 1)
  assert.equal(cache.get<number>('c')?.value, 3)
})

test('overwriting a key replaces its value without growing the cache', () => {
  const cache = createResourceCache(2)
  cache.set('a', 1, 0)
  cache.set('a', 2, 1)
  assert.equal(cache.size, 1)
  assert.equal(cache.get<number>('a')?.value, 2)
  assert.equal(cache.get('a')?.nonce, 1)
  cache.delete('a')
  assert.equal(cache.size, 0)
})

import assert from 'node:assert/strict'
import test from 'node:test'
import { getQuotas, withBypassCache } from './api.ts'

function stubFetch(seen: Array<RequestCache | undefined>) {
  return (async (_url: unknown, init?: RequestInit) => {
    seen.push(init?.cache)
    return new Response(JSON.stringify({ providers: [] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }) as typeof fetch
}

test('withBypassCache marks every request initiated inside the scope, and only those', async () => {
  const seen: Array<RequestCache | undefined> = []
  const originalFetch = globalThis.fetch
  globalThis.fetch = stubFetch(seen)
  try {
    // Parallel refresh fan-out: both fetches are initiated synchronously
    // inside the scope, so both must carry the bypass (the old one-shot flag
    // was consumed by whichever ran first).
    await withBypassCache(() => Promise.all([getQuotas(), getQuotas()]))
    // A sibling request outside the scope keeps normal HTTP caching.
    await getQuotas()
  } finally {
    globalThis.fetch = originalFetch
  }
  assert.deepEqual(seen, ['no-cache', 'no-cache', undefined])
})

test('withBypassCache clears the flag even when the callback throws', async () => {
  const seen: Array<RequestCache | undefined> = []
  const originalFetch = globalThis.fetch
  globalThis.fetch = stubFetch(seen)
  try {
    assert.throws(() => withBypassCache((): void => {
      throw new Error('boom')
    }), /boom/)
    await getQuotas()
  } finally {
    globalThis.fetch = originalFetch
  }
  assert.deepEqual(seen, [undefined])
})

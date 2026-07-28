import assert from 'node:assert/strict'
import test from 'node:test'
import {
  deletePricingAlias,
  getPricingAliases,
  upsertPricingAlias,
} from './api.ts'

interface SeenRequest {
  url: string
  init: RequestInit | undefined
}

function responseBody() {
  return {
    source_id: 'codex',
    supported: true,
    catalog: { source_id: 'codex', models: [] },
    catalogs: [],
    aliases: [],
    observed_models: [],
    reprice: 'started',
  }
}

test('pricing alias API helpers preserve an exact empty provider across mutations', async () => {
  const seen: SeenRequest[] = []
  const originalFetch = globalThis.fetch
  globalThis.fetch = (async (url: string | URL | Request, init?: RequestInit) => {
    seen.push({ url: String(url), init })
    return new Response(JSON.stringify(responseBody()), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }) as typeof fetch

  try {
    await getPricingAliases('codex')
    await upsertPricingAlias({
      source_id: 'codex',
      provider_id: '',
      model_id: 'custom model',
      target_source_id: 'codex',
      target_model_id: 'gpt-5.4',
    })
    await upsertPricingAlias({
      source_id: 'claude_code',
      provider_id: 'anthropic',
      model_id: 'gpt-5.6-sol',
      target_source_id: 'codex',
      target_model_id: 'gpt-5.6-sol',
    })
    await deletePricingAlias({
      source_id: 'codex',
      provider_id: '',
      model_id: 'custom model',
    })
  } finally {
    globalThis.fetch = originalFetch
  }

  assert.equal(seen[0].url, '/api/v1/pricing/aliases?source=codex')
  assert.equal(seen[0].init?.method, undefined)

  assert.equal(seen[1].url, '/api/v1/pricing/aliases')
  assert.equal(seen[1].init?.method, 'POST')
  assert.equal(new Headers(seen[1].init?.headers).get('Content-Type'), 'application/json')
  assert.deepEqual(JSON.parse(String(seen[1].init?.body)), {
    source_id: 'codex',
    provider_id: '',
    model_id: 'custom model',
    target_source_id: 'codex',
    target_model_id: 'gpt-5.4',
  })

  // A cross-source target must survive the round trip: dropping target_source_id
  // would silently reprice the model from the observing source's own catalog.
  assert.deepEqual(JSON.parse(String(seen[2].init?.body)), {
    source_id: 'claude_code',
    provider_id: 'anthropic',
    model_id: 'gpt-5.6-sol',
    target_source_id: 'codex',
    target_model_id: 'gpt-5.6-sol',
  })

  assert.equal(seen[3].url, '/api/v1/pricing/aliases?source=codex&provider=&model=custom+model')
  assert.equal(seen[3].init?.method, 'DELETE')
})

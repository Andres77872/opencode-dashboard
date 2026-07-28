import assert from 'node:assert/strict'
import test from 'node:test'
import {
  filterPricingAliasRows,
  formatPricingProvider,
  getRepriceStatusDisplay,
  mergePricingAliasDrafts,
  mergePricingAliasRows,
  pricingAliasRowKey,
  pricingRowNeedsAttention,
  samePricingTarget,
} from './pricing-aliases.ts'
import type {
  PricingAlias,
  PricingObservedModel,
  PricingRepriceStatus,
} from '../types/api.ts'

const EMPTY_TOKENS = {
  input: 0,
  output: 0,
  reasoning: 0,
  cache: { read: 0, write: 0 },
}

function observed(
  providerId: string,
  modelId: string,
  messages: number,
  overrides: Partial<PricingObservedModel> = {},
): PricingObservedModel {
  return {
    source_id: 'codex',
    provider_id: providerId,
    model_id: modelId,
    sessions: 1,
    messages,
    tokens: EMPTY_TOKENS,
    resolution_kind: 'unknown',
    resolution_reason: 'No catalog match',
    resolved: false,
    aliasable: true,
    ...overrides,
  }
}

function alias(
  providerId: string,
  modelId: string,
  targetModelId: string,
  detected = false,
  messages = 0,
  overrides: Partial<PricingAlias> = {},
): PricingAlias {
  return {
    source_id: 'codex',
    provider_id: providerId,
    model_id: modelId,
    target_source_id: 'codex',
    target_model_id: targetModelId,
    created_ms: 1,
    updated_ms: 2,
    detected,
    sessions: detected ? 1 : 0,
    messages,
    tokens: EMPTY_TOKENS,
    state: detected ? 'active' : 'not_detected',
    state_reason: 'test alias state',
    active: detected,
    editable: true,
    target_valid: true,
    overrides_native: false,
    ...overrides,
  }
}

test('merges exact provider/model aliases into detected rows and retains stale aliases', () => {
  const rows = mergePricingAliasRows(
    [observed('', 'custom-model', 12)],
    [alias('', 'custom-model', 'gpt-5.4'), alias('legacy', 'retired-model', 'gpt-5.3')],
  )

  assert.equal(rows.length, 2)
  assert.deepEqual(rows[0], {
    key: pricingAliasRowKey('codex', '', 'custom-model'),
    source_id: 'codex',
    provider_id: '',
    model_id: 'custom-model',
    detected: true,
    sessions: 1,
    messages: 12,
    tokens: EMPTY_TOKENS,
    resolution_kind: 'unknown',
    resolution_reason: 'No catalog match',
    resolution_note: null,
    resolved: false,
    aliasable: true,
    target: { source_id: 'codex', model_id: 'gpt-5.4' },
    alias: alias('', 'custom-model', 'gpt-5.4'),
    editable: true,
  })
  assert.equal(rows[1].detected, false)
  assert.equal(rows[1].model_id, 'retired-model')
  assert.deepEqual(rows[1].target, { source_id: 'codex', model_id: 'gpt-5.3' })
})

test('carries a cross-source target through to the row', () => {
  const rows = mergePricingAliasRows(
    [observed('anthropic', 'gpt-5.6-sol', 1754, { source_id: 'claude_code' })],
    [alias('anthropic', 'gpt-5.6-sol', 'gpt-5.6-sol', true, 1754, {
      source_id: 'claude_code',
      target_source_id: 'codex',
    })],
  )

  assert.equal(rows.length, 1)
  assert.deepEqual(rows[0].target, { source_id: 'codex', model_id: 'gpt-5.6-sol' })
})

test('retains activity for configured aliases that no longer appear in observed rows', () => {
  const rows = mergePricingAliasRows([], [alias('openai', 'custom-model', 'gpt-5.4', true, 42)])

  assert.equal(rows.length, 1)
  assert.equal(rows[0].detected, true)
  assert.equal(rows[0].sessions, 1)
  assert.equal(rows[0].messages, 42)
  assert.deepEqual(rows[0].tokens, EMPTY_TOKENS)
})

test('sorts detected rows by messages then provider/model and stale aliases last', () => {
  const rows = mergePricingAliasRows(
    [
      observed('zeta', 'model-z', 20),
      observed('alpha', 'model-b', 20),
      observed('alpha', 'model-a', 20),
      observed('alpha', 'model-high', 50),
    ],
    [alias('aardvark', 'stale-a', 'target-a'), alias('zeta', 'stale-z', 'target-z')],
  )

  assert.deepEqual(
    rows.map((row) => `${row.detected ? 'detected' : 'stale'}:${row.provider_id}:${row.model_id}`),
    [
      'detected:alpha:model-high',
      'detected:alpha:model-a',
      'detected:alpha:model-b',
      'detected:zeta:model-z',
      'stale:aardvark:stale-a',
      'stale:zeta:stale-z',
    ],
  )
})

test('sorts models that still need pricing ahead of ones that already price', () => {
  const rows = mergePricingAliasRows(
    [
      observed('openai', 'priced', 900, { resolution_kind: 'exact', resolved: true }),
      observed('openai', 'unpriced', 5),
    ],
    [],
  )

  assert.deepEqual(rows.map((row) => row.model_id), ['unpriced', 'priced'])
  assert.deepEqual(rows.map(pricingRowNeedsAttention), [true, false])
})

test('an active alias needs no attention, an inactive one does', () => {
  const [active] = mergePricingAliasRows([], [alias('openai', 'a', 'target', true, 3)])
  const [stale] = mergePricingAliasRows([], [alias('openai', 'b', 'target', true, 3, {
    state: 'target_missing', active: false, target_valid: false,
  })])

  assert.equal(pricingRowNeedsAttention(active), false)
  assert.equal(pricingRowNeedsAttention(stale), true)
})

test('filters by observed model, provider display, and alias target case-insensitively', () => {
  const rows = mergePricingAliasRows(
    [observed('', 'Custom-Model', 4), observed('openai', 'other', 3)],
    [alias('', 'Custom-Model', 'GPT-5.4')],
  )

  assert.deepEqual(filterPricingAliasRows(rows, 'custom').map((row) => row.model_id), ['Custom-Model'])
  assert.deepEqual(filterPricingAliasRows(rows, 'unknown provider').map((row) => row.model_id), ['Custom-Model'])
  assert.deepEqual(filterPricingAliasRows(rows, 'OPENAI').map((row) => row.model_id), ['other'])
  assert.deepEqual(filterPricingAliasRows(rows, 'gpt-5.4').map((row) => row.model_id), ['Custom-Model'])
  assert.equal(filterPricingAliasRows(rows, 'missing').length, 0)
  assert.equal(filterPricingAliasRows(rows, '   '), rows)
})

test('filters by the target source so a cross-source alias is findable by vendor', () => {
  const rows = mergePricingAliasRows(
    [observed('anthropic', 'gpt-5.6-sol', 4, { source_id: 'claude_code' })],
    [alias('anthropic', 'gpt-5.6-sol', 'gpt-5.6-sol', true, 4, {
      source_id: 'claude_code',
      target_source_id: 'codex',
    })],
  )

  assert.deepEqual(filterPricingAliasRows(rows, 'codex').map((row) => row.model_id), ['gpt-5.6-sol'])
})

test('formats an exact empty provider for display without changing non-empty ids', () => {
  assert.equal(formatPricingProvider(''), 'Unknown provider')
  assert.equal(formatPricingProvider('openai'), 'openai')
})

test('provides explicit user-facing copy for every reprice status', () => {
  const statuses: PricingRepriceStatus[] = ['started', 'queued', 'disabled', 'unavailable']
  for (const status of statuses) {
    const display = getRepriceStatusDisplay(status)
    assert.match(display.title.toLocaleLowerCase(), new RegExp(status))
    assert.ok(display.message.length > 20)
  }

  assert.equal(getRepriceStatusDisplay('started').tone, 'success')
  assert.equal(getRepriceStatusDisplay('queued').tone, 'info')
  assert.equal(getRepriceStatusDisplay('disabled').tone, 'warning')
  assert.equal(getRepriceStatusDisplay('unavailable').tone, 'warning')
})

test('an alias overriding native pricing stays editable', () => {
  const rows = mergePricingAliasRows(
    [observed('openai', 'now-native', 9, { resolution_kind: 'user_alias', resolved: true })],
    [alias('openai', 'now-native', 'gpt-5.4', true, 9, { overrides_native: true })],
  )

  assert.equal(rows.length, 1)
  assert.equal(rows[0].editable, true)
  assert.equal(rows[0].alias?.overrides_native, true)
})

test('target equality compares both the source and the model', () => {
  assert.equal(samePricingTarget(null, null), true)
  assert.equal(samePricingTarget({ source_id: 'codex', model_id: 'a' }, null), false)
  assert.equal(
    samePricingTarget({ source_id: 'codex', model_id: 'a' }, { source_id: 'codex', model_id: 'a' }),
    true,
  )
  assert.equal(
    samePricingTarget({ source_id: 'codex', model_id: 'a' }, { source_id: 'claude_code', model_id: 'a' }),
    false,
    'the same model id in a different catalog is a different target',
  )
})

test('drafts survive a refresh and only the mutated row is reset to its saved target', () => {
  const rowA = pricingAliasRowKey('codex', 'openai', 'model-a')
  const rowB = pricingAliasRowKey('codex', 'openai', 'model-b')
  const target = (model_id: string) => ({ source_id: 'codex' as const, model_id })
  const drafts = { [rowA]: target('gpt-5.4'), [rowB]: target('gpt-5.3') }

  assert.deepEqual(mergePricingAliasDrafts(drafts, []), drafts)

  const saved = alias('openai', 'model-a', 'gpt-5.4', true, 4)
  const afterSave = mergePricingAliasDrafts(drafts, [saved], [rowA])
  assert.deepEqual(afterSave[rowA], target('gpt-5.4'))
  assert.deepEqual(afterSave[rowB], target('gpt-5.3'), 'an unrelated unsaved selection must not be discarded')

  const afterRemove = mergePricingAliasDrafts(drafts, [], [rowA])
  assert.equal(afterRemove[rowA], undefined, 'a removed alias clears its own draft')
  assert.deepEqual(afterRemove[rowB], target('gpt-5.3'))

  const serverWins = mergePricingAliasDrafts({ [rowA]: target('stale-choice') }, [saved])
  assert.deepEqual(serverWins[rowA], target('gpt-5.4'), 'a persisted target overrides an older draft for the same row')

  const crossSource = alias('openai', 'model-a', 'gpt-5.6', true, 4, { target_source_id: 'codex' })
  assert.deepEqual(
    mergePricingAliasDrafts({}, [{ ...crossSource, source_id: 'claude_code' }])[
      pricingAliasRowKey('claude_code', 'openai', 'model-a')
    ],
    { source_id: 'codex', model_id: 'gpt-5.6' },
  )
})

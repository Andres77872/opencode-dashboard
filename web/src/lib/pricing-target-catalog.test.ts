import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  buildPricingTargetGroups,
  countPricingTargets,
  filterPricingTargetGroups,
  findPricingTarget,
  flattenPricingTargetGroups,
  formatPricingTargetRates,
} from './pricing-target-catalog.ts'
import type { PricingAliasCatalog } from '../types/api.ts'

function catalog(
  sourceId: PricingAliasCatalog['source_id'],
  label: string,
  models: Array<[string, number, number, boolean, string?]>,
): PricingAliasCatalog {
  return {
    source_id: sourceId,
    source_label: label,
    currency: 'USD',
    models: models.map(([model_id, input, output, targetable, display_name]) => ({
      model_id,
      display_name,
      targetable,
      rate: { input_per_million: input, cached_input_per_million: input / 10, output_per_million: output },
    })),
  }
}

const CATALOGS: PricingAliasCatalog[] = [
  catalog('codex', 'Codex', [
    ['gpt-5.6', 5, 30, true],
    ['gpt-5.6-sol', 5, 30, true],
    ['unpriced-model', 0, 0, false],
  ]),
  catalog('claude_code', 'Claude Code', [
    ['claude-opus-5', 15, 75, true],
    ['claude-3-5-haiku-20241022', 0.8, 4, true],
  ]),
  catalog('kimi_code', 'Kimi Code', [
    ['kimi-k3', 1, 5, true, 'Kimi K3'],
  ]),
]

test('the shipped Astra catalog is searchable and selectable with all four rate columns', () => {
  const snapshot = JSON.parse(readFileSync(new URL('../../../internal/source/codex/pricing_snapshot.json', import.meta.url), 'utf8'))
  const astra = snapshot.models['gpt-6-astra']
  const groups = buildPricingTargetGroups([{
    source_id: 'codex',
    currency: snapshot.currency,
    models: [{
      model_id: 'gpt-6-astra',
      targetable: true,
      rate: {
        input_per_million: astra.input_per_million,
        cached_input_per_million: astra.cached_input_per_million,
        cache_write_per_million: astra.cache_write_input_per_million,
        output_per_million: astra.output_per_million,
      },
    }],
  }], 'claude_code')
  const matches = filterPricingTargetGroups(groups, 'Astra')
  const target = findPricingTarget(matches, { source_id: 'codex', model_id: 'gpt-6-astra' })
  assert.ok(target)
  assert.deepEqual(formatPricingTargetRates(target.rate, target.currency), {
    input: '$10.00', cached: '$1.00', cacheWrite: '$12.50', output: '$50.00',
  })
})

test('lists the aliasing source first, then other catalogs by label', () => {
  const groups = buildPricingTargetGroups(CATALOGS, 'claude_code')

  assert.deepEqual(groups.map((group) => group.source_id), ['claude_code', 'codex', 'kimi_code'])
  assert.deepEqual(groups.map((group) => group.is_current_source), [true, false, false])
})

test('offers only targetable models and sorts them by display name', () => {
  const [codex] = buildPricingTargetGroups(CATALOGS, 'codex')

  assert.deepEqual(codex.options.map((option) => option.model_id), ['gpt-5.6', 'gpt-5.6-sol'])
  assert.equal(countPricingTargets(buildPricingTargetGroups(CATALOGS, 'codex')), 5)
})

test('drops a catalog whose models are all unpriced', () => {
  const groups = buildPricingTargetGroups([catalog('codex', 'Codex', [['x', 0, 0, false]])], 'codex')
  assert.deepEqual(groups, [])
})

test('falls back to the source id when a catalog carries no label', () => {
  const [group] = buildPricingTargetGroups(
    [{ source_id: 'codex', currency: 'USD', models: [{ model_id: 'a', targetable: true, rate: { input_per_million: 1, cached_input_per_million: 0, output_per_million: 2 } }] }],
    'codex',
  )
  assert.equal(group.source_label, 'codex')
})

test('a model query narrows within groups and a source query keeps a whole catalog', () => {
  const groups = buildPricingTargetGroups(CATALOGS, 'claude_code')

  const byModel = filterPricingTargetGroups(groups, 'sol')
  assert.deepEqual(flattenPricingTargetGroups(byModel).map((option) => option.model_id), ['gpt-5.6-sol'])

  const bySource = filterPricingTargetGroups(groups, 'codex')
  assert.deepEqual(bySource.map((group) => group.source_id), ['codex'])
  assert.equal(bySource[0].options.length, 2, 'a source-name match keeps every model in that catalog')

  const byDisplayName = filterPricingTargetGroups(groups, 'kimi k3')
  assert.deepEqual(flattenPricingTargetGroups(byDisplayName).map((option) => option.model_id), ['kimi-k3'])

  assert.deepEqual(filterPricingTargetGroups(groups, 'nothing-here'), [])
  assert.equal(filterPricingTargetGroups(groups, '  '), groups)
})

test('finds a target only when both the source and the model match', () => {
  const groups = buildPricingTargetGroups(CATALOGS, 'claude_code')

  assert.equal(findPricingTarget(groups, { source_id: 'codex', model_id: 'gpt-5.6' })?.source_label, 'Codex')
  assert.equal(findPricingTarget(groups, { source_id: 'claude_code', model_id: 'gpt-5.6' }), null)
  assert.equal(findPricingTarget(groups, null), null)
})

test('formats rate columns and marks absent cached or cache-write prices', () => {
  const rates = formatPricingTargetRates(
    { input_per_million: 5, cached_input_per_million: 0.5, cache_write_per_million: 6.25, output_per_million: 30 },
    'USD',
  )
  assert.deepEqual(rates, { input: '$5.00', cached: '$0.50', cacheWrite: '$6.25', output: '$30.00' })

  const sparse = formatPricingTargetRates(
    { input_per_million: 1, cached_input_per_million: 0, output_per_million: 5 },
    'USD',
  )
  assert.equal(sparse.cached, '—')
  assert.equal(sparse.cacheWrite, '—', 'a catalog that bills cache writes at the input rate publishes no price')

  const other = formatPricingTargetRates(
    { input_per_million: 2, cached_input_per_million: 0, output_per_million: 8 },
    'CNY',
  )
  assert.equal(other.input, 'CNY 2')
})

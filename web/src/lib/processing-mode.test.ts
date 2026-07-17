import assert from 'node:assert/strict'
import test from 'node:test'
import {
  aggregateProcessingModeUsage,
  getProcessingModeMeta,
  getProcessingModePricingDisclosure,
  getProcessingModePricingLabel,
  PROCESSING_MODE_ORDER,
  REQUESTED_TIER_DISCLOSURE,
  resolveProcessingMode,
} from './processing-mode.ts'
import type { DimensionDayStats } from '../types/api.ts'

test('normalizes raw Codex request tiers to honest processing-mode labels', () => {
  const fast = getProcessingModeMeta(undefined, 'priority')
  assert.equal(fast.label, 'Fast requested')
  assert.equal(fast.color, 'var(--cat-1)')
  assert.equal(fast.tone, 'accent')
  assert.equal(getProcessingModeMeta(undefined, 'default').label, 'Standard requested')
  assert.equal(getProcessingModeMeta(undefined, 'flex').label, 'Flex requested')
  const unknown = getProcessingModeMeta(undefined, undefined)
  assert.equal(unknown.label, 'Tier unknown')
  assert.equal(unknown.color, 'var(--fg-faint)')
  assert.equal(unknown.tone, 'neutral')
})

test('prefers the normalized processing mode over a conflicting raw tier', () => {
  assert.equal(resolveProcessingMode('standard', 'priority'), 'standard')
  assert.equal(resolveProcessingMode('unknown', 'priority'), 'unknown')
})

test('treats unsupported values as unknown and keeps a stable display order', () => {
  assert.equal(resolveProcessingMode('turbo', 'expedited'), 'unknown')
  assert.deepEqual(PROCESSING_MODE_ORDER, ['fast', 'standard', 'flex', 'unknown'])
})

test('disclosure describes local request state without claiming the processing outcome', () => {
  assert.match(REQUESTED_TIER_DISCLOSURE, /Local Codex request setting/)
  assert.match(REQUESTED_TIER_DISCLOSURE, /not a server-confirmed processing outcome/)
  assert.match(REQUESTED_TIER_DISCLOSURE, /Fast uses Priority rates/)
  assert.match(REQUESTED_TIER_DISCLOSURE, /Flex uses Flex rates/)
  assert.match(REQUESTED_TIER_DISCLOSURE, /Tier unknown remains unknown and falls back to Standard rates/)
  assert.match(REQUESTED_TIER_DISCLOSURE, /not actual billed spend/i)
})

test('maps requested modes to their official USD API pricing labels and caveats', () => {
  assert.equal(getProcessingModePricingLabel('fast'), 'Priority API estimate')
  assert.equal(getProcessingModePricingLabel('flex'), 'Flex API estimate')
  assert.equal(getProcessingModePricingLabel('standard'), 'Standard API estimate')
  assert.equal(getProcessingModePricingLabel('unknown'), 'Standard API estimate')

  assert.match(getProcessingModePricingDisclosure('fast'), /Requested Fast → Priority API rates/)
  assert.match(getProcessingModePricingDisclosure('flex'), /Requested Flex → Flex API rates/)
  assert.match(getProcessingModePricingDisclosure('standard'), /Requested Standard → Standard API rates/)
  assert.match(getProcessingModePricingDisclosure('unknown'), /Tier unknown remains unknown/)
  for (const mode of PROCESSING_MODE_ORDER) {
    assert.match(getProcessingModePricingDisclosure(mode), /not actual billed spend/)
  }
})

test('aggregates messages and every disjoint token bucket by requested mode', () => {
  const rows: DimensionDayStats[] = [
    {
      date: '2026-07-17',
      dimension_key: 'fast',
      sessions: 1,
      messages: 2,
      cost: 0.35,
      tokens: { input: 100, output: 20, reasoning: 5, cache: { read: 40, write: 10 } },
    },
    {
      date: '2026-07-17',
      dimension_key: 'priority',
      sessions: 1,
      messages: 1,
      cost: 0.05,
      tokens: { input: 25, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
    },
    {
      date: '2026-07-17',
      dimension_key: 'experimental',
      sessions: 1,
      messages: 3,
      cost: 0.1,
      tokens: { input: 50, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
    },
  ]

  const totals = aggregateProcessingModeUsage(rows)
  assert.deepEqual(
    { mode: totals[0].mode, messages: totals[0].messages, tokens: totals[0].tokens },
    { mode: 'fast', messages: 3, tokens: 200 },
  )
  assert.ok(Math.abs(totals[0].cost - 0.4) < 1e-12)
  assert.deepEqual(totals[3], { mode: 'unknown', messages: 3, tokens: 50, cost: 0.1 })
})

test('aggregates cost provenance independently for each requested mode', () => {
  const rows: DimensionDayStats[] = [
    {
      date: '2026-07-16', dimension_key: 'fast', sessions: 1, messages: 1, cost: 0.25,
      tokens: { input: 10, output: 2, reasoning: 0, cache: { read: 0, write: 0 } },
      cost_status: 'estimated_api_equivalent',
      cost_provenance: { status: 'estimated_api_equivalent', currency: 'USD', computed_count: 1 },
    },
    {
      date: '2026-07-17', dimension_key: 'priority', sessions: 1, messages: 2, cost: 0.5,
      tokens: { input: 20, output: 4, reasoning: 0, cache: { read: 0, write: 0 } },
      cost_status: 'estimated_api_equivalent',
      cost_provenance: { status: 'estimated_api_equivalent', currency: 'USD', computed_count: 2 },
    },
    {
      date: '2026-07-17', dimension_key: 'flex', sessions: 1, messages: 1, cost: 0,
      tokens: { input: 30, output: 5, reasoning: 0, cache: { read: 0, write: 0 } },
      cost_status: 'missing',
      cost_provenance: { status: 'missing', currency: 'USD', missing_count: 1 },
    },
  ]

  const totals = aggregateProcessingModeUsage(rows)
  assert.equal(totals[0].costStatus, 'estimated_api_equivalent')
  assert.equal(totals[0].costProvenance?.computed_count, 3)
  assert.equal(totals[2].cost, 0)
  assert.equal(totals[2].costStatus, 'missing')
  assert.equal(totals[2].costProvenance?.missing_count, 1)
  assert.equal(totals[1].costStatus, undefined)
})

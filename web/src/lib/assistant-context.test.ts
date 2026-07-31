import assert from 'node:assert/strict'
import test from 'node:test'
import { DAILY_PERIOD_VALUES } from '../types/api.ts'
import { buildAssistantTimeContext } from './assistant-context.ts'

const NOW = new Date('2026-07-31T18:00:00Z')

test('assistant preset contract contains only the supported analytics periods', () => {
  assert.deepEqual(DAILY_PERIOD_VALUES, [
    '1h', '6h', '12h', '24h', '72h', '1d', '7d', '14d', '30d', '1y', 'all',
  ])

  for (const preset of DAILY_PERIOD_VALUES) {
    assert.deepEqual(buildAssistantTimeContext({ mode: 'preset', preset }, NOW), {
      context: { period: preset },
      error: null,
    })
  }
  assert.deepEqual(buildAssistantTimeContext({ mode: 'preset', preset: '90d' }, NOW), {
    context: {},
    error: 'Choose a supported analytics period before asking the assistant.',
  })
})

test('assistant custom context uses structured closed and open ranges', () => {
  assert.deepEqual(buildAssistantTimeContext({
    mode: 'custom',
    preset: '7d',
    customRange: { from: '2026-07-01', to: '2026-07-15' },
  }, NOW), {
    context: { from: '2026-07-01', to: '2026-07-15' },
    error: null,
  })

  assert.deepEqual(buildAssistantTimeContext({
    mode: 'custom',
    preset: '7d',
    customRange: { from: '2026-07-01' },
  }, NOW), {
    context: { from: '2026-07-01' },
    error: null,
  })
})

test('assistant blocks unfinished, malformed, inverted, and future custom ranges', () => {
  const invalidRanges = [
    undefined,
    { from: 'July 1' },
    { from: '2026-02-31' },
    { from: '2026-07-20', to: '2026-07-01' },
    { from: '2026-08-01' },
    { from: '2026-07-01', to: '2026-08-01' },
  ]

  for (const customRange of invalidRanges) {
    assert.deepEqual(buildAssistantTimeContext({
      mode: 'custom',
      preset: '7d',
      customRange,
    }, NOW), {
      context: {},
      error: 'Choose a valid current or past custom date range before asking the assistant.',
    })
  }
})

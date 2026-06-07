import assert from 'node:assert/strict'
import test from 'node:test'
import { customRangeLabel, periodTriggerLabel, presetLabel } from './period-label.ts'

test('presetLabel maps every preset to a human label', () => {
  assert.equal(presetLabel('1h'), 'Last hour')
  assert.equal(presetLabel('7d'), 'Last 7 days')
  assert.equal(presetLabel('1d'), 'Today')
  assert.equal(presetLabel('all'), 'All time')
})

test('customRangeLabel collapses the leading year within the same year', () => {
  assert.equal(customRangeLabel({ from: '2026-04-01', to: '2026-04-15' }), 'Apr 1 – Apr 15, 2026')
})

test('customRangeLabel keeps both years across a year boundary', () => {
  assert.equal(customRangeLabel({ from: '2025-12-20', to: '2026-01-05' }), 'Dec 20, 2025 – Jan 5, 2026')
})

test('customRangeLabel renders an open end as Now', () => {
  assert.equal(customRangeLabel({ from: '2026-04-01' }), 'Apr 1, 2026 – Now')
})

test('periodTriggerLabel dispatches on mode and sentinel', () => {
  assert.equal(periodTriggerLabel({ mode: 'preset', preset: '30d' }), 'Last 30 days')
  assert.equal(periodTriggerLabel({ mode: 'custom', preset: '7d' }), 'Custom range')
  assert.equal(
    periodTriggerLabel({ mode: 'custom', preset: '7d', customRange: { from: '2026-04-01', to: '2026-04-15' } }),
    'Apr 1 – Apr 15, 2026',
  )
})

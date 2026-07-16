import assert from 'node:assert/strict'
import test from 'node:test'
import { clampPercent, formatQuotaCurrency, formatResetCountdown, formatResetLabel, providerMeta, quotaTone, windowLabel } from './quotas.ts'

test('windowLabel derives from actual window duration, not the id', () => {
  assert.equal(windowLabel({ id: '5h', used_percent: 0, window_minutes: 300 }), '5h')
  // MiniMax reports a 4-hour interval under the generic short-window id
  assert.equal(windowLabel({ id: '5h', used_percent: 0, window_minutes: 240 }), '4h')
  assert.equal(windowLabel({ id: 'weekly', used_percent: 0, window_minutes: 10080 }), 'week')
  assert.equal(windowLabel({ id: '5h', used_percent: 0, window_minutes: 90 }), '90m')
  assert.equal(windowLabel({ id: '5h', used_percent: 0, window_minutes: 2880 }), '2d')
})

test('windowLabel falls back to the id when duration is missing', () => {
  assert.equal(windowLabel({ id: '5h', used_percent: 0 }), '5h')
  assert.equal(windowLabel({ id: 'weekly', used_percent: 0 }), 'week')
})

test('windowLabel prefers a provider-supplied label', () => {
  assert.equal(windowLabel({ id: 'weekly', label: 'Weekly limit', used_percent: 0, window_minutes: 10080 }), 'week')
  assert.equal(windowLabel({ id: 'monthly', label: 'Monthly Kimi quota', used_percent: 0 }), 'Monthly Kimi quota')
})

test('formatResetCountdown renders compact remaining time', () => {
  const now = Date.UTC(2026, 6, 11, 12, 0, 0)
  const at = (deltaMinutes: number) => Math.floor(now / 1000) + deltaMinutes * 60
  assert.equal(formatResetCountdown(at(130), now), '2h 10m')
  assert.equal(formatResetCountdown(at(60), now), '1h')
  assert.equal(formatResetCountdown(at(45), now), '45m')
  assert.equal(formatResetCountdown(at(3 * 1440 + 4 * 60), now), '3d 4h')
  assert.equal(formatResetCountdown(at(2 * 1440), now), '2d')
  assert.equal(formatResetCountdown(at(-5), now), 'resets now')
})

test('formatResetLabel uses grammatical full reset copy', () => {
  const now = Date.UTC(2026, 6, 11, 12, 0, 0)
  const at = (deltaMinutes: number) => Math.floor(now / 1000) + deltaMinutes * 60
  assert.equal(formatResetLabel(at(130), now), 'resets in 2h 10m')
  assert.equal(formatResetLabel(at(-5), now), 'resets now')
})

test('quotaTone maps used percent to traffic-light tokens', () => {
  assert.equal(quotaTone(0), 'var(--success)')
  assert.equal(quotaTone(69.9), 'var(--success)')
  assert.equal(quotaTone(70), 'var(--warning)')
  assert.equal(quotaTone(89.9), 'var(--warning)')
  assert.equal(quotaTone(90), 'var(--danger)')
  assert.equal(quotaTone(120), 'var(--danger)')
})

test('clampPercent bounds provider-reported values for display', () => {
  assert.equal(clampPercent(-3), 0)
  assert.equal(clampPercent(42.5), 42.5)
  assert.equal(clampPercent(140), 100)
  assert.equal(clampPercent(Number.NaN), 0)
})

test('Kimi quota metadata and Extra Usage currency formatting are available', () => {
  assert.equal(providerMeta({ provider: 'kimi_code', label: 'Kimi Code', status: 'ok' }).label, 'Kimi Code')
  assert.equal(formatQuotaCurrency(12345, 'USD'), '$123.45')
  assert.equal(formatQuotaCurrency(5000, 'cny'), '¥50.00')
  assert.equal(formatQuotaCurrency(42, 'EUR'), '0.42 EUR')
})

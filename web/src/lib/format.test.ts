import assert from 'node:assert/strict'
import test from 'node:test'
import {
  formatCompactCurrency,
  formatCount,
  formatCurrency,
  formatAxisCurrency,
  formatCurrencyExact,
  formatDateTime,
  formatDuration,
  formatHour,
  formatShare,
  formatShortDate,
  formatShortId,
  formatShortWeekday,
} from './format.ts'

// Regression: Intl.DateTimeFormat.format() throws "Invalid time value" on an invalid
// Date. Hourly trend dates (e.g. "2026-06-08T00:00:00Z", used for the 1d / hour presets)
// previously crashed formatShortWeekday because it appended "T00:00:00Z" to an already-
// full ISO timestamp. These helpers must never throw — invalid input falls back to a string.

test('formatShortDate handles plain days and hourly ISO timestamps', () => {
  assert.equal(formatShortDate('2026-06-08'), 'Jun 8')
  assert.equal(formatShortDate('2026-06-08T14:00:00Z'), '14') // hourly → hour-of-day
})

test('formatShortWeekday does not throw on hourly ISO timestamps', () => {
  assert.doesNotThrow(() => formatShortWeekday('2026-06-08T00:00:00Z'))
  // 2026-06-08 is a Monday (UTC)
  assert.equal(formatShortWeekday('2026-06-08'), 'Mon')
  assert.equal(formatShortWeekday('2026-06-08T23:00:00Z'), 'Mon')
})

test('date formatters return a fallback (never throw) on malformed input', () => {
  assert.doesNotThrow(() => formatShortWeekday('not-a-date'))
  assert.doesNotThrow(() => formatShortDate('not-a-date'))
  assert.doesNotThrow(() => formatDateTime('not-a-date'))
  assert.doesNotThrow(() => formatHour('not-a-date'))
  assert.equal(formatShortWeekday('not-a-date'), '—')
  assert.equal(formatDateTime('not-a-date'), '—')
})

test('formatDateTime keeps the UTC suffix for valid timestamps', () => {
  assert.match(formatDateTime('2026-06-08T14:23:00Z'), /UTC$/)
})

// Costs used to render with up to six decimals everywhere ("$493.595289"),
// which clipped stat tiles and buried the figure that matters. Precision now
// follows magnitude; the exact value is still reachable for tooltips.
test('formatCurrency shows cents from a dollar up and two significant digits below it', () => {
  assert.equal(formatCurrency(493.595289), '$493.60')
  assert.equal(formatCurrency(2743.76593), '$2,743.77')
  assert.equal(formatCurrency(1), '$1.00')
  assert.equal(formatCurrency(0.5), '$0.50')
  assert.equal(formatCurrency(0.3158), '$0.32')
  assert.equal(formatCurrency(0.070423), '$0.07')
  assert.equal(formatCurrency(0.0312), '$0.031')
  assert.equal(formatCurrency(0.00123), '$0.0012')
  assert.equal(formatCurrency(0.000123), '$0.00012')
  assert.equal(formatCurrency(0.0000004), '$0.00')
  assert.equal(formatCurrency(0), '$0.00')
  assert.equal(formatCurrency(Number.NaN), '$0.00')
  assert.equal(formatCurrency(-12.345678), '-$12.35')
})

test('formatCompactCurrency abbreviates only from a million up', () => {
  assert.equal(formatCompactCurrency(999999.994), '$999,999.99')
  assert.equal(formatCompactCurrency(1_250_000), '$1.25M')
})

test('formatAxisCurrency fits a chart axis', () => {
  assert.equal(formatAxisCurrency(0), '$0.00')
  assert.equal(formatAxisCurrency(0.5), '$0.50')
  assert.equal(formatAxisCurrency(62.5), '$62.50')
  assert.equal(formatAxisCurrency(250), '$250')
  assert.equal(formatAxisCurrency(1000), '$1K')
  assert.equal(formatAxisCurrency(1250), '$1.25K')
  assert.equal(formatAxisCurrency(Number.NaN), '$0.00')
})

test('formatCurrencyExact keeps the stored six-decimal precision', () => {
  assert.equal(formatCurrencyExact(493.595289), '$493.595289')
  assert.equal(formatCurrencyExact(6), '$6.00')
})

test('formatCount agrees the noun with the number', () => {
  assert.equal(formatCount(1, 'session'), '1 session')
  assert.equal(formatCount(0, 'session'), '0 sessions')
  assert.equal(formatCount(1234, 'message'), '1,234 messages')
  assert.equal(formatCount(2, 'entry', 'entries'), '2 entries')
})

test('formatDuration picks the two most significant units', () => {
  assert.equal(formatDuration('2026-06-08T10:00:00Z', '2026-06-08T10:00:09Z'), '9s')
  assert.equal(formatDuration('2026-06-08T10:00:00Z', '2026-06-08T10:04:12Z'), '4m 12s')
  assert.equal(formatDuration('2026-06-08T10:00:00Z', '2026-06-08T12:30:00Z'), '2h 30m')
  assert.equal(formatDuration('2026-06-05T08:00:00Z', '2026-06-08T12:30:00Z'), '3d 4h')
  assert.equal(formatDuration('2026-06-08T10:00:00Z', '2026-06-08T09:00:00Z'), '0s')
  assert.equal(formatDuration('nope', '2026-06-08T09:00:00Z'), '—')
})

test('short ids are truncated with an ellipsis only when they are longer', () => {
  assert.equal(formatShortId('ses_0123456789abcdef'), 'ses_01234567…')
  assert.equal(formatShortId('short'), 'short')
  assert.equal(formatShortId('abcdefgh', 4), 'abcd…')
})

test('shares never round a real contribution down to nothing', () => {
  assert.equal(formatShare(42, 100), '42%')
  assert.equal(formatShare(1, 1000), '<1%')
  assert.equal(formatShare(0, 1000), '0%')
  assert.equal(formatShare(5, 0), '—')
})

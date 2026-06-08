import assert from 'node:assert/strict'
import test from 'node:test'
import { formatDateTime, formatHour, formatShortDate, formatShortWeekday } from './format.ts'

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

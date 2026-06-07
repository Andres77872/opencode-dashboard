import type { CustomPeriod, DailyPeriod, PeriodMode } from '../types/api'

/**
 * Human-readable labels for the period picker trigger and screen-reader text.
 * Single source of truth so the visible trigger and aria-label never drift.
 *
 * The preset sidebar shows the terse codes ("7d", "1d", …) as visible text and
 * uses these full labels as each option's aria-label.
 */
export const PRESET_LABELS: Record<DailyPeriod, string> = {
  '1h': 'Last hour',
  '6h': 'Last 6 hours',
  '12h': 'Last 12 hours',
  '24h': 'Last 24 hours',
  '72h': 'Last 72 hours',
  '1d': 'Today',
  '7d': 'Last 7 days',
  '14d': 'Last 14 days',
  '30d': 'Last 30 days',
  '1y': 'Last year',
  all: 'All time',
}

export interface PeriodLabelState {
  mode: PeriodMode
  preset: DailyPeriod
  customRange?: CustomPeriod
}

// UTC formatters keep labels deterministic regardless of the viewer's timezone,
// consistent with the UTC date handling in format.ts.
const dayMonthFormatter = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  timeZone: 'UTC',
})
const dayMonthYearFormatter = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  year: 'numeric',
  timeZone: 'UTC',
})

function parseUtcDay(value: string): Date {
  return new Date(`${value}T00:00:00Z`)
}

export function presetLabel(preset: DailyPeriod): string {
  return PRESET_LABELS[preset] ?? preset
}

/**
 * "Apr 1 – Apr 15, 2026" (same year collapses the leading year),
 * "Apr 1, 2025 – Apr 15, 2026" (cross-year), or "Apr 1, 2026 – Now" (open end).
 */
export function customRangeLabel(range: CustomPeriod): string {
  const fromDate = parseUtcDay(range.from)

  if (!range.to) {
    return `${dayMonthYearFormatter.format(fromDate)} – Now`
  }

  const toDate = parseUtcDay(range.to)
  const sameYear = fromDate.getUTCFullYear() === toDate.getUTCFullYear()
  const fromLabel = sameYear ? dayMonthFormatter.format(fromDate) : dayMonthYearFormatter.format(fromDate)
  return `${fromLabel} – ${dayMonthYearFormatter.format(toDate)}`
}

/** Trigger / aria label for the current period selection. */
export function periodTriggerLabel(state: PeriodLabelState): string {
  if (state.mode === 'custom') {
    return state.customRange?.from ? customRangeLabel(state.customRange) : 'Custom range'
  }
  return presetLabel(state.preset)
}

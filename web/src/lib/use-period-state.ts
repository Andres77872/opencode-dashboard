import { useSearchParams } from 'react-router-dom'
import { isDailyPeriod, isValidCustomRange, type CustomPeriod, type DailyPeriod, type PeriodMode } from '../types/api'
import { getStoredPeriod } from './persisted-prefs.ts'

export interface PeriodState {
  mode: PeriodMode
  preset: DailyPeriod
  customRange?: CustomPeriod
}

/**
 * Serializes a custom period into a cache key string.
 * Format: "from_{YYYY-MM-DD}_to_{YYYY-MM-DD}" or "from_{YYYY-MM-DD}_to__now__"
 */
export function serializeCustomPeriod(from: string, to?: string): string {
  if (to) {
    return `from_${from}_to_${to}`
  }
  return `from_${from}_to__now__`
}

/**
 * Reads ?period, ?from, ?to from URL search params and returns
 * the current period state (mode, preset, customRange).
 *
 * - If `from` is present → mode = 'custom'
 * - If only `period` (or neither) → mode = 'preset', default '7d'
 */
export function usePeriodState(): PeriodState {
  const [searchParams] = useSearchParams()

  const from = searchParams.get('from')
  const to = searchParams.get('to') ?? undefined
  const period = searchParams.get('period')
  const modeParam = searchParams.get('mode')

  // When the URL carries no time-range params, restore the last persisted range so
  // the selection survives navigation between views and full reloads. Any explicit
  // URL param (shared link, back/forward) still takes precedence below.
  if (!from && !period && !modeParam) {
    const stored = getStoredPeriod()
    if (stored?.from && isValidCustomRange(stored.from, stored.to)) {
      return { mode: 'custom', preset: '7d', customRange: { from: stored.from, to: stored.to } }
    }
    if (stored?.period && isDailyPeriod(stored.period)) {
      return { mode: 'preset', preset: stored.period }
    }
  }

  if (from) {
    // Validate — if invalid, fall back to preset
    if (!isValidCustomRange(from, to)) {
      return { mode: 'preset', preset: '7d' }
    }
    return {
      mode: 'custom',
      preset: '7d',
      customRange: { from, to },
    }
  }

  // mode=custom sentinel — user clicked Custom but hasn't picked a date yet
  if (modeParam === 'custom') {
    return { mode: 'custom', preset: '7d' }
  }

  // Preset mode (or default)
  const rawPeriod = period ?? '7d'
  return {
    mode: 'preset',
    preset: isDailyPeriod(rawPeriod) ? rawPeriod : '7d',
  }
}

/**
 * Normalizes URL search params based on current period state.
 *
 * This intentionally clones and edits only period/range keys so unrelated
 * share-state such as `source=claude_code`, pagination, filters, or project
 * drilldowns survive period changes.
 */
export function applyPeriodToUrl(
  searchParams: URLSearchParams,
  state: { mode: PeriodMode; preset?: DailyPeriod; customRange?: CustomPeriod },
): URLSearchParams {
  const next = new URLSearchParams(searchParams)

  if (state.mode === 'custom') {
    next.delete('period')
    next.delete('mode')
    if (state.customRange?.from) {
      // User has selected a date — write from/to to URL
      next.set('from', state.customRange.from)
      if (state.customRange.to) {
        next.set('to', state.customRange.to)
      } else {
        next.delete('to')
      }
    } else {
      // User clicked Custom but hasn't picked a date yet — use mode=custom sentinel
      next.delete('from')
      next.delete('to')
      next.set('mode', 'custom')
    }
  } else {
    // Preset mode
    next.delete('from')
    next.delete('to')
    next.delete('mode')
    next.set('period', state.preset ?? '7d')
  }

  return next
}

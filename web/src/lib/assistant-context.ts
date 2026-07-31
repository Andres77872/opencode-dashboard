import type { AssistantRequestContext } from '../types/assistant'
import {
  isDailyPeriod,
  isValidCustomRange,
  type CustomPeriod,
  type PeriodMode,
} from '../types/api.ts'

export interface AssistantPeriodState {
  mode: PeriodMode
  /** Kept as a string so this browser/API boundary remains runtime-validated. */
  preset: string
  customRange?: CustomPeriod
}

export type AssistantTimeContext = Pick<AssistantRequestContext, 'period' | 'from' | 'to'>

export interface AssistantTimeContextResult {
  context: AssistantTimeContext
  error: string | null
}

const INVALID_PRESET_MESSAGE = 'Choose a supported analytics period before asking the assistant.'
const INVALID_CUSTOM_RANGE_MESSAGE = 'Choose a valid current or past custom date range before asking the assistant.'

function utcDate(now: Date): string | undefined {
  if (!Number.isFinite(now.getTime())) return undefined
  return now.toISOString().slice(0, 10)
}

/**
 * Converts dashboard period state to the analytics tools' wire contract.
 * Invalid or unfinished selections are reported to the composer. They must not
 * silently become the backend's default period because that would answer a
 * different time range than the dashboard currently shows.
 */
export function buildAssistantTimeContext(
  state: AssistantPeriodState,
  now: Date = new Date(),
): AssistantTimeContextResult {
  if (state.mode === 'preset') {
    return isDailyPeriod(state.preset)
      ? { context: { period: state.preset }, error: null }
      : { context: {}, error: INVALID_PRESET_MESSAGE }
  }

  const range = state.customRange
  const today = utcDate(now)
  if (!range?.from || !today || !isValidCustomRange(range.from, range.to)) {
    return { context: {}, error: INVALID_CUSTOM_RANGE_MESSAGE }
  }
  if (range.from > today || (range.to !== undefined && range.to > today)) {
    return { context: {}, error: INVALID_CUSTOM_RANGE_MESSAGE }
  }

  return {
    context: range.to === undefined
      ? { from: range.from }
      : { from: range.from, to: range.to },
    error: null,
  }
}

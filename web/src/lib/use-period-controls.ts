import { useCallback, useEffect, useRef } from 'react'
import { useSearchParams } from 'react-router-dom'
import { applyPeriodToUrl, serializeCustomPeriod, usePeriodState, type PeriodState } from './use-period-state'
import { setStoredPeriod } from './persisted-prefs.ts'
import type { CustomPeriod, DailyPeriod, PeriodMode } from '../types/api'

export interface UsePeriodControlsOptions {
  /** Extra mutation applied to the URL params atomically with the period write (e.g. page=1). */
  mutateUrl?: (params: URLSearchParams) => void
}

/**
 * Runs `reset` whenever `key` changes (skipping the first render).
 *
 * Views use this to drop local state — sort, metric, an open drawer — that no
 * longer makes sense once the period or source changed.
 *
 * This replaces an `onChange` callback on usePeriodControls, which could never
 * fire: onChange is only invoked from the period picker's own handlers, and the
 * picker is rendered solely by FilterBar (from its own usePeriodControls
 * instance). A view that passed onChange but didn't render a picker — every view
 * that passed it — was registering a callback nothing could call.
 */
export function useResetOnChange(key: string, reset: () => void) {
  const previousRef = useRef(key)

  useEffect(() => {
    if (previousRef.current === key) return
    previousRef.current = key
    reset()
    // `reset` is a fresh closure every render, so listing it would re-run this on
    // every render. The key comparison is what gates the call.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])
}

export interface PeriodPickerProps {
  mode: PeriodMode
  preset: DailyPeriod
  customRange?: CustomPeriod
  onPresetChange: (preset: DailyPeriod) => void
  onCustomRangeChange: (range: CustomPeriod) => void
}

export interface PeriodControls {
  /** Current decoded state from the URL. */
  state: PeriodState
  /** Source-agnostic cache/period key: preset string OR "from_..._to_...". */
  cacheKey: string
  /** Spread straight into <PeriodPicker {...pickerProps} />. */
  pickerProps: PeriodPickerProps
}

/**
 * Encapsulates the period state + cacheKey + URL handlers that were previously
 * copy-pasted across every stats view. Reuses usePeriodState / applyPeriodToUrl /
 * serializeCustomPeriod verbatim, so the URL and cache-key contract is unchanged.
 */
export function usePeriodControls(options: UsePeriodControlsOptions = {}): PeriodControls {
  const { mutateUrl } = options
  const [, setSearchParams] = useSearchParams()
  const state = usePeriodState()

  const cacheKey =
    state.mode === 'custom' && state.customRange
      ? serializeCustomPeriod(state.customRange.from, state.customRange.to)
      : state.preset

  const onPresetChange = useCallback(
    (preset: DailyPeriod) => {
      setStoredPeriod({ period: preset })
      setSearchParams((previous) => {
        const next = applyPeriodToUrl(previous, { mode: 'preset', preset })
        mutateUrl?.(next)
        return next
      })
    },
    [mutateUrl, setSearchParams],
  )

  const onCustomRangeChange = useCallback(
    (range: CustomPeriod) => {
      setStoredPeriod({ from: range.from, to: range.to })
      setSearchParams((previous) => {
        const next = applyPeriodToUrl(previous, { mode: 'custom', customRange: range })
        mutateUrl?.(next)
        return next
      })
    },
    [mutateUrl, setSearchParams],
  )

  return {
    state,
    cacheKey,
    pickerProps: {
      mode: state.mode,
      preset: state.preset,
      customRange: state.customRange,
      onPresetChange,
      onCustomRangeChange,
    },
  }
}

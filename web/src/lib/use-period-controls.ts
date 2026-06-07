import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { applyPeriodToUrl, serializeCustomPeriod, usePeriodState, type PeriodState } from './use-period-state'
import type { CustomPeriod, DailyPeriod, PeriodMode } from '../types/api'

export interface UsePeriodControlsOptions {
  /** Fired before any URL change so a view can reset local state (sort/page/selection). */
  onChange?: () => void
  /** Extra mutation applied to the URL params atomically with the period write (e.g. page=1). */
  mutateUrl?: (params: URLSearchParams) => void
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
  const { onChange, mutateUrl } = options
  const [, setSearchParams] = useSearchParams()
  const state = usePeriodState()

  const cacheKey =
    state.mode === 'custom' && state.customRange
      ? serializeCustomPeriod(state.customRange.from, state.customRange.to)
      : state.preset

  const onPresetChange = useCallback(
    (preset: DailyPeriod) => {
      onChange?.()
      setSearchParams((previous) => {
        const next = applyPeriodToUrl(previous, { mode: 'preset', preset })
        mutateUrl?.(next)
        return next
      })
    },
    [onChange, mutateUrl, setSearchParams],
  )

  const onCustomRangeChange = useCallback(
    (range: CustomPeriod) => {
      onChange?.()
      setSearchParams((previous) => {
        const next = applyPeriodToUrl(previous, { mode: 'custom', customRange: range })
        mutateUrl?.(next)
        return next
      })
    },
    [onChange, mutateUrl, setSearchParams],
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

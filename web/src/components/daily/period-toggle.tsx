import { DateRangePicker, type DateRangePickerValue } from '../ui/date-range-picker'
import { SegmentedControl } from './segmented-control'
import { cn } from '../../lib/utils'
import { format } from 'date-fns'
import type { CustomPeriod, DailyPeriod, PeriodMode } from '../../types/api'

export type { PeriodMode }

/**
 * "Quick" vs "Custom":
 *
 *   Quick — one-click predefined durations (1h, 7d, All, …).
 *   Custom — manual date-range selection in a calendar popover.
 *
 * "Quick" is shorter, more active, and frames the contrast correctly:
 * the user picks a pre-baked range quickly, or takes control with a
 * custom selection.  "Presets" felt too technical and passive.
 */
const MODE_OPTIONS = [
  { label: 'Quick', value: 'preset' as const },
  { label: 'Custom', value: 'custom' as const },
]

interface PeriodToggleProps {
  mode: PeriodMode
  preset?: DailyPeriod
  customRange?: CustomPeriod
  onPresetChange: (preset: DailyPeriod) => void
  onCustomRangeChange: (range: CustomPeriod) => void
  onModeChange: (mode: PeriodMode) => void
  disabled?: boolean
}

const hourPresets: Array<{ label: string; value: DailyPeriod }> = [
  { label: '1h', value: '1h' },
  { label: '6h', value: '6h' },
  { label: '12h', value: '12h' },
  { label: '24h', value: '24h' },
  { label: '72h', value: '72h' },
]

const dayPresets: Array<{ label: string; value: DailyPeriod }> = [
  { label: '1d', value: '1d' },
  { label: '7d', value: '7d' },
  { label: '14d', value: '14d' },
  { label: '30d', value: '30d' },
  { label: '1y', value: '1y' },
  { label: 'All', value: 'all' },
]

const presetBtnBase =
  'inline-flex items-center justify-center whitespace-nowrap rounded-md px-2.5 h-7 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1'

function presetBtnClass(selected: boolean): string {
  if (selected) {
    return `${presetBtnBase} bg-primary text-primary-foreground shadow-sm`
  }
  return `${presetBtnBase} text-muted-foreground hover:bg-accent hover:text-accent-foreground`
}

export function PeriodToggle({
  mode,
  preset = '7d',
  customRange,
  onPresetChange,
  onCustomRangeChange,
  onModeChange,
  disabled = false,
}: PeriodToggleProps) {
  const isCustom = mode === 'custom'

  const handleCustomRangeChange = (range: DateRangePickerValue) => {
    const from = range.from ? format(range.from, 'yyyy-MM-dd') : customRange?.from
    const to = range.to ? format(range.to, 'yyyy-MM-dd') : undefined

    if (!from) return

    onCustomRangeChange({ from, to })
  }

  const handlePresetClick = (value: DailyPeriod) => {
    if (isCustom) {
      onModeChange('preset')
    }
    onPresetChange(value)
  }

  return (
    <div className="flex flex-col gap-3">
      {/* Mode switch: Quick | Custom */}
      <SegmentedControl
        ariaLabel="Period mode"
        value={mode}
        onChange={(value) => onModeChange(value)}
        options={MODE_OPTIONS}
        disabled={disabled}
        className="w-full"
      />

      {/*
       * Grid overlay — both modes are always in the DOM at the same grid cell.
       * The container height is driven by the TALLEST child (presets when they
       * wrap).  Switching modes toggles visibility only, so there is ZERO
       * layout shift.
       */}
      <div className="rounded-lg bg-muted/5 p-3">
        <div className="grid">
          {/* Quick-range presets (hours + days) */}
          <div
            className={cn(
              'flex flex-wrap items-center gap-x-2 gap-y-1.5 [grid-area:1/1]',
              isCustom && 'invisible',
            )}
            aria-hidden={isCustom}
          >
            {hourPresets.map((opt) => (
              <button
                key={opt.value}
                type="button"
                disabled={disabled}
                className={presetBtnClass(preset === opt.value)}
                onClick={() => handlePresetClick(opt.value)}
              >
                {opt.label}
              </button>
            ))}

            <div className="mx-1 h-5 w-px shrink-0 bg-border/50" aria-hidden="true" />

            {dayPresets.map((opt) => (
              <button
                key={opt.value}
                type="button"
                disabled={disabled}
                className={presetBtnClass(preset === opt.value)}
                onClick={() => handlePresetClick(opt.value)}
              >
                {opt.label}
              </button>
            ))}
          </div>

          {/* Custom date-range picker */}
          <div
            className={cn('[grid-area:1/1]', !isCustom && 'invisible')}
            aria-hidden={!isCustom}
          >
            <DateRangePicker
              value={{
                from: customRange?.from ? new Date(customRange.from + 'T00:00:00') : undefined,
                to: customRange?.to ? new Date(customRange.to + 'T00:00:00') : undefined,
              }}
              onChange={handleCustomRangeChange}
            />
          </div>
        </div>
      </div>
    </div>
  )
}

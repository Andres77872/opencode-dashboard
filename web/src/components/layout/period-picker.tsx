/* Vael period picker — presets + custom range (react-day-picker) in a Popover.
   Presentational: emits semantic callbacks from usePeriodControls (never
   touches the URL directly). */
import { useCallback, useState } from 'react'
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent } from 'react'
import { DayPicker, type DateRange } from 'react-day-picker'
import 'react-day-picker/style.css'
import { format, startOfDay } from 'date-fns'
import { Button, PickerButton, Popover } from '../vael/controls'
import { periodTriggerLabel, presetLabel } from '../../lib/period-label'
import type { CustomPeriod, DailyPeriod, PeriodMode } from '../../types/api'

export interface PeriodPickerProps {
  mode: PeriodMode
  preset: DailyPeriod
  customRange?: CustomPeriod
  onPresetChange: (preset: DailyPeriod) => void
  onCustomRangeChange: (range: CustomPeriod) => void
}

const PRESET_GROUPS: Array<{ label: string; items: readonly DailyPeriod[] }> = [
  { label: 'Hours', items: ['1h', '6h', '12h', '24h', '72h'] },
  { label: 'Days', items: ['1d', '7d', '14d', '30d', '1y', 'all'] },
]

function presetText(value: DailyPeriod): string {
  return value === 'all' ? 'All' : value
}

function parseLocalDay(value?: string): Date | undefined {
  if (!value) return undefined
  const d = new Date(`${value}T00:00:00`)
  return Number.isNaN(d.getTime()) ? undefined : d
}

const dayPickerStyle = {
  '--rdp-accent-color': 'var(--accent)',
  '--rdp-accent-background-color': 'var(--accent-soft)',
  '--rdp-day-width': '34px',
  '--rdp-day-height': '34px',
  '--rdp-day_button-width': '34px',
  '--rdp-day_button-height': '34px',
  margin: 0,
  color: 'var(--fg-secondary)',
  fontFamily: 'var(--font-ui)',
} as CSSProperties

export function PeriodPicker({ mode, preset, customRange, onPresetChange, onCustomRangeChange }: PeriodPickerProps) {
  const [pendingRange, setPendingRange] = useState<DateRange | undefined>(undefined)
  const [visibleMonth, setVisibleMonth] = useState<Date>(() => startOfDay(new Date()))
  const today = startOfDay(new Date())
  const customFrom = customRange?.from
  const customTo = customRange?.to

  // Each opening starts from the applied range, not from an abandoned draft.
  const handleOpenChange = useCallback((open: boolean) => {
    if (!open) return
    const fromD = parseLocalDay(customFrom)
    const toD = parseLocalDay(customTo)
    setPendingRange(fromD ? { from: fromD, to: toD } : undefined)
    setVisibleMonth(toD ?? fromD ?? startOfDay(new Date()))
  }, [customFrom, customTo])

  const triggerLabel = periodTriggerLabel({ mode, preset, customRange })

  return (
    <Popover
      portal
      flush
      closeOnClick={false}
      label="Time range"
      width={412}
      onOpenChange={handleOpenChange}
      trigger={(open, toggle) => (
        <PickerButton
          open={open}
          onClick={toggle}
          popup="dialog"
          icon="calendar"
          ariaLabel={`Time range: ${triggerLabel}`}
          onKeyDown={(e: ReactKeyboardEvent) => {
            if (e.key === 'ArrowDown' && !open) {
              e.preventDefault()
              toggle()
            }
          }}
        >
          {triggerLabel}
        </PickerButton>
      )}
    >
      {(close) => (
        <div className="vael-period-panel">
          <div className="vael-period-presets">
            {PRESET_GROUPS.map((group) => (
              <div key={group.label} role="group" aria-label={group.label} className="vael-period-group">
                <div className="vael-period-group-label">{group.label}</div>
                {group.items.map((value) => {
                  const active = mode === 'preset' && preset === value
                  return (
                    <button
                      key={value}
                      type="button"
                      aria-label={presetLabel(value)}
                      aria-pressed={active}
                      className="vael-period-preset"
                      onClick={() => {
                        onPresetChange(value)
                        close()
                      }}
                    >
                      {presetText(value)}
                    </button>
                  )
                })}
              </div>
            ))}
          </div>

          <div style={{ display: 'flex', flexDirection: 'column', minWidth: 0 }}>
            <div className="vael-daypicker" style={{ padding: 10, display: 'flex', justifyContent: 'center' }}>
              <DayPicker
                mode="range"
                numberOfMonths={1}
                selected={pendingRange}
                onSelect={setPendingRange}
                month={Number.isNaN(visibleMonth?.getTime?.() ?? NaN) ? today : visibleMonth}
                onMonthChange={setVisibleMonth}
                disabled={{ after: today }}
                style={dayPickerStyle}
              />
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, borderTop: '1px solid var(--border-default)', padding: '8px 12px' }}>
              <span aria-live="polite" style={{ font: '400 12px/1.3 var(--font-mono)', color: 'var(--fg-muted)' }}>
                {!pendingRange?.from && 'Pick a start date'}
                {pendingRange?.from && !pendingRange.to && `${format(pendingRange.from, 'MMM d, yyyy')} – now`}
                {pendingRange?.from && pendingRange.to && `${format(pendingRange.from, 'MMM d')} – ${format(pendingRange.to, 'MMM d, yyyy')}`}
              </span>
              <Button
                size="sm"
                variant="primary"
                disabled={!pendingRange?.from}
                onClick={() => {
                  if (!pendingRange?.from) return
                  onCustomRangeChange({
                    from: format(pendingRange.from, 'yyyy-MM-dd'),
                    to: pendingRange.to ? format(pendingRange.to, 'yyyy-MM-dd') : undefined,
                  })
                  close()
                }}
              >
                Apply
              </Button>
            </div>
          </div>
        </div>
      )}
    </Popover>
  )
}

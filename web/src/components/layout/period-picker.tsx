/* Vael period picker — presets + custom range (react-day-picker), self-contained
   open/outside-click state, Vael-styled. Presentational: emits semantic callbacks
   from usePeriodControls (never touches the URL directly). */
import { useEffect, useRef, useState } from 'react'
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent } from 'react'
import { DayPicker, type DateRange } from 'react-day-picker'
import 'react-day-picker/style.css'
import { format, startOfDay } from 'date-fns'
import { Icon } from '../vael/icon'
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
  const [open, setOpen] = useState(false)
  const [pendingRange, setPendingRange] = useState<DateRange | undefined>(undefined)
  const [visibleMonth, setVisibleMonth] = useState<Date>(() => startOfDay(new Date()))
  const ref = useRef<HTMLDivElement>(null)
  const today = startOfDay(new Date())

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const triggerLabel = periodTriggerLabel({ mode, preset, customRange })

  const handleOpen = () => {
    const next = !open
    if (next) {
      const fromD = parseLocalDay(customRange?.from)
      const toD = parseLocalDay(customRange?.to)
      setPendingRange(fromD ? { from: fromD, to: toD } : undefined)
      setVisibleMonth(toD ?? fromD ?? today)
    }
    setOpen(next)
  }

  const handlePreset = (value: DailyPeriod) => {
    onPresetChange(value)
    setOpen(false)
  }

  const applyCustom = () => {
    if (!pendingRange?.from) return
    onCustomRangeChange({
      from: format(pendingRange.from, 'yyyy-MM-dd'),
      to: pendingRange.to ? format(pendingRange.to, 'yyyy-MM-dd') : undefined,
    })
    setOpen(false)
  }

  const onTriggerKey = (e: ReactKeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'Enter') {
      e.preventDefault()
      if (!open) handleOpen()
    }
  }

  return (
    <div ref={ref} style={{ position: 'relative', display: 'inline-flex' }}>
      <button
        type="button"
        onClick={handleOpen}
        onKeyDown={onTriggerKey}
        aria-haspopup="dialog"
        aria-expanded={open}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 8,
          height: 32,
          padding: '0 11px',
          background: open ? 'var(--ink-700)' : 'var(--ink-750)',
          border: `1px solid ${open ? 'var(--border-accent)' : 'var(--border-default)'}`,
          borderRadius: 'var(--radius-md)',
          cursor: 'pointer',
          font: '500 13px/1 var(--font-ui)',
          color: 'var(--fg-primary)',
          whiteSpace: 'nowrap',
        }}
      >
        <Icon name="calendar" size={15} color="var(--fg-muted)" />
        {triggerLabel}
        <Icon name="chevron-down" size={14} color="var(--fg-muted)" style={{ transform: open ? 'rotate(180deg)' : 'none', transition: 'transform var(--dur-fast)' }} />
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Select time range"
          style={{
            position: 'absolute',
            top: 'calc(100% + 6px)',
            left: 0,
            zIndex: 60,
            display: 'flex',
            background: 'var(--ink-700)',
            border: '1px solid var(--border-strong)',
            borderRadius: 'var(--radius-lg)',
            boxShadow: 'var(--shadow-lg)',
            overflow: 'hidden',
          }}
        >
          {/* Presets */}
          <div style={{ width: 150, borderRight: '1px solid var(--border-default)', padding: 8, display: 'flex', flexDirection: 'column', gap: 2 }}>
            {PRESET_GROUPS.map((group) => (
              <div key={group.label}>
                <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-faint)', padding: '8px 9px 4px' }}>{group.label}</div>
                {group.items.map((value) => {
                  const active = mode === 'preset' && preset === value
                  return (
                    <button
                      key={value}
                      type="button"
                      aria-label={presetLabel(value)}
                      onClick={() => handlePreset(value)}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        width: '100%',
                        height: 30,
                        padding: '0 9px',
                        border: 'none',
                        borderRadius: 'var(--radius-sm)',
                        background: active ? 'var(--accent-soft)' : 'transparent',
                        color: active ? 'var(--blue-300)' : 'var(--fg-secondary)',
                        font: `${active ? 600 : 500} 13px/1 var(--font-ui)`,
                        cursor: 'pointer',
                        textAlign: 'left',
                      }}
                      onMouseEnter={(e) => { if (!active) e.currentTarget.style.background = 'var(--ink-650)' }}
                      onMouseLeave={(e) => { if (!active) e.currentTarget.style.background = 'transparent' }}
                    >
                      {presetText(value)}
                    </button>
                  )
                })}
              </div>
            ))}
          </div>

          {/* Calendar */}
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <div className="vael-daypicker" style={{ padding: 10 }}>
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
              <span style={{ font: '400 12px/1.3 var(--font-mono)', color: 'var(--fg-muted)' }}>
                {!pendingRange?.from && 'Pick a start date'}
                {pendingRange?.from && !pendingRange.to && `${format(pendingRange.from, 'MMM d, yyyy')} — ends “Now”`}
                {pendingRange?.from && pendingRange.to && `${format(pendingRange.from, 'MMM d')} – ${format(pendingRange.to, 'MMM d, yyyy')}`}
              </span>
              <button
                type="button"
                onClick={applyCustom}
                disabled={!pendingRange?.from}
                style={{
                  height: 28,
                  padding: '0 12px',
                  border: 'none',
                  borderRadius: 'var(--radius-md)',
                  background: pendingRange?.from ? 'var(--accent)' : 'var(--ink-600)',
                  color: pendingRange?.from ? 'var(--fg-on-accent)' : 'var(--fg-faint)',
                  font: '600 12px/1 var(--font-ui)',
                  cursor: pendingRange?.from ? 'pointer' : 'not-allowed',
                }}
              >
                Apply
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

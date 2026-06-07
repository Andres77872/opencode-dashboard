import { useEffect, useRef, useState } from 'react'
import { format, startOfDay, subMonths } from 'date-fns'
import { CalendarIcon, ChevronDown } from 'lucide-react'
import type { DateRange } from 'react-day-picker'
import { Button } from '../ui/button'
import { Calendar } from '../ui/calendar'
import { Popover, PopoverContent, PopoverTrigger } from '../ui/popover'
import { cn } from '../../lib/utils'
import { periodTriggerLabel, presetLabel } from '../../lib/period-label'
import { DAILY_PERIOD_VALUES, type CustomPeriod, type DailyPeriod, type PeriodMode } from '../../types/api'

interface PeriodPickerProps {
  mode: PeriodMode
  preset: DailyPeriod
  customRange?: CustomPeriod
  onPresetChange: (preset: DailyPeriod) => void
  onCustomRangeChange: (range: CustomPeriod) => void
  disabled?: boolean
  align?: 'start' | 'center' | 'end'
}

const PRESET_GROUPS: Array<{ label: string; items: readonly DailyPeriod[] }> = [
  { label: 'Hours', items: ['1h', '6h', '12h', '24h', '72h'] },
  { label: 'Days', items: ['1d', '7d', '14d', '30d', '1y', 'all'] },
]

// Flat order drives roving-tabindex keyboard navigation.
const ALL_PRESETS = DAILY_PERIOD_VALUES

function presetText(value: DailyPeriod): string {
  return value === 'all' ? 'All' : value
}

function parseLocalDay(value: string): Date {
  return new Date(`${value}T00:00:00`)
}

/** matchMedia hook — drives single vs dual month on small screens. */
function useMinWidth(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : true,
  )

  useEffect(() => {
    if (typeof window === 'undefined') return
    const mql = window.matchMedia(query)
    const onChange = () => setMatches(mql.matches)
    onChange()
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])

  return matches
}

/**
 * Unified time-range picker: one trigger button opens a single popover combining
 * the quick presets (sidebar) and a custom date range (calendar). The Quick/Custom
 * mode is inferred from the action — clicking a preset applies a preset; selecting a
 * calendar range + Apply applies a custom range. Presentational only: it never reads
 * or writes the URL, it just emits semantic callbacks (see usePeriodControls).
 */
export function PeriodPicker({
  mode,
  preset,
  customRange,
  onPresetChange,
  onCustomRangeChange,
  disabled = false,
  align = 'end',
}: PeriodPickerProps) {
  const [open, setOpen] = useState(false)
  const [pendingRange, setPendingRange] = useState<DateRange | undefined>(undefined)
  const [visibleMonth, setVisibleMonth] = useState<Date>(() => startOfDay(new Date()))
  const presetRefs = useRef<Partial<Record<DailyPeriod, HTMLButtonElement | null>>>({})

  const today = startOfDay(new Date())
  const isWide = useMinWidth('(min-width: 640px)')

  const triggerLabel = periodTriggerLabel({ mode, preset, customRange })
  const rovingValue: DailyPeriod = mode === 'preset' ? preset : ALL_PRESETS[0]

  // Initialize the in-popover selection from the committed range each time it opens,
  // so Apply has a meaningful before/after and re-opening reflects current state.
  const handleOpenChange = (next: boolean) => {
    if (next) {
      const initial: DateRange | undefined = customRange?.from
        ? {
            from: parseLocalDay(customRange.from),
            to: customRange.to ? parseLocalDay(customRange.to) : undefined,
          }
        : undefined
      setPendingRange(initial)
      const anchor = customRange?.to ?? customRange?.from
      const base = anchor ? parseLocalDay(anchor) : today
      setVisibleMonth(isWide ? subMonths(base, 1) : base)
    }
    setOpen(next)
  }

  const handlePresetClick = (value: DailyPeriod) => {
    onPresetChange(value)
    setOpen(false)
  }

  const handlePresetKeyDown = (event: React.KeyboardEvent, index: number) => {
    let nextIndex: number | null = null
    switch (event.key) {
      case 'ArrowDown':
      case 'ArrowRight':
        nextIndex = (index + 1) % ALL_PRESETS.length
        break
      case 'ArrowUp':
      case 'ArrowLeft':
        nextIndex = (index - 1 + ALL_PRESETS.length) % ALL_PRESETS.length
        break
      case 'Home':
        nextIndex = 0
        break
      case 'End':
        nextIndex = ALL_PRESETS.length - 1
        break
      default:
        return
    }
    event.preventDefault()
    presetRefs.current[ALL_PRESETS[nextIndex]]?.focus()
  }

  const applyCustom = () => {
    if (!pendingRange?.from) return
    onCustomRangeChange({
      from: format(pendingRange.from, 'yyyy-MM-dd'),
      to: pendingRange.to ? format(pendingRange.to, 'yyyy-MM-dd') : undefined,
    })
    setOpen(false)
  }

  const announcement = !pendingRange?.from
    ? ''
    : pendingRange.to
      ? `Selected ${format(pendingRange.from, 'MMMM d')} to ${format(pendingRange.to, 'MMMM d, yyyy')}`
      : `Start date ${format(pendingRange.from, 'MMMM d, yyyy')} selected, choose an end date or apply`

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          disabled={disabled}
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-label={`Time range: ${triggerLabel}`}
          className="min-w-[10rem] justify-between gap-2 font-normal"
        >
          <span className="flex min-w-0 items-center gap-2">
            <CalendarIcon className="size-4 shrink-0 opacity-70" />
            <span className="truncate">{triggerLabel}</span>
          </span>
          <ChevronDown className="size-4 shrink-0 opacity-60" />
        </Button>
      </PopoverTrigger>

      <PopoverContent
        align={align}
        sideOffset={6}
        className="w-auto max-w-[calc(100vw-2rem)] p-0"
      >
        <div className="flex flex-col sm:flex-row">
          {/* Preset sidebar */}
          <div
            role="listbox"
            aria-label="Quick ranges"
            aria-orientation="vertical"
            className="flex flex-row flex-wrap gap-1 border-b border-border/60 p-2 sm:w-40 sm:flex-col sm:flex-nowrap sm:border-b-0 sm:border-r"
          >
            {PRESET_GROUPS.map((group) => (
              <div key={group.label} className="contents sm:block">
                <div className="hidden w-full px-1 pt-1 pb-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground sm:block">
                  {group.label}
                </div>
                {group.items.map((value) => {
                  const active = mode === 'preset' && preset === value
                  const index = ALL_PRESETS.indexOf(value)
                  return (
                    <button
                      key={value}
                      ref={(node) => {
                        presetRefs.current[value] = node
                      }}
                      type="button"
                      role="option"
                      aria-selected={active}
                      aria-label={presetLabel(value)}
                      tabIndex={value === rovingValue ? 0 : -1}
                      disabled={disabled}
                      onClick={() => handlePresetClick(value)}
                      onKeyDown={(event) => handlePresetKeyDown(event, index)}
                      className={cn(
                        'inline-flex h-7 items-center justify-center rounded-md px-2.5 text-xs font-medium transition-colors',
                        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                        'sm:w-full sm:justify-start sm:px-3',
                        active
                          ? 'bg-accent text-accent-foreground'
                          : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                      )}
                    >
                      {presetText(value)}
                    </button>
                  )
                })}
              </div>
            ))}
          </div>

          {/* Calendar + footer */}
          <div className="flex flex-col">
            <Calendar
              mode="range"
              navLayout="around"
              numberOfMonths={isWide ? 2 : 1}
              selected={pendingRange}
              onSelect={(range) => setPendingRange(range)}
              month={visibleMonth}
              onMonthChange={setVisibleMonth}
              disabled={{ after: today }}
              autoFocus
            />

            <div className="flex items-center justify-between gap-3 border-t border-border/60 px-3 py-2">
              <div className="min-w-0 text-xs">
                {!pendingRange?.from && <span className="text-muted-foreground">Pick a start date</span>}
                {pendingRange?.from && !pendingRange.to && (
                  <span className="text-muted-foreground">
                    {format(pendingRange.from, 'MMM d, yyyy')} — Apply ends “Now”
                  </span>
                )}
                {pendingRange?.from && pendingRange.to && (
                  <span className="font-medium text-foreground">
                    {format(pendingRange.from, 'MMM d')} – {format(pendingRange.to, 'MMM d, yyyy')}
                  </span>
                )}
              </div>
              <Button size="sm" onClick={applyCustom} disabled={disabled || !pendingRange?.from}>
                Apply
              </Button>
            </div>
          </div>
        </div>

        <span className="sr-only" aria-live="polite">
          {announcement}
        </span>
      </PopoverContent>
    </Popover>
  )
}

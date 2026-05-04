import { format } from 'date-fns'
import { CalendarIcon } from 'lucide-react'
import { useState } from 'react'
import type { DateRange } from 'react-day-picker'
import { Button } from './button'
import { Calendar } from './calendar'
import { Popover, PopoverContent, PopoverTrigger } from './popover'
import { cn } from '../../lib/utils'

export interface DateRangePickerValue {
  from?: Date
  to?: Date
}

interface DateRangePickerProps {
  value: DateRangePickerValue
  onChange: (range: DateRangePickerValue) => void
}

/**
 * Date range picker with a single range-mode Calendar popover.
 * Uses react-day-picker's mode="range" for native range highlighting.
 * Inline validation cues are shown in the calendar footer.
 */
export function DateRangePicker({ value, onChange }: DateRangePickerProps) {
  const [open, setOpen] = useState(false)

  const selectedRange: DateRange | undefined =
    value.from ? { from: value.from, to: value.to } : undefined

  const hasValue = !!value.from
  const hasFullRange = hasValue && !!value.to

  const handleSelect = (range: DateRange | undefined) => {
    if (!range) {
      // User clicked the same selected range day → clear all
      onChange({})
      return
    }

    // If only from is set (user picked first day but not second yet),
    // keep the popover open so they can pick the end date
    if (range.from && !range.to) {
      onChange({ from: range.from })
      return
    }

    // Both from and to are set → range complete, close popover
    if (range.from && range.to) {
      onChange({ from: range.from, to: range.to })
      setOpen(false)
    }
  }

  /**
   * Inline validation footer rendered inside the calendar itself.
   * Shows the selection state so the user always knows what to do next.
   */
  const calendarFooter = (
    <div className="border-t border-border/50 px-3 py-2 text-xs text-muted-foreground">
      {!hasValue && <span>Select a start date to begin range</span>}
      {hasValue && !value.to && <span>Now select an end date to complete the range</span>}
      {hasFullRange && (
        <span>
          Range: {format(value.from!, 'MMM d')} &mdash; {format(value.to!, 'MMM d, yyyy')}
        </span>
      )}
    </div>
  )

  const formatRangeLabel = () => {
    if (!hasValue) return <span>Select date range</span>
    const fromStr = format(value.from!, 'MMM d, yyyy')
    if (!value.to) return <span>{fromStr} &mdash; ?</span>
    return <span>{fromStr} &mdash; {format(value.to!, 'MMM d, yyyy')}</span>
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className={cn(
            'w-full justify-start text-left font-normal',
            !hasValue && 'text-muted-foreground',
          )}
        >
          <CalendarIcon className="mr-2 h-4 w-4 shrink-0" />
          <span className="min-w-0 truncate">{formatRangeLabel()}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto p-0" align="start" sideOffset={6}>
        <div className="flex flex-col">
          {/* Calendar with inline validation footer */}
          <Calendar
            mode="range"
            selected={selectedRange}
            onSelect={handleSelect}
            initialFocus
            numberOfMonths={1}
            footer={calendarFooter}
          />
        </div>
      </PopoverContent>
    </Popover>
  )
}

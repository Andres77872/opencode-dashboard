import { DayPicker } from 'react-day-picker'
import type { ClassNames } from 'react-day-picker'
import { cn } from '../../lib/utils'

export type CalendarProps = React.ComponentProps<typeof DayPicker>

/**
 * Calendar wrapper over react-day-picker's DayPicker (v9 API).
 *
 * Uses v9 classNames exclusively. v8 deprecated keys (cell, day, caption,
 * table, head_row, head_cell, row, day_today, day_outside, day_disabled,
 * day_selected, day_range_*, nav_button, nav_button_previous/next)
 * are silently ignored by the runtime — see DeprecatedUI in react-day-picker UI.ts.
 *
 * Key DOM structure (v9):
 *   <div class="months">
 *     <div class="month">
 *       <button class="button_previous" />
 *       <div class="month_caption"><span class="caption_label" /></div>
 *       <button class="button_next" />
 *       <table class="month_grid">
 *         <thead><tr class="weekdays"><th class="weekday" />...</tr></thead>
 *         <tbody class="weeks">
 *           <tr class="week">
 *             <td class="day" role="gridcell" aria-selected="true">
 *               <button class="day_button" />
 *             </td>
 *           </tr>
 *         </tbody>
 *       </table>
 *     </div>
 *   </div>
 *
 * Modifier classes (today, outside, disabled, selected, range_*) are
 * applied to the <td> (day) element via getClassNamesForModifiers.
 * The day_button (button) only gets its base class — no aria modifiers.
 */
export function Calendar({ className, classNames, showOutsideDays = true, ...props }: CalendarProps) {
  return (
    <DayPicker
      data-slot="calendar"
      showOutsideDays={showOutsideDays}
      className={cn('p-3', className)}
      classNames={
        {
          // ── Layout containers ──────────────────────────
          months: 'flex flex-col sm:flex-row gap-2',
          month: 'flex flex-wrap items-center justify-center relative',

          // ── Month caption ───────────────────────────────
          // v9: month_caption replaces deprecated "caption"
          month_caption:
            'flex items-center justify-center h-7 text-sm font-medium mx-8',
          caption_label: 'text-sm font-medium',

          // ── Navigation buttons ──────────────────────────
          // v9: button_previous / button_next replace deprecated
          //     "nav_button", "nav_button_previous", "nav_button_next"
          nav: 'hidden', // hide outer nav wrapper; use per-month buttons
          button_previous: cn(
            'absolute left-0 top-0 inline-flex items-center justify-center',
            'rounded-md text-sm font-medium transition-colors',
            'h-7 w-7 bg-transparent p-0',
            'hover:bg-accent hover:text-accent-foreground',
          ),
          button_next: cn(
            'absolute right-0 top-0 inline-flex items-center justify-center',
            'rounded-md text-sm font-medium transition-colors',
            'h-7 w-7 bg-transparent p-0',
            'hover:bg-accent hover:text-accent-foreground',
          ),

          // ── Grid wrapper ────────────────────────────────
          // v9: month_grid replaces deprecated "table" — renders <table>
          month_grid: 'w-full border-collapse mt-1',

          // v9: weekdays replaces deprecated "head_row" — renders <thead><tr>
          weekdays: '',

          // v9: weekday replaces deprecated "head_cell" — renders <th>
          weekday: 'text-muted-foreground w-8 font-normal text-[0.8rem] pb-1',

          // v9: week replaces deprecated "row" — renders <tr>
          week: '',

          // Unused — weeks is the <tbody>, no custom styling needed
          weeks: '',

          // ── Day cell (<td>) ─────────────────────────────
          // v9: day replaces deprecated "cell" — renders <td>
          // aria-selected and modifier classes (selected, range_*, etc.)
          // are applied to this element by the runtime.
          day: cn(
            'text-center text-sm p-0 relative h-8 w-8',
            'focus-within:relative focus-within:z-20',
          ),

          // ── Day button (<button>) ───────────────────────
          // v9: day_button replaces deprecated "day" — renders <button>
          // NOTE: aria-selected is NOT on the button in v9 (it's on the <td>).
          // Selection styling comes via modifier classes on <td>.
          day_button: cn(
            'inline-flex items-center justify-center rounded-md',
            'text-sm font-medium h-8 w-8 p-0',
            'hover:bg-accent/70 hover:text-accent-foreground',
            'focus-visible:outline-none focus-visible:ring-2',
            'focus-visible:ring-ring focus-visible:ring-offset-2',
          ),

          // ── DayFlag modifiers (v9, replace v8 "day_today" etc.) ──
          today: 'ring-1 ring-inset ring-accent/60 text-accent-foreground',
          outside: 'text-muted-foreground/60',
          disabled:
            'text-muted-foreground/40 hover:bg-transparent hover:text-muted-foreground/40',

          // ── SelectionState modifiers (v9, replace v8 "day_selected" etc.) ──
          selected:
            'bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground focus:bg-primary focus:text-primary-foreground',
          range_start:
            'range-start rounded-l-md bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground focus:bg-primary focus:text-primary-foreground',
          range_end:
            'range-end rounded-r-md bg-primary text-primary-foreground hover:bg-primary hover:text-primary-foreground focus:bg-primary focus:text-primary-foreground',
          range_middle:
            'bg-accent/40 text-accent-foreground',

          // ── Footer ─────────────────────────────────────
          footer:
            'border-t border-border/50 mt-2 pt-2 text-xs text-muted-foreground',

          // ── Chevron (used by nav/dropdown buttons) ─────
          chevron: 'h-4 w-4',
        } satisfies Partial<ClassNames>
      }
      {...props}
    />
  )
}

import type { DailyPeriod } from '../../types/api'
import { cn } from '../../lib/utils'

interface PeriodToggleProps {
  value: DailyPeriod
  onChange: (value: DailyPeriod) => void
  disabled?: boolean
}

const options: Array<{ label: string; value: DailyPeriod }> = [
  { label: '7 days', value: '7d' },
  { label: '30 days', value: '30d' },
]

export function PeriodToggle({ value, onChange, disabled = false }: PeriodToggleProps) {
  return (
    <div
      role="group"
      aria-label="Daily period"
      className="inline-flex rounded-xl border border-border/70 bg-panel/80 p-1 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)]"
    >
      {options.map((option) => {
        const active = option.value === value

        return (
          <button
            key={option.value}
            type="button"
            aria-pressed={active}
            disabled={disabled}
            onClick={() => onChange(option.value)}
            className={cn(
              'rounded-lg px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/70 disabled:cursor-not-allowed disabled:opacity-50',
              active
                ? 'bg-accent text-accent-foreground shadow-[0_0_0_1px_color-mix(in_oklab,var(--color-accent)_55%,transparent)]'
                : 'text-muted-foreground hover:bg-white/6 hover:text-foreground',
            )}
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}

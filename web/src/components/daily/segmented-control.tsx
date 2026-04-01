import { cn } from '../../lib/utils'

export interface SegmentedOption<T extends string> {
  label: string
  value: T
}

interface SegmentedControlProps<T extends string> {
  ariaLabel: string
  className?: string
  disabled?: boolean
  onChange: (value: T) => void
  options: ReadonlyArray<SegmentedOption<T>>
  value: T
}

export function SegmentedControl<T extends string>({
  ariaLabel,
  className,
  disabled = false,
  onChange,
  options,
  value,
}: SegmentedControlProps<T>) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={cn(
        'inline-flex flex-wrap rounded-xl border border-border/70 bg-panel/80 p-1 shadow-[inset_0_1px_0_rgba(255,255,255,0.03)]',
        className,
      )}
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

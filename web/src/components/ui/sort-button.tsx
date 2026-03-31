import { cn } from '../../lib/utils'

interface SortButtonProps {
  /** Column header label (e.g., "Cost", "Sessions") */
  label: string
  /** Whether this column is currently the active sort key */
  active: boolean
  /** Callback when sort button is clicked */
  onClick: () => void
}

/**
 * Sortable column header button for ranking tables.
 * Used in Models, Tools, and Projects views for consistent sort UX.
 */
export function SortButton({ label, active, onClick }: SortButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-2 rounded-md px-1 py-1 text-left text-[11px] font-medium uppercase tracking-[0.16em] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/70',
        active ? 'text-foreground' : 'text-muted-foreground hover:text-foreground',
      )}
    >
      <span>{label}</span>
      <span
        aria-hidden="true"
        className={cn('text-[10px]', active ? 'text-accent' : 'text-muted-foreground/70')}
      >
        ↓
      </span>
    </button>
  )
}
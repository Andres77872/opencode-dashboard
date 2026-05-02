import { Button } from './button'
import { cn } from '../../lib/utils'
import type { SortDirection } from '../../lib/table-sort'

interface SortButtonProps {
  /** Column header label (e.g., "Cost", "Sessions") */
  label: string
  /** Whether this column is currently the active sort key */
  active: boolean
  /** Current sort direction when the column is active */
  direction?: SortDirection
  /** Callback when sort button is clicked */
  onClick: () => void
}

/**
 * Sortable column header button for ranking tables.
 * Used in Models, Tools, and Projects views for consistent sort UX.
 */
export function SortButton({ label, active, direction, onClick }: SortButtonProps) {
  const icon = active ? (direction === 'asc' ? '↑' : '↓') : '↕'
  const sortStateLabel = active ? (direction === 'asc' ? 'ascending' : 'descending') : 'not sorted'

  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      onClick={onClick}
      aria-label={`Sort by ${label}. Currently ${sortStateLabel}.`}
      title={`Sort by ${label}`}
      className={cn(
        'rounded-md px-1 py-1 text-left text-[11px] font-medium uppercase tracking-[0.16em]',
        active ? 'bg-background/40 text-foreground' : 'text-muted-foreground hover:text-foreground',
      )}
    >
      <span>{label}</span>
      <span
        aria-hidden="true"
        className={cn('text-[10px] leading-none', active ? 'text-accent' : 'text-muted-foreground/70')}
      >
        {icon}
      </span>
    </Button>
  )
}

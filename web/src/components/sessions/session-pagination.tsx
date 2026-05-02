import { Button } from '../ui/button'
import { formatInteger } from '../../lib/format'

interface SessionPaginationProps {
  page: number
  total: number
  pageSize: number
  totalPages: number
  firstVisible: number
  lastVisible: number
  onPageChange: (updater: number | ((prev: number) => number)) => void
}

export function SessionPagination({
  page,
  total,
  pageSize,
  totalPages,
  firstVisible,
  lastVisible,
  onPageChange,
}: SessionPaginationProps) {
  return (
    <div className="flex flex-col gap-3 rounded-2xl border border-border/70 bg-panel/50 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
      <div className="space-y-1 text-sm text-muted-foreground">
        <div className="font-medium text-foreground">
          Showing {firstVisible}-{lastVisible} of {formatInteger(total)} sessions
        </div>
        <div>
          Backend pagination is authoritative: <span className="font-mono text-foreground">page={page}</span>,{' '}
          <span className="font-mono text-foreground">page_size={pageSize}</span>,{' '}
          <span className="font-mono text-foreground">total={total}</span>
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button
          variant="ghost"
          disabled={page <= 1}
          onClick={() => onPageChange((current: number) => Math.max(1, current - 1))}
          className="min-w-24"
        >
          Previous
        </Button>
        <Button
          variant="ghost"
          disabled={page >= totalPages}
          onClick={() => onPageChange((current: number) => current + 1)}
          className="min-w-24"
        >
          Next
        </Button>
      </div>
    </div>
  )
}

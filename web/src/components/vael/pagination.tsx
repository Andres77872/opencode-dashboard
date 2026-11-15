/* Vael Pagination — the footer under a paged table. */
import { Button } from './controls'

export interface PaginationProps {
  /** 1-based current page. */
  page: number
  pageSize: number
  total: number
  /** Plural noun for the rows, e.g. "sessions". */
  noun: string
  onPageChange: (page: number) => void
  /** Blocks the buttons while a page is loading. */
  disabled?: boolean
}

/** Paged-table footer — "Showing 1–12 of 96 sessions" with previous/next buttons. @category Navigation */
export function Pagination({ page, pageSize, total, noun, onPageChange, disabled = false }: PaginationProps) {
  const pageCount = Math.max(1, Math.ceil(total / Math.max(1, pageSize)))
  const current = Math.min(Math.max(1, page), pageCount)
  const first = total === 0 ? 0 : (current - 1) * pageSize + 1
  const last = Math.min(total, current * pageSize)
  const strong = { color: 'var(--fg-primary)', fontWeight: 600 } as const

  return (
    <nav
      aria-label={`${noun} pagination`}
      style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, rowGap: 8, flexWrap: 'wrap', padding: '10px 14px', borderTop: '1px solid var(--border-subtle)' }}
    >
      <span aria-live="polite" style={{ font: '400 12px/1.3 var(--font-ui)', color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>
        Showing <span style={strong}>{first.toLocaleString('en-US')}–{last.toLocaleString('en-US')}</span> of{' '}
        <span style={strong}>{total.toLocaleString('en-US')}</span> {noun}
      </span>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        {pageCount > 1 && (
          <span style={{ font: '400 12px/1 var(--font-mono)', color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums', marginRight: 4 }}>
            Page {current} of {pageCount}
          </span>
        )}
        <Button size="sm" iconLeft="chevron-left" disabled={disabled || current <= 1} onClick={() => onPageChange(current - 1)}>
          Previous
        </Button>
        <Button size="sm" disabled={disabled || current >= pageCount} onClick={() => onPageChange(current + 1)}>
          Next
        </Button>
      </div>
    </nav>
  )
}

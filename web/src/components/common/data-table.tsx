import type { ReactNode } from 'react'
import { SortButton } from '../ui/sort-button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../ui/table'
import { Button } from '../ui/button'
import { getAriaSort } from '../../lib/table-sort'
import type { SortState, SortDirection } from '../../lib/table-sort'

// ── Column config ──────────────────────────────────────────────

export interface DataTableColumn<T> {
  key: string
  label: string
  /** Whether clicking this column header triggers sort. Default false. */
  sortable?: boolean
  /** Hide on mobile (<md). Default false. */
  desktopOnly?: boolean
  /** Column width class (e.g. "w-[8rem]"). */
  width?: string
  /** Render cell content for this column. */
  render: (row: T) => ReactNode
  /** Optional header override — receives the current sort state for this key. */
  renderHeader?: (sortState: SortState<string> | null) => ReactNode
}

// ── Pagination ─────────────────────────────────────────────────

export interface DataTablePagination {
  page: number
  total: number
  pageSize: number
  onPageChange: (page: number) => void
}

// ── Props ──────────────────────────────────────────────────────

export interface DataTableProps<T> {
  rows: T[]
  columns: DataTableColumn<T>[]
  sortState: SortState<string> | null
  onSortChange?: (key: string) => void
  rowKey: (row: T) => string
  /** Optional mobile card renderer. Falls back to a default column-value list. */
  mobileCard?: (row: T) => ReactNode
  /** Empty state content. Default: "No data" message. */
  emptyState?: ReactNode
  /** Pagination config. If omitted, no pagination controls shown. */
  pagination?: DataTablePagination
  /** Click handler for rows. */
  onRowClick?: (row: T) => void
}

// ── Component ──────────────────────────────────────────────────

export function DataTable<T>({
  rows,
  columns,
  sortState,
  onSortChange,
  rowKey,
  mobileCard,
  emptyState,
  pagination,
  onRowClick,
}: DataTableProps<T>) {
  const totalPages = pagination ? Math.max(1, Math.ceil(pagination.total / pagination.pageSize)) : 0

  if (rows.length === 0) {
    return (
      <div className="rounded-2xl border border-border/70 bg-card/40 p-6 text-sm text-muted-foreground">
        {emptyState ?? 'No data'}
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {/* ── Desktop table (md+) ── */}
      <div className="hidden md:block">
        <Table className="min-w-[42rem] overflow-hidden rounded-2xl border border-border/70 bg-card/40">
          <TableHeader className="bg-panel/75">
            <TableRow className="border-b border-border/70 hover:bg-transparent">
              {columns.map((col) => {
                if (col.desktopOnly && col.renderHeader) {
                  return (
                    <TableHead
                      key={col.key}
                      className={`${col.width ?? ''} text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground`}
                    >
                      {col.renderHeader(sortState)}
                    </TableHead>
                  )
                }

                return (
                  <TableHead
                    key={col.key}
                    className={`${col.width ?? ''} ${col.desktopOnly ? 'hidden lg:table-cell' : ''}`}
                    aria-sort={col.sortable && sortState ? getAriaSort(sortState, col.key) : undefined}
                  >
                    {col.sortable && onSortChange ? (
                      <SortButton
                        active={sortState?.key === col.key}
                        direction={sortState?.key === col.key ? sortState.direction : undefined}
                        label={col.label}
                        onClick={() => onSortChange(col.key)}
                      />
                    ) : col.renderHeader ? (
                      col.renderHeader(sortState)
                    ) : (
                      <span className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                        {col.label}
                      </span>
                    )}
                  </TableHead>
                )
              })}
            </TableRow>
          </TableHeader>
          <TableBody className="divide-y divide-border/60">
            {rows.map((row) => (
              <TableRow
                key={rowKey(row)}
                className={`bg-card/40 ${onRowClick ? 'cursor-pointer hover:bg-white/4' : 'hover:bg-transparent'}`}
                onClick={onRowClick ? () => onRowClick(row) : undefined}
              >
                {columns.map((col) => (
                  <TableCell
                    key={col.key}
                    className={`${col.desktopOnly ? 'hidden lg:table-cell' : ''} ${col.width ?? ''}`}
                  >
                    {col.render(row)}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {/* ── Mobile cards (<md) ── */}
      <div className="space-y-3 md:hidden">
        {rows.map((row) => {
          if (mobileCard) {
            return <div key={rowKey(row)}>{mobileCard(row)}</div>
          }

          // Default mobile rendering: column values as a stacked card
          return (
            <div
              key={rowKey(row)}
              className={`rounded-2xl border border-border/70 bg-panel/65 p-4 ${onRowClick ? 'cursor-pointer transition-colors hover:bg-panel/80' : ''}`}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              onKeyDown={onRowClick ? (e) => { if (e.key === 'Enter' || e.key === ' ') onRowClick(row) } : undefined}
              role={onRowClick ? 'button' : undefined}
              tabIndex={onRowClick ? 0 : undefined}
            >
              {columns.map((col) => (
                <div key={col.key} className="flex items-center justify-between gap-2 py-1">
                  {!col.desktopOnly && (
                    <>
                      <span className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{col.label}</span>
                      <span className="text-right text-sm">{col.render(row)}</span>
                    </>
                  )}
                </div>
              ))}
            </div>
          )
        })}
      </div>

      {/* ── Pagination ── */}
      {pagination && totalPages > 1 ? (
        <div className="flex flex-col gap-3 rounded-2xl border border-border/70 bg-panel/50 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="text-sm text-muted-foreground">
            Page {pagination.page} of {totalPages} · {pagination.total} total records
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="ghost"
              disabled={pagination.page <= 1}
              onClick={() => pagination.onPageChange(pagination.page - 1)}
              className="min-w-24"
            >
              Previous
            </Button>
            <Button
              variant="ghost"
              disabled={pagination.page >= totalPages}
              onClick={() => pagination.onPageChange(pagination.page + 1)}
              className="min-w-24"
            >
              Next
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  )
}

export type { SortDirection }

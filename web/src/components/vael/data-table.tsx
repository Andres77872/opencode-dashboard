/* Vael DataTable — generic, sortable, hover rows, sticky header.
   Ported from the Vael ui_kit and typed for arbitrary row shapes. */
import { useState } from 'react'
import type { ReactNode } from 'react'
import { Icon } from './icon'

export type SortDir = 'asc' | 'desc'

export interface SortSpec {
  key: string
  dir: SortDir
}

export interface Column<T> {
  key: string
  header: ReactNode
  numeric?: boolean
  align?: 'left' | 'right' | 'center'
  width?: number | string
  sortable?: boolean
  muted?: boolean
  wrap?: boolean
  render?: (row: T, index: number) => ReactNode
}

export interface DataTableProps<T> {
  columns: Column<T>[]
  rows: T[]
  sort?: SortSpec | null
  onSort?: (key: string) => void
  onRowClick?: (row: T, index: number) => void
  dense?: boolean
  rowKey?: (row: T, index: number) => string | number
}

export function DataTable<T>({ columns, rows, sort, onSort, onRowClick, dense, rowKey = (_r, i) => i }: DataTableProps<T>) {
  const [hover, setHover] = useState(-1)
  const rh = dense ? 38 : 44
  return (
    <div style={{ width: '100%', overflowX: 'auto', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', background: 'var(--ink-800)' }}>
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr style={{ background: 'var(--ink-750)' }}>
            {columns.map((c) => {
              const alignR = c.numeric || c.align === 'right'
              const active = !!sort && sort.key === c.key
              const sortable = c.sortable && !!onSort
              return (
                <th
                  key={c.key}
                  onClick={sortable ? () => onSort!(c.key) : undefined}
                  style={{
                    position: 'sticky',
                    top: 0,
                    textAlign: alignR ? 'right' : c.align || 'left',
                    padding: '0 14px',
                    height: 38,
                    width: c.width,
                    whiteSpace: 'nowrap',
                    color: active ? 'var(--fg-primary)' : 'var(--fg-muted)',
                    font: '600 11px/1 var(--font-ui)',
                    letterSpacing: '0.06em',
                    textTransform: 'uppercase',
                    background: 'var(--ink-750)',
                    borderBottom: '1px solid var(--border-default)',
                    cursor: sortable ? 'pointer' : 'default',
                    userSelect: 'none',
                  }}
                >
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, justifyContent: alignR ? 'flex-end' : 'flex-start', width: '100%' }}>
                    {c.header}
                    {active && <Icon name={sort!.dir === 'asc' ? 'arrow-up' : 'chevron-down'} size={12} color="var(--accent)" />}
                  </span>
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr
              key={rowKey(r, i)}
              onMouseEnter={() => setHover(i)}
              onMouseLeave={() => setHover(-1)}
              onClick={onRowClick ? () => onRowClick(r, i) : undefined}
              style={{ background: hover === i ? 'var(--ink-750)' : 'transparent', cursor: onRowClick ? 'pointer' : 'default', transition: 'background var(--dur-fast)' }}
            >
              {columns.map((c) => {
                const alignR = c.numeric || c.align === 'right'
                const fallback = (r as Record<string, unknown>)[c.key]
                return (
                  <td
                    key={c.key}
                    style={{
                      padding: '0 14px',
                      height: rh,
                      textAlign: alignR ? 'right' : c.align || 'left',
                      borderBottom: i < rows.length - 1 ? '1px solid var(--border-subtle)' : 'none',
                      color: c.muted ? 'var(--fg-muted)' : 'var(--fg-secondary)',
                      font: c.numeric ? '500 13px/1 var(--font-mono)' : '400 13px/1.3 var(--font-ui)',
                      fontVariantNumeric: c.numeric ? 'tabular-nums' : 'normal',
                      whiteSpace: c.wrap ? 'normal' : 'nowrap',
                      width: c.width,
                    }}
                  >
                    {c.render ? c.render(r, i) : (fallback as ReactNode)}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

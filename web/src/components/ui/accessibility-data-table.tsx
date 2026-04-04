import { useState } from 'react'
import { cn } from '../../lib/utils'

interface Column<T> {
  key: keyof T | string
  label: string
}

interface AccessibilityDataTableProps<T extends Record<string, unknown>> {
  columns: Column<T>[]
  data: T[]
  caption: string
  className?: string
}

export function AccessibilityDataTable<T extends Record<string, unknown>>({
  columns,
  data,
  caption,
  className,
}: AccessibilityDataTableProps<T>) {
  const [isExpanded, setIsExpanded] = useState(false)

  return (
    <div className={cn('mt-4', className)}>
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="text-sm text-muted-foreground hover:text-foreground transition-colors"
        aria-expanded={isExpanded}
      >
        {isExpanded ? 'Hide data table' : 'Show data table'}
      </button>

      {isExpanded && (
        <div role="region" aria-label={caption} className="mt-3 overflow-x-auto">
          <table className="w-full text-sm">
            <caption className="sr-only">{caption}</caption>
            <thead>
              <tr className="border-b border-border">
                {columns.map((column) => (
                  <th
                    key={String(column.key)}
                    className="px-3 py-2 text-left font-medium text-muted-foreground"
                    scope="col"
                  >
                    {column.label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {data.map((row, rowIndex) => (
                <tr key={rowIndex} className="border-b border-border/50">
                  {columns.map((column) => {
                    const value = row[column.key as keyof T]
                    const isNumeric = typeof value === 'number'
                    return (
                      <td
                        key={String(column.key)}
                        className={cn(
                          'px-3 py-2 text-muted-foreground',
                          isNumeric && 'tabular-nums text-right'
                        )}
                      >
                        {isNumeric
                          ? (value as number).toLocaleString()
                          : String(value ?? '')}
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
import type { ReactNode } from 'react'
import { cn } from '../../lib/utils'

/**
 * Single source of truth for the KPI grid layout, shared by every view and the
 * loading skeleton so loaded/loading states stay aligned. Denser than before:
 * 2-up from `sm`, 4-up from `xl`, tighter gap.
 */
export const KPI_GRID_CLASS = 'grid gap-3 sm:grid-cols-2 xl:grid-cols-4'

export function KpiGrid({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cn(KPI_GRID_CLASS, className)}>{children}</div>
}

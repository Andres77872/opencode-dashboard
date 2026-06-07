import type { ReactNode } from 'react'

interface PageHeaderProps {
  title: string
  description?: string
  /** Right-aligned slot for view-specific actions (search, export, etc.). */
  actions?: ReactNode
}

/**
 * Consistent page header used by every view in both loaded and loading states,
 * so the two never drift. The global time-range picker lives in the FilterBar,
 * not here — this is title + one-line description + optional per-view actions.
 */
export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="space-y-1">
        <h2 className="text-xl font-semibold tracking-tight text-foreground">{title}</h2>
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
    </div>
  )
}

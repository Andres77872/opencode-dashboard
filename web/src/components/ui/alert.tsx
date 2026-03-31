import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils'

type AlertTone = 'danger' | 'warning' | 'info'

interface AlertProps extends HTMLAttributes<HTMLDivElement> {
  tone?: AlertTone
}

const toneClasses: Record<AlertTone, string> = {
  danger: 'border-danger/40 bg-danger/10 text-danger',
  warning: 'border-warning/40 bg-warning/10 text-warning',
  info: 'border-info/40 bg-info/10 text-info',
}

export function Alert({ className, tone = 'info', ...props }: AlertProps) {
  return (
    <div
      role="alert"
      className={cn('rounded-xl border px-4 py-3 text-sm', toneClasses[tone], className)}
      {...props}
    />
  )
}

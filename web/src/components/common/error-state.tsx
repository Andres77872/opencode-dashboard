import { Alert } from '../ui/alert'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'

interface ErrorStateProps {
  title: string
  message?: string
  onRetry?: () => void
  className?: string
}

/** Shared danger alert + Retry button used across stats views. */
export function ErrorState({ title, message, onRetry, className }: ErrorStateProps) {
  return (
    <Alert
      tone="danger"
      className={cn('flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between', className)}
    >
      <div>
        <div className="font-medium text-foreground">{title}</div>
        {message ? <div className="text-sm opacity-90">{message}</div> : null}
      </div>
      {onRetry ? (
        <Button variant="ghost" onClick={onRetry}>
          Retry
        </Button>
      ) : null}
    </Alert>
  )
}

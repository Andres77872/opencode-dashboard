import { Button } from '../ui/button'
import { cn } from '../../lib/utils'

interface CopyButtonProps {
  copyId: string
  copiedId: string | null
  label: string
  value: string
  onCopy: (copyId: string, value: string) => void
  className?: string
}

export function CopyButton({ copyId, copiedId, label, value, onCopy, className }: CopyButtonProps) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      className={cn('text-xs text-muted-foreground hover:text-foreground', className)}
      onClick={() => onCopy(copyId, value)}
    >
      {copiedId === copyId ? 'Copied' : label}
    </Button>
  )
}

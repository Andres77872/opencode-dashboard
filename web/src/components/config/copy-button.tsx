import { Check, Copy } from 'lucide-react'
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
  const isCopied = copiedId === copyId

  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      className={cn(
        'gap-1 text-xs text-muted-foreground hover:text-foreground',
        isCopied ? 'text-success hover:text-success' : '',
        className,
      )}
      onClick={() => onCopy(copyId, value)}
    >
      {isCopied ? <Check className="size-3" /> : <Copy className="size-3" />}
      {isCopied ? 'Copied' : label}
    </Button>
  )
}

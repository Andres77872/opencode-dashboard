import type { ConfigJsonPrimitive } from '../../types/config'
import { formatDisplayLabel, formatPrimitiveValue, isRedactedValue } from '../../lib/config-utils'
import { cn } from '../../lib/utils'
import { CopyButton } from './copy-button'

interface PrimitiveFieldProps {
  label: string
  value: ConfigJsonPrimitive
  path: string
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

export function PrimitiveField({ label, value, path, copiedId, onCopy }: PrimitiveFieldProps) {
  const redacted = isRedactedValue(value)
  const displayLabel = formatDisplayLabel(label)
  const formattedValue = formatPrimitiveValue(value)

  return (
    <div className="group/row flex min-h-[32px] items-center gap-3 border-b border-border/20 px-1 py-1.5 last:border-b-0 hover:bg-muted/20">
      <span className="shrink-0 text-sm text-muted-foreground">{displayLabel}</span>

      <span
        className={cn(
          'min-w-0 flex-1 text-sm',
          redacted
            ? 'font-mono text-warning'
            : typeof value === 'string'
              ? 'break-all text-foreground'
              : 'font-mono text-accent',
        )}
      >
        {formattedValue}
      </span>

      <span className="shrink-0 opacity-0 transition-opacity group-hover/row:opacity-100">
        <CopyButton copyId={path} copiedId={copiedId} label="Copy" value={formattedValue} onCopy={onCopy} />
      </span>
    </div>
  )
}

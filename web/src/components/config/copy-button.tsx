/* Copy affordances for the config viewer. CopyButton is the labeled pill
   (header/section actions); InlineCopyButton is the icon-only hover-revealed
   variant used on tree rows. Copied-state feedback is owned by the view via
   copiedId/onCopy. */
import type { CSSProperties } from 'react'
import { Icon } from '../vael'

export interface CopyButtonProps {
  copyId: string
  copiedId: string | null
  label: string
  value: string
  onCopy: (copyId: string, value: string) => void
}

export function CopyButton({ copyId, copiedId, label, value, onCopy }: CopyButtonProps) {
  const copied = copiedId === copyId
  return (
    <button
      type="button"
      onClick={() => onCopy(copyId, value)}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        height: 28,
        padding: '0 10px',
        font: '600 12px/1 var(--font-ui)',
        color: copied ? 'var(--success)' : 'var(--fg-secondary)',
        background: 'transparent',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-md)',
        cursor: 'pointer',
        whiteSpace: 'nowrap',
        flexShrink: 0,
      }}
    >
      <Icon name={copied ? 'check' : 'copy'} size={14} />
      {copied ? 'Copied' : label}
    </button>
  )
}

export interface InlineCopyButtonProps {
  copyId: string
  copiedId: string | null
  value: string
  onCopy: (copyId: string, value: string) => void
  style?: CSSProperties
}

export function InlineCopyButton({ copyId, copiedId, value, onCopy, style }: InlineCopyButtonProps) {
  const copied = copiedId === copyId
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        onCopy(copyId, value)
      }}
      aria-label="Copy value"
      title="Copy value"
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: 24,
        height: 24,
        border: 'none',
        background: 'transparent',
        color: copied ? 'var(--success)' : 'var(--fg-faint)',
        cursor: 'pointer',
        borderRadius: 'var(--radius-sm)',
        flexShrink: 0,
        ...style,
      }}
    >
      <Icon name={copied ? 'check' : 'copy'} size={14} />
    </button>
  )
}

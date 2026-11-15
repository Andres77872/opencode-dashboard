/* Copy affordances for the config viewer, in the Vael look. CopyButton is the
   labeled action (header and card actions); InlineCopyButton is the icon-only
   variant on tree rows; CopyChip is a value that copies itself. All three get
   their copy/confirm/fail behavior from the shared useCopyFeedback, the same
   one the assistant panel's copy buttons use. */
import type { CSSProperties } from 'react'
import { Button, Icon } from '../vael'
import { useCopyFeedback } from '../../lib/clipboard'

export interface CopyButtonProps {
  label: string
  /** A thunk defers serializing a large value until the button is used. */
  value: string | (() => string)
  disabled?: boolean
  /** Shown as the tooltip when disabled, so the button explains itself. */
  disabledReason?: string
}

export function CopyButton({ label, value, disabled, disabledReason }: CopyButtonProps) {
  const { state, copy } = useCopyFeedback()
  const text = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : label
  return (
    <Button
      size="sm"
      variant="secondary"
      iconLeft={state === 'copied' ? 'check' : state === 'failed' ? 'alert-triangle' : 'copy'}
      disabled={disabled}
      title={disabled ? disabledReason : state === 'failed' ? 'The browser blocked clipboard access.' : undefined}
      onClick={() => copy(value)}
    >
      <span aria-live="polite">{text}</span>
    </Button>
  )
}

export interface InlineCopyButtonProps {
  /** Accessible name, e.g. "Copy providers as JSON". */
  label: string
  value: string | (() => string)
  /** False fades the button out; it stays focusable and shows again on focus. */
  visible: boolean
  style?: CSSProperties
}

export function InlineCopyButton({ label, value, visible, style }: InlineCopyButtonProps) {
  const { state, copy } = useCopyFeedback()
  const text = state === 'copied' ? 'Copied' : state === 'failed' ? 'Copy failed' : label
  return (
    <button
      type="button"
      onClick={() => copy(value)}
      aria-label={text}
      title={text}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: 26,
        height: 26,
        border: 'none',
        background: 'transparent',
        color: state === 'copied' ? 'var(--success)' : state === 'failed' ? 'var(--danger)' : 'var(--fg-muted)',
        cursor: 'pointer',
        borderRadius: 'var(--radius-sm)',
        flexShrink: 0,
        opacity: visible || state !== 'idle' ? 1 : 0,
        transition: 'opacity var(--dur-fast)',
        ...style,
      }}
    >
      <Icon name={state === 'copied' ? 'check' : state === 'failed' ? 'alert-triangle' : 'copy'} size={14} />
    </button>
  )
}

/** A short value rendered as a chip that copies itself when activated. */
export function CopyChip({ text, redacted }: { text: string; redacted: boolean }) {
  const { state, copy } = useCopyFeedback()
  return (
    <button
      type="button"
      onClick={() => copy(text)}
      title={state === 'failed' ? 'Copy failed' : state === 'copied' ? 'Copied' : 'Copy value'}
      aria-label={state === 'copied' ? `Copied ${text}` : state === 'failed' ? `Copy failed for ${text}` : `Copy ${text}`}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 4,
        maxWidth: '100%',
        padding: '2px 8px',
        font: '400 12px/1.4 var(--font-mono)',
        color: redacted ? 'var(--warning)' : 'var(--fg-secondary)',
        background: redacted ? 'var(--warning-soft)' : 'var(--ink-750)',
        border: `1px solid ${redacted ? 'var(--border-subtle)' : 'var(--border-default)'}`,
        borderRadius: 'var(--radius-sm)',
        cursor: 'pointer',
      }}
    >
      <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{text}</span>
      {state === 'copied' && <Icon name="check" size={12} color="var(--success)" />}
      {state === 'failed' && <Icon name="alert-triangle" size={12} color="var(--danger)" />}
    </button>
  )
}

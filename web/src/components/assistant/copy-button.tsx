import { useCopyFeedback } from '../../lib/clipboard'
import { Icon } from '../vael/icon'

/**
 * Copies a string and confirms it in place. Used for code blocks, individual
 * messages, and the whole transcript, so the value is always the raw Markdown
 * source the panel was given — never the rendered DOM.
 *
 * The copy/confirm/fail behavior is the shared `useCopyFeedback`; this file
 * only supplies the assistant panel's look.
 */
export function CopyButton({
  value,
  label = 'Copy',
  copiedLabel = 'Copied',
  iconSize = 12,
  className = '',
  showLabel = true,
}: {
  /** A thunk defers assembling large values until the button is actually used. */
  value: string | (() => string)
  label?: string
  copiedLabel?: string
  iconSize?: number
  className?: string
  showLabel?: boolean
}) {
  const { state, copy } = useCopyFeedback(1_400)
  const text = state === 'copied' ? copiedLabel : state === 'failed' ? 'Copy failed' : label
  return (
    <button
      type="button"
      className={`analytics-assistant-copy${state === 'failed' ? ' failed' : ''}${className ? ` ${className}` : ''}`}
      onClick={() => copy(value)}
      aria-label={text}
      title={text}
    >
      <Icon name={state === 'copied' ? 'check' : 'copy'} size={iconSize} />
      {showLabel && <span>{text}</span>}
    </button>
  )
}

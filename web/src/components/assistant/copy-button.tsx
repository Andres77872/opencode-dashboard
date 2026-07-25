import { useCallback, useEffect, useRef, useState } from 'react'
import { copyText } from '../../lib/clipboard'
import { Icon } from '../vael/icon'

const RESET_MS = 1_400

type CopyState = 'idle' | 'copied' | 'failed'

/**
 * Copies a string and confirms it in place. Used for code blocks, individual
 * messages, and the whole transcript, so the value is always the raw Markdown
 * source the panel was given — never the rendered DOM.
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
  const [state, setState] = useState<CopyState>('idle')
  const timerRef = useRef<number | undefined>(undefined)

  // A message can be cleared by "New chat" while its confirmation is still
  // pending, so the timer has to be cancelled rather than left to fire.
  useEffect(() => () => window.clearTimeout(timerRef.current), [])

  const onClick = useCallback(() => {
    void copyText(typeof value === 'function' ? value() : value).then((ok) => {
      setState(ok ? 'copied' : 'failed')
      window.clearTimeout(timerRef.current)
      timerRef.current = window.setTimeout(() => setState('idle'), RESET_MS)
    })
  }, [value])

  const text = state === 'copied' ? copiedLabel : state === 'failed' ? 'Copy failed' : label
  return (
    <button
      type="button"
      className={`analytics-assistant-copy${state === 'failed' ? ' failed' : ''}${className ? ` ${className}` : ''}`}
      onClick={onClick}
      aria-label={text}
      title={text}
    >
      <Icon name={state === 'copied' ? 'check' : 'copy'} size={iconSize} />
      {showLabel && <span>{text}</span>}
    </button>
  )
}

/* Vael Tooltip — minimal hover tooltip (replaces Radix Tooltip). */
import { useState } from 'react'
import type { CSSProperties, ReactNode } from 'react'

export type TooltipSide = 'top' | 'bottom'

export interface TooltipProps {
  content: ReactNode
  children: ReactNode
  side?: TooltipSide
}

export function Tooltip({ content, children, side = 'top' }: TooltipProps) {
  const [show, setShow] = useState(false)
  const pos: CSSProperties =
    side === 'bottom'
      ? { top: 'calc(100% + 6px)', left: '50%', transform: 'translateX(-50%)' }
      : { bottom: 'calc(100% + 6px)', left: '50%', transform: 'translateX(-50%)' }
  return (
    <span
      style={{ position: 'relative', display: 'inline-flex' }}
      onMouseEnter={() => setShow(true)}
      onMouseLeave={() => setShow(false)}
    >
      {children}
      {show && content != null && (
        <span
          role="tooltip"
          style={{
            position: 'absolute',
            ...pos,
            zIndex: 70,
            pointerEvents: 'none',
            background: 'var(--ink-700)',
            border: '1px solid var(--border-strong)',
            borderRadius: 'var(--radius-md)',
            boxShadow: 'var(--shadow-lg)',
            padding: '6px 9px',
            font: '500 12px/1.3 var(--font-mono)',
            color: 'var(--fg-primary)',
            whiteSpace: 'nowrap',
            maxWidth: 320,
          }}
        >
          {content}
        </span>
      )}
    </span>
  )
}

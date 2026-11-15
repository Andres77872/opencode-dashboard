/* Vael Drawer — a lightweight right-side slide-over (replaces Radix Sheet).
   Used for message / project drill-down detail. */
import { useEffect, useId, useRef } from 'react'
import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { IconButton } from './controls'
import { useEscapeKey, useFocusTrap } from './overlay'

export interface DrawerProps {
  open: boolean
  onClose: () => void
  title?: ReactNode
  subtitle?: ReactNode
  width?: number
  children: ReactNode
}

/** Right-side slide-over panel with scrim, header, and scrollable body; Escape or scrim click closes, and focus stays inside while open. @category Overlays */
export function Drawer({ open, onClose, title, subtitle, width = 560, children }: DrawerProps) {
  const panelRef = useRef<HTMLElement>(null)
  const titleId = useId()

  useEscapeKey(open, onClose)
  useFocusTrap(open, panelRef)

  useEffect(() => {
    if (!open) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [open])

  if (!open) return null

  return createPortal(
    <div data-vael-modal="" style={{ position: 'fixed', inset: 0, zIndex: 'var(--z-modal)' }}>
      <div
        aria-hidden="true"
        onClick={onClose}
        style={{ position: 'absolute', inset: 0, background: 'var(--scrim)', animation: 'vael-fade-in var(--dur-base) var(--ease-out)' }}
      />
      <aside
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={title ? titleId : undefined}
        tabIndex={-1}
        style={{
          position: 'absolute',
          top: 0,
          right: 0,
          height: '100%',
          width: `min(${width}px, 94vw)`,
          background: 'var(--ink-850)',
          borderLeft: '1px solid var(--border-default)',
          boxShadow: 'var(--shadow-xl)',
          display: 'flex',
          flexDirection: 'column',
          outline: 'none',
          animation: 'vael-slide-in-right var(--dur-base) var(--ease-out)',
        }}
      >
        <header
          style={{
            flexShrink: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 12,
            minHeight: 'var(--topbar-height)',
            padding: '8px 12px 8px 16px',
            borderBottom: '1px solid var(--border-default)',
          }}
        >
          <div style={{ minWidth: 0 }}>
            {title && <div id={titleId} style={{ font: '700 15px/1.2 var(--font-ui)', color: 'var(--fg-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{title}</div>}
            {subtitle && <div style={{ font: '400 12px/1.3 var(--font-mono)', color: 'var(--fg-muted)', marginTop: 2, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{subtitle}</div>}
          </div>
          <IconButton name="x" label="Close" onClick={onClose} />
        </header>
        <div style={{ flex: 1, overflowY: 'auto', padding: 16, minHeight: 0 }}>{children}</div>
      </aside>
    </div>,
    document.body,
  )
}

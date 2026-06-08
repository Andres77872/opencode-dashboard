/* Vael Drawer — a lightweight right-side slide-over (replaces Radix Sheet).
   Used for message / project drill-down detail. */
import { useEffect } from 'react'
import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Icon } from './icon'

export interface DrawerProps {
  open: boolean
  onClose: () => void
  title?: ReactNode
  subtitle?: ReactNode
  width?: number
  children: ReactNode
}

export function Drawer({ open, onClose, title, subtitle, width = 560, children }: DrawerProps) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [open, onClose])

  if (!open) return null

  return createPortal(
    <div style={{ position: 'fixed', inset: 0, zIndex: 80 }}>
      <div
        onClick={onClose}
        style={{ position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.55)', animation: 'vael-fade-rise var(--dur-base) var(--ease-out)' }}
      />
      <aside
        role="dialog"
        aria-modal="true"
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
            height: 'var(--topbar-height)',
            padding: '0 16px',
            borderBottom: '1px solid var(--border-default)',
          }}
        >
          <div style={{ minWidth: 0 }}>
            {title && <div style={{ font: '700 15px/1.2 var(--font-ui)', color: 'var(--fg-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{title}</div>}
            {subtitle && <div style={{ font: '400 12px/1.3 var(--font-mono)', color: 'var(--fg-muted)', marginTop: 2, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{subtitle}</div>}
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 32, height: 32, color: 'var(--fg-muted)', background: 'transparent', border: '1px solid transparent', borderRadius: 'var(--radius-md)', cursor: 'pointer', flexShrink: 0 }}
            onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--ink-750)'; e.currentTarget.style.color = 'var(--fg-primary)' }}
            onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; e.currentTarget.style.color = 'var(--fg-muted)' }}
          >
            <Icon name="x" size={17} />
          </button>
        </header>
        <div style={{ flex: 1, overflowY: 'auto', padding: 16, minHeight: 0 }}>{children}</div>
      </aside>
    </div>,
    document.body,
  )
}

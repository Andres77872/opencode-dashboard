/* Vael ConfirmDialog — an in-app replacement for window.confirm, so a
   destructive action is confirmed in the dashboard's own surface (and does not
   freeze the page the way a native modal does). */
import { useId, useRef } from 'react'
import type { ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Button } from './controls'
import { useEscapeKey, useFocusTrap } from './overlay'

export interface ConfirmDialogProps {
  open: boolean
  title: ReactNode
  children?: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  /** Styles the confirm button as destructive. */
  danger?: boolean
  busy?: boolean
  onConfirm: () => void
  onCancel: () => void
}

/** Centered modal asking the user to confirm or cancel an action; Escape and the scrim cancel. @category Overlays */
export function ConfirmDialog({ open, title, children, confirmLabel = 'Confirm', cancelLabel = 'Cancel', danger = false, busy = false, onConfirm, onCancel }: ConfirmDialogProps) {
  const panelRef = useRef<HTMLDivElement>(null)
  const titleId = useId()
  const bodyId = useId()

  useEscapeKey(open, onCancel)
  useFocusTrap(open, panelRef)

  if (!open) return null

  return createPortal(
    <div data-vael-modal="" style={{ position: 'fixed', inset: 0, zIndex: 'var(--z-dialog)', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: 16 }}>
      <div aria-hidden="true" onClick={busy ? undefined : onCancel} style={{ position: 'absolute', inset: 0, background: 'var(--scrim)', animation: 'vael-fade-in var(--dur-base) var(--ease-out)' }} />
      <div
        ref={panelRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={children ? bodyId : undefined}
        tabIndex={-1}
        style={{
          position: 'relative',
          width: 'min(440px, 100%)',
          background: 'var(--ink-750)',
          border: '1px solid var(--border-strong)',
          borderRadius: 'var(--radius-xl)',
          boxShadow: 'var(--shadow-xl)',
          padding: 20,
          outline: 'none',
          animation: 'vael-fade-rise var(--dur-base) var(--ease-out)',
        }}
      >
        <div id={titleId} style={{ font: '700 15px/1.3 var(--font-ui)', color: 'var(--fg-primary)' }}>{title}</div>
        {children && <div id={bodyId} style={{ marginTop: 8, font: '400 13px/1.55 var(--font-ui)', color: 'var(--fg-secondary)' }}>{children}</div>}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 20, flexWrap: 'wrap' }}>
          <Button variant="ghost" onClick={onCancel} disabled={busy}>{cancelLabel}</Button>
          <Button variant={danger ? 'danger' : 'primary'} onClick={onConfirm} busy={busy}>{confirmLabel}</Button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

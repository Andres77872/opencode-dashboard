/* Overlay plumbing shared by every Vael surface that floats above the page:
   one Escape-key stack and one focus trap. Kept apart from the components so
   those files export components only and stay valid Fast Refresh boundaries. */
import { useEffect, useRef } from 'react'
import type { RefObject } from 'react'

interface StackEntry {
  dismiss: () => void
}

const stack: StackEntry[] = []

function onDocumentKeyDown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || event.defaultPrevented) return
  const top = stack[stack.length - 1]
  if (!top) return
  // Consumed here so nothing else on the page reacts to the same press.
  event.preventDefault()
  top.dismiss()
}

/**
 * Close the topmost open overlay on Escape — and only that one.
 *
 * Every overlay used to attach its own document listener, so one press closed
 * a popover, the drawer behind it and the assistant panel all at once. Open
 * overlays now register on a shared stack and Escape reaches just the newest.
 */
export function useEscapeKey(active: boolean, dismiss: () => void) {
  const dismissRef = useRef(dismiss)
  useEffect(() => {
    dismissRef.current = dismiss
  })

  useEffect(() => {
    if (!active) return
    const entry: StackEntry = { dismiss: () => dismissRef.current() }
    if (stack.length === 0) document.addEventListener('keydown', onDocumentKeyDown)
    stack.push(entry)
    return () => {
      const index = stack.indexOf(entry)
      if (index >= 0) stack.splice(index, 1)
      if (stack.length === 0) document.removeEventListener('keydown', onDocumentKeyDown)
    }
  }, [active])
}

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

/**
 * Modal focus handling: move focus into the container when it opens, keep Tab
 * inside it, and return focus to whatever opened it on close. Without this,
 * tabbing from an open drawer walks into the page hidden behind its scrim.
 */
export function useFocusTrap(active: boolean, containerRef: RefObject<HTMLElement | null>) {
  useEffect(() => {
    if (!active) return
    const container = containerRef.current
    if (!container) return
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    if (!container.contains(document.activeElement)) container.focus()

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Tab') return
      const items = Array.from(container.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => el.offsetParent !== null)
      if (items.length === 0) {
        event.preventDefault()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const current = document.activeElement
      if (event.shiftKey && (current === first || current === container)) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && current === last) {
        event.preventDefault()
        first.focus()
      }
    }
    container.addEventListener('keydown', onKeyDown)
    return () => {
      container.removeEventListener('keydown', onKeyDown)
      opener?.focus?.()
    }
  }, [active, containerRef])
}

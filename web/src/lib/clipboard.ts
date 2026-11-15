import { useCallback, useEffect, useRef, useState } from 'react'

/**
 * Copies text to the clipboard, reporting whether it worked.
 *
 * The async Clipboard API is unavailable over plain HTTP and can be denied by
 * permission policy — both realistic for a dashboard served from a workstation
 * on the LAN. The legacy execCommand path covers those cases, and the boolean
 * result lets the UI show a real failure instead of a button that does nothing.
 */
export async function copyText(value: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
      return true
    }
  } catch {
    // Fall through to the legacy path below.
  }
  return legacyCopy(value)
}

function legacyCopy(value: string): boolean {
  if (typeof document === 'undefined') return false
  const area = document.createElement('textarea')
  area.value = value
  // Off-screen rather than hidden: execCommand ignores unrendered elements, and
  // readonly plus a fixed position keeps focus from scrolling the page.
  area.setAttribute('readonly', '')
  area.style.position = 'fixed'
  area.style.top = '-9999px'
  area.style.opacity = '0'
  document.body.appendChild(area)
  try {
    area.select()
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    area.remove()
  }
}

export type CopyState = 'idle' | 'copied' | 'failed'

/**
 * Copy-with-confirmation state for one button: `copy(value)` writes to the
 * clipboard and `state` reports "copied" or "failed" for a moment before
 * returning to idle. Every copy control in the app shares this, so a blocked
 * clipboard is always reported instead of looking like a dead button.
 *
 * A thunk defers assembling a large value until the button is actually used.
 */
export function useCopyFeedback(resetMs = 1_500) {
  const [state, setState] = useState<CopyState>('idle')
  const timerRef = useRef<number | undefined>(undefined)
  const mountedRef = useRef(true)

  // The owner can unmount while the confirmation is still showing (a cleared
  // chat, a collapsed tree row), so the timer is cancelled, not left to fire.
  useEffect(() => {
    mountedRef.current = true
    return () => {
      mountedRef.current = false
      window.clearTimeout(timerRef.current)
    }
  }, [])

  const copy = useCallback((value: string | (() => string)) => {
    void copyText(typeof value === 'function' ? value() : value).then((ok) => {
      if (!mountedRef.current) return
      setState(ok ? 'copied' : 'failed')
      window.clearTimeout(timerRef.current)
      timerRef.current = window.setTimeout(() => setState('idle'), resetMs)
    })
  }, [resetMs])

  return { state, copy }
}

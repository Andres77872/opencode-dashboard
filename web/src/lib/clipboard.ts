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

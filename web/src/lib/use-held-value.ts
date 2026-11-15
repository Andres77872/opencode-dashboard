import { useState } from 'react'

/**
 * Keeps the last value seen within `scope` on screen while the next one loads.
 *
 * A paged resource is keyed per page, so its data is briefly null on every
 * page turn or search keystroke. Rendering that gap would drop the table to a
 * skeleton and unmount the search box mid-typing; holding the previous page
 * avoids it. A new `scope` (another source or period) discards what was held,
 * because those rows no longer describe what the view is showing.
 */
export function useHeldValue<T>(scope: string, value: T | null): T | null {
  const [held, setHeld] = useState<{ scope: string; value: T | null }>({ scope, value })

  // Adjusting state while rendering (rather than in an effect) keeps the held
  // value in step with `value` without an extra committed frame.
  if (held.scope !== scope || (value !== null && value !== held.value)) {
    setHeld({ scope, value })
  }

  return value ?? (held.scope === scope ? held.value : null)
}

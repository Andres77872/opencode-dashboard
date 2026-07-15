import { useSyncExternalStore } from 'react'

/**
 * Subscribe to a CSS media query, e.g. useMediaQuery('(max-width: 880px)').
 * Returns false during SSR / when matchMedia is unavailable.
 */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (onStoreChange) => {
      if (typeof window.matchMedia !== 'function') {
        return () => {}
      }
      const mql = window.matchMedia(query)
      mql.addEventListener('change', onStoreChange)
      return () => mql.removeEventListener('change', onStoreChange)
    },
    () => (typeof window.matchMedia === 'function' ? window.matchMedia(query).matches : false),
    () => false,
  )
}

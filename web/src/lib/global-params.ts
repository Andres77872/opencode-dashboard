// Filters that follow the user between views. View-specific params (page,
// search text, an open drawer) are dropped on navigation.
const GLOBAL_PARAM_KEYS = ['source', 'period', 'from', 'to', 'mode'] as const

/**
 * The query string to carry to another view: the global filters from `search`,
 * with optional overrides (a null override removes that filter).
 */
export function globalSearch(search: string, overrides: Partial<Record<(typeof GLOBAL_PARAM_KEYS)[number], string | null>> = {}): string {
  const current = new URLSearchParams(search)
  const next = new URLSearchParams()
  for (const key of GLOBAL_PARAM_KEYS) {
    const override = overrides[key]
    const value = override === undefined ? current.get(key) : override
    if (value !== null) next.set(key, value)
  }
  const serialized = next.toString()
  return serialized ? `?${serialized}` : ''
}

import { useEffect, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context.ts'
import { withBypassCache } from './api.ts'
import { isResourceFresh, sharedResourceCache } from './resource-cache.ts'

export interface CachedResourceOptions<T> {
  /**
   * Identity of the data, namespaced by resource (e.g. "daily|3:codex::7d").
   * Changing it switches what is shown; the same key always means the same data.
   */
  key: string
  /** False while prerequisites (source metadata) are still loading: nothing is fetched yet. */
  ready: boolean
  /** Set when the resource cannot load at all: data is cleared and this becomes the error. */
  blocked?: string | null
  /** False defers the fetch (lazy resources). Whatever is cached is still returned. */
  enabled?: boolean
  /**
   * Keep results in the shared cache under `key`. Must be false for a result
   * that also depends on `depsKey` — it would be replayed for another page.
   */
  cache?: boolean
  /** Extra query dimensions (page, filter). A change refetches while the current rows stay on screen. */
  depsKey?: string
  load: (signal: AbortSignal) => Promise<T>
  fallbackError?: string
}

export interface CachedResourceResult<T> {
  data: T | null
  loading: boolean
  error: string | null
}

/**
 * Fetch + cache + refresh for one API resource.
 *
 * - Results live in an app-wide cache, so returning to a view shows its data
 *   immediately instead of a skeleton.
 * - Stale-while-revalidate: a refresh (the nonce in DashboardContext) or an
 *   aged entry refetches in the background and never blanks what is on screen.
 * - A change of `key` aborts the in-flight request and discards its result.
 * - Every fetch registers with the global "refreshing" indicator and always
 *   releases it, including when aborted.
 */
export function useCachedResource<T>(options: CachedResourceOptions<T>): CachedResourceResult<T> {
  const { key, ready, blocked = null, enabled = true, cache = true, depsKey = '', fallbackError = 'Failed to load data' } = options
  const context = useDashboardContext()
  const { refreshNonce } = context

  // Callers pass inline closures and the context object changes identity on
  // every refresh tick; neither should restart the fetch. Refs keep the effect
  // keyed on the query dimensions alone.
  const loadRef = useRef(options.load)
  const contextRef = useRef(context)
  useEffect(() => {
    loadRef.current = options.load
    contextRef.current = context
  })

  // The refresh generation each uncached variant (key + deps) was last fetched
  // under. Cached resources carry this on their cache entry instead.
  const fetchedUnderRef = useRef(new Map<string, number>())

  const [held, setHeld] = useState<{ key: string; data: T | null }>(() => ({
    key,
    data: cache ? (sharedResourceCache.peek<T>(key)?.value ?? null) : null,
  }))
  const [loading, setLoading] = useState(() => !(cache && isResourceFresh(sharedResourceCache.peek(key), refreshNonce)))
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!ready) {
      setLoading(true)
      setError(null)
      return
    }
    if (blocked) {
      setHeld({ key, data: null })
      setLoading(false)
      setError(blocked)
      return
    }

    const entry = cache ? sharedResourceCache.get<T>(key) : undefined
    // Blank the rows only when the scope changed and nothing is cached for the
    // new one: showing one source's rows under another's label would be wrong.
    // A `depsKey` change (a page turn, a filter keystroke) keeps the previous
    // rows on screen — nulling them would drop the view to its skeleton and
    // unmount the search input mid-typing.
    setHeld((previous) => {
      if (previous.key === key) return entry && previous.data !== entry.value ? { key, data: entry.value } : previous
      return { key, data: entry?.value ?? null }
    })

    if (!enabled || isResourceFresh(entry, refreshNonce)) {
      setLoading(false)
      setError(null)
      return
    }

    const controller = new AbortController()
    const release = contextRef.current.trackRefresh()
    setLoading(true)
    setError(null)

    // API responses are browser-cacheable for a short while. Once the user has
    // asked for a refresh, the first request for each variant under the new
    // generation must skip that cache or it would replay the old body; repeat
    // requests in the same generation (flipping back a page) may still use it.
    const variant = `${key}\n${depsKey}`
    const fetchedUnder = entry ? entry.nonce : fetchedUnderRef.current.get(variant)
    const refreshing = refreshNonce > 0 && fetchedUnder !== refreshNonce
    const start = () => loadRef.current(controller.signal)

    void (async () => {
      try {
        const next = await (refreshing ? withBypassCache(start) : start())
        if (controller.signal.aborted) return
        if (cache) {
          sharedResourceCache.set(key, next, refreshNonce)
        } else {
          const fetched = fetchedUnderRef.current
          if (fetched.size > 256) fetched.clear()
          fetched.set(variant, refreshNonce)
        }
        setHeld({ key, data: next })
        contextRef.current.setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted) return
        setError(caught instanceof Error ? caught.message : fallbackError)
      } finally {
        release()
        if (!controller.signal.aborted) setLoading(false)
      }
    })()

    return () => {
      controller.abort()
      release()
    }
  }, [key, ready, blocked, enabled, cache, depsKey, refreshNonce, fallbackError])

  // During the render in which `key` changes, `held` still describes the old
  // scope. Read the cache directly so a cached period shows without a
  // one-frame skeleton; otherwise there is nothing valid to show yet.
  const data = blocked
    ? null
    : held.key === key
      ? held.data
      : cache
        ? (sharedResourceCache.peek<T>(key)?.value ?? null)
        : null

  return { data, loading, error }
}

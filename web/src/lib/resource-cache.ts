/* A small bounded cache for API results, shared across view mounts.
 *
 * Each entry remembers the refresh generation ("nonce") it was fetched under
 * and when. That gives callers stale-while-revalidate for free: an entry from
 * an older generation, or one past its age budget, can still be shown at once
 * while a fresh copy loads, instead of dropping the view back to a skeleton.
 */

export interface ResourceCacheEntry<T> {
  value: T
  /** Refresh generation the value was fetched under. */
  nonce: number
  /** Epoch milliseconds when the value was stored. */
  storedAt: number
}

export interface ResourceCache {
  get<T>(key: string): ResourceCacheEntry<T> | undefined
  /** Read without marking the entry as used — safe to call while rendering. */
  peek<T>(key: string): ResourceCacheEntry<T> | undefined
  set<T>(key: string, value: T, nonce: number, now?: number): void
  delete(key: string): void
  clear(): void
  readonly size: number
}

/** Entries older than this are shown but revalidated in the background. */
export const RESOURCE_STALE_AFTER_MS = 45_000

/**
 * Whether an entry can be used without refetching: it must belong to the
 * current refresh generation and be within its age budget.
 */
export function isResourceFresh(entry: ResourceCacheEntry<unknown> | undefined, nonce: number, now = Date.now()): boolean {
  return entry !== undefined && entry.nonce === nonce && now - entry.storedAt < RESOURCE_STALE_AFTER_MS
}

/** Least-recently-used cache; reading or writing an entry marks it as used. */
export function createResourceCache(maxEntries = 96): ResourceCache {
  const entries = new Map<string, ResourceCacheEntry<unknown>>()
  return {
    get<T>(key: string) {
      const entry = entries.get(key) as ResourceCacheEntry<T> | undefined
      if (entry) {
        entries.delete(key)
        entries.set(key, entry)
      }
      return entry
    },
    peek<T>(key: string) {
      return entries.get(key) as ResourceCacheEntry<T> | undefined
    },
    set<T>(key: string, value: T, nonce: number, now = Date.now()) {
      entries.delete(key)
      entries.set(key, { value, nonce, storedAt: now })
      while (entries.size > maxEntries) {
        const oldest = entries.keys().next().value
        if (oldest === undefined) break
        entries.delete(oldest)
      }
    },
    delete(key: string) {
      entries.delete(key)
    },
    clear() {
      entries.clear()
    },
    get size() {
      return entries.size
    },
  }
}

/** The app-wide cache. Keys are namespaced by resource id, so views do not collide. */
export const sharedResourceCache = createResourceCache()

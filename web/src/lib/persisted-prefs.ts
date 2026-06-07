import { isDailyPeriod, isSourceID, isValidCustomRange, type SourceID } from '../types/api.ts'

/**
 * Best-effort persistence for the two global dashboard filters — the selected data
 * source and the time range. These are URL-driven first; localStorage is the seed
 * that restores the last choice on a cold load (and a safety net for cross-view
 * navigation when the URL is bare). Every access is guarded so a disabled or
 * throwing localStorage (private mode, sandboxed iframe, quota) is a no-op rather
 * than a crash, and so the module is inert under non-browser test runners.
 */

const SOURCE_KEY = 'ocd:source'
const PERIOD_KEY = 'ocd:period'

function getStorage(): Storage | null {
  try {
    if (typeof localStorage !== 'undefined') {
      return localStorage
    }
  } catch {
    // Accessing localStorage can throw when storage is blocked.
  }
  return null
}

function readRaw(key: string): string | null {
  const storage = getStorage()
  if (!storage) return null
  try {
    return storage.getItem(key)
  } catch {
    return null
  }
}

function writeRaw(key: string, value: string): void {
  const storage = getStorage()
  if (!storage) return
  try {
    storage.setItem(key, value)
  } catch {
    // Quota / private-mode write failures are non-fatal: persistence is best-effort.
  }
}

// ── Source ──────────────────────────────────────────────────────────

export function getStoredSourceId(): SourceID | null {
  const raw = readRaw(SOURCE_KEY)
  return isSourceID(raw) ? raw : null
}

export function setStoredSourceId(sourceId: SourceID): void {
  writeRaw(SOURCE_KEY, sourceId)
}

// ── Time range ──────────────────────────────────────────────────────

/** The persisted committed range: a preset key, or a custom from/to pair. */
export interface StoredPeriod {
  period?: string
  from?: string
  to?: string
}

export function getStoredPeriod(): StoredPeriod | null {
  const raw = readRaw(PERIOD_KEY)
  if (!raw) return null

  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) return null

  const candidate = parsed as Record<string, unknown>
  const from = typeof candidate.from === 'string' ? candidate.from : undefined
  const to = typeof candidate.to === 'string' ? candidate.to : undefined
  const period = typeof candidate.period === 'string' ? candidate.period : undefined

  // Validate before trusting persisted input — a stale or hand-edited value must
  // never select an unknown period or an inverted custom range.
  if (from && isValidCustomRange(from, to)) {
    return { from, to }
  }
  if (period && isDailyPeriod(period)) {
    return { period }
  }
  return null
}

export function setStoredPeriod(value: StoredPeriod): void {
  writeRaw(PERIOD_KEY, JSON.stringify(value))
}

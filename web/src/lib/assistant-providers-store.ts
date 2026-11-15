/*
  One shared copy of the assistant provider catalog.

  Both the Config section and the docked assistant panel show it, and both are
  on screen together on /config. Each used to fetch it on its own and then go
  stale the moment the other changed the active model. They now read the same
  snapshot: one request serves both, and a change made in either place is
  visible in the other without a round trip.
*/
import { useEffect, useSyncExternalStore } from 'react'
import { getAssistantProviders } from './api.ts'
import type { AssistantProvidersResponse, AssistantSelection } from '../types/assistant.ts'

/**
 * Fired on `window` after the active provider/model, or a provider it depends
 * on, changed. The catalog itself travels through the store; this tells
 * listeners that state derived from it elsewhere (the assistant's status and
 * consent text) needs another look.
 */
export const ASSISTANT_SELECTION_EVENT = 'opencode-dashboard:assistant-selection-changed'

export function notifyAssistantSelectionChanged() {
  if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent(ASSISTANT_SELECTION_EVENT))
}

export interface AssistantProvidersSnapshot {
  data: AssistantProvidersResponse | null
  loading: boolean
  error: string | null
}

// A second consumer mounting within this window reuses the answer the first
// one just got instead of asking again.
const FRESH_FOR_MS = 5_000

let snapshot: AssistantProvidersSnapshot = { data: null, loading: false, error: null }
let loadedAt = 0
let generation = 0
let inFlight: Promise<AssistantProvidersResponse> | null = null
const listeners = new Set<() => void>()

function publish(next: AssistantProvidersSnapshot) {
  snapshot = next
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function getSnapshot() {
  return snapshot
}

/**
 * Fetch the catalog now, superseding any request already in flight: after a
 * mutation the older response may predate it and must not win.
 */
export function reloadAssistantProviders(): Promise<AssistantProvidersResponse> {
  const id = ++generation
  publish({ ...snapshot, loading: true })
  const request = getAssistantProviders().then(
    (data) => {
      if (id === generation) {
        loadedAt = Date.now()
        publish({ data, loading: false, error: null })
      }
      return data
    },
    (caught: unknown) => {
      if (id === generation) {
        publish({
          ...snapshot,
          loading: false,
          error: caught instanceof Error ? caught.message : 'Assistant providers could not be loaded.',
        })
      }
      throw caught
    },
  )
  inFlight = request
  const settle = () => {
    if (inFlight === request) inFlight = null
  }
  request.then(settle, settle)
  return request
}

/** Fetch the catalog unless a request is already running or the copy is fresh. */
function ensureAssistantProviders(maxAgeMs = FRESH_FOR_MS): Promise<AssistantProvidersResponse> {
  if (inFlight) return inFlight
  if (snapshot.data && Date.now() - loadedAt < maxAgeMs) return Promise.resolve(snapshot.data)
  return reloadAssistantProviders()
}

/** Record a selection the server just confirmed, without re-fetching the catalog. */
export function applyAssistantSelection(selection: AssistantSelection) {
  if (!snapshot.data) return
  publish({ ...snapshot, data: { ...snapshot.data, selection } })
}

/** Test seam: drop the shared snapshot so each test starts from an empty store. */
export function resetAssistantProvidersStore() {
  generation += 1
  inFlight = null
  loadedAt = 0
  publish({ data: null, loading: false, error: null })
}

/**
 * The shared provider catalog. Mounting a consumer loads it when nothing fresh
 * is held; `loading` is true until the first answer or error arrives.
 */
export function useAssistantProviders(): AssistantProvidersSnapshot {
  const value = useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
  useEffect(() => {
    void ensureAssistantProviders().catch(() => undefined)
  }, [])
  // Before the mount effect has run nothing is in flight yet; reporting that
  // as "loaded and empty" would flash an empty state for one frame.
  if (value.data === null && value.error === null && !value.loading) return { ...value, loading: true }
  return value
}

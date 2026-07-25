/* Shared provider-quota store: one 60s poll feeds every subscribed component
   (the sidebar section is always mounted, so the Overview section reuses the
   same fetch instead of adding a second poller). */
import { useEffect, useSyncExternalStore } from 'react'
import { getQuotas, withBypassCache } from './api'
import type { QuotasResponse } from '../types/api'
import { useDashboardContext } from '../components/layout/dashboard-context'

const POLL_INTERVAL_MS = 60_000

export interface QuotasState {
  data: QuotasResponse | null
  error: string | null
  loading: boolean
}

let state: QuotasState = { data: null, error: null, loading: false }
const listeners = new Set<() => void>()
let timer: number | null = null
let inFlight: AbortController | null = null
let lastHandledNonce = 0

function setState(next: Partial<QuotasState>) {
  state = { ...state, ...next }
  listeners.forEach((listener) => listener())
}

async function refresh(bypassHttpCache = false) {
  inFlight?.abort()
  const controller = new AbortController()
  inFlight = controller
  setState({ loading: true })
  try {
    const data = bypassHttpCache
      ? await withBypassCache(() => getQuotas(controller.signal))
      : await getQuotas(controller.signal)
    if (controller.signal.aborted) return
    setState({ data, error: null, loading: false })
  } catch (error) {
    if (controller.signal.aborted) return
    setState({ error: error instanceof Error ? error.message : 'Failed to load quotas', loading: false })
  }
}

function pollTick() {
  // Skip background-tab polls; the next visible tick catches up.
  if (typeof document !== 'undefined' && document.hidden) return
  void refresh()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  if (listeners.size === 1) {
    void refresh()
    timer = window.setInterval(pollTick, POLL_INTERVAL_MS)
  }
  return () => {
    listeners.delete(listener)
    if (listeners.size === 0 && timer !== null) {
      window.clearInterval(timer)
      timer = null
      inFlight?.abort()
      inFlight = null
    }
  }
}

function getSnapshot(): QuotasState {
  return state
}

/** Re-fetch quotas now — backs the retry action on the quota error states. */
export function refreshQuotas() {
  void refresh(true)
}

export function useQuotas(): QuotasState {
  const { refreshNonce } = useDashboardContext()
  const snapshot = useSyncExternalStore(subscribe, getSnapshot)

  // The global refresh button also refreshes quotas; the nonce guard keeps
  // multiple mounted subscribers from firing duplicate fetches.
  useEffect(() => {
    if (refreshNonce > 0 && refreshNonce !== lastHandledNonce) {
      lastHandledNonce = refreshNonce
      void refresh(true)
    }
  }, [refreshNonce])

  return snapshot
}

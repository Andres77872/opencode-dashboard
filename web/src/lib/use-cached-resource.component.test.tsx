import { act, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DashboardProvider } from '../components/layout/dashboard-provider'
import type { DashboardContextValue } from '../components/layout/dashboard-context'
import { sharedResourceCache } from './resource-cache'
import { useCachedResource, type CachedResourceOptions } from './use-cached-resource'

function context(overrides: Partial<DashboardContextValue> = {}): DashboardContextValue {
  return {
    sources: [], sourceMetadata: null, sourceMetadataLoading: false, sourceMetadataError: null,
    selectedSourceId: 'opencode', selectedSourceInfo: null, sourceAvailable: true, sourceStateError: null,
    setSelectedSourceId: vi.fn(), lastUpdatedAt: null, isRefreshing: false, refreshNonce: 0,
    requestRefresh: vi.fn(), trackRefresh: () => () => {}, setLastUpdatedAt: vi.fn(),
    ...overrides,
  }
}

/** A loader whose requests are settled by hand, recording each call's signal. */
function manualLoader() {
  const calls: { resolve: (value: string) => void; reject: (error: Error) => void; signal: AbortSignal }[] = []
  const load = (signal: AbortSignal) => new Promise<string>((resolve, reject) => calls.push({ resolve, reject, signal }))
  return { calls, load }
}

// `key` is reserved on JSX props, so the test view takes it as `resourceKey`.
function View({ resourceKey, ...options }: Omit<CachedResourceOptions<string>, 'ready' | 'key'> & { resourceKey: string; ready?: boolean }) {
  const { data, loading, error } = useCachedResource<string>({ ready: true, key: resourceKey, ...options })
  return <div data-testid="view">{`${data ?? 'none'}|${loading ? 'loading' : 'idle'}|${error ?? 'ok'}`}</div>
}

const view = () => screen.getByTestId('view').textContent

describe('useCachedResource', () => {
  beforeEach(() => sharedResourceCache.clear())

  it('shows cached data at once on a later mount and does not refetch while it is fresh', async () => {
    const first = manualLoader()
    const mounted = render(<DashboardProvider value={context()}><View resourceKey="k" load={first.load} /></DashboardProvider>)
    expect(view()).toBe('none|loading|ok')
    await act(async () => first.calls[0].resolve('rows'))
    expect(view()).toBe('rows|idle|ok')
    mounted.unmount()

    const second = manualLoader()
    render(<DashboardProvider value={context()}><View resourceKey="k" load={second.load} /></DashboardProvider>)
    expect(view()).toBe('rows|idle|ok')
    expect(second.calls).toHaveLength(0)
  })

  it('refreshes in the background without blanking what is on screen', async () => {
    const loader = manualLoader()
    const { rerender } = render(<DashboardProvider value={context()}><View resourceKey="k" load={loader.load} /></DashboardProvider>)
    await act(async () => loader.calls[0].resolve('old rows'))

    rerender(<DashboardProvider value={context({ refreshNonce: 1 })}><View resourceKey="k" load={loader.load} /></DashboardProvider>)
    await waitFor(() => expect(loader.calls).toHaveLength(2))
    expect(view()).toBe('old rows|loading|ok')

    await act(async () => loader.calls[1].resolve('new rows'))
    expect(view()).toBe('new rows|idle|ok')
  })

  it('keeps the last data and reports the error when a refresh fails', async () => {
    const loader = manualLoader()
    const { rerender } = render(<DashboardProvider value={context()}><View resourceKey="k" load={loader.load} /></DashboardProvider>)
    await act(async () => loader.calls[0].resolve('rows'))
    rerender(<DashboardProvider value={context({ refreshNonce: 1 })}><View resourceKey="k" load={loader.load} /></DashboardProvider>)
    await waitFor(() => expect(loader.calls).toHaveLength(2))
    await act(async () => loader.calls[1].reject(new Error('backend down')))
    expect(view()).toBe('rows|idle|backend down')
  })

  it('drops the previous scope when the key changes and discards its late answer', async () => {
    const loader = manualLoader()
    const { rerender } = render(<DashboardProvider value={context()}><View resourceKey="a" load={loader.load} /></DashboardProvider>)
    await waitFor(() => expect(loader.calls).toHaveLength(1))

    rerender(<DashboardProvider value={context()}><View resourceKey="b" load={loader.load} /></DashboardProvider>)
    await waitFor(() => expect(loader.calls).toHaveLength(2))
    expect(loader.calls[0].signal.aborted).toBe(true)
    expect(view()).toBe('none|loading|ok')

    await act(async () => loader.calls[0].resolve('answer for a'))
    expect(view()).toBe('none|loading|ok')
    await act(async () => loader.calls[1].resolve('answer for b'))
    expect(view()).toBe('answer for b|idle|ok')
  })

  it('keeps the current rows while a deps change (a page turn) loads', async () => {
    const loader = manualLoader()
    const { rerender } = render(<DashboardProvider value={context()}><View resourceKey="k" cache={false} depsKey="page-1" load={loader.load} /></DashboardProvider>)
    await act(async () => loader.calls[0].resolve('page 1'))

    rerender(<DashboardProvider value={context()}><View resourceKey="k" cache={false} depsKey="page-2" load={loader.load} /></DashboardProvider>)
    await waitFor(() => expect(loader.calls).toHaveLength(2))
    expect(view()).toBe('page 1|loading|ok')
    await act(async () => loader.calls[1].resolve('page 2'))
    expect(view()).toBe('page 2|idle|ok')
  })

  it('waits for prerequisites, and reports a blocked resource without fetching', async () => {
    const loader = manualLoader()
    const { rerender } = render(<DashboardProvider value={context()}><View resourceKey="k" ready={false} load={loader.load} /></DashboardProvider>)
    expect(view()).toBe('none|loading|ok')
    expect(loader.calls).toHaveLength(0)

    rerender(<DashboardProvider value={context()}><View resourceKey="k" blocked="Source is unavailable" load={loader.load} /></DashboardProvider>)
    expect(view()).toBe('none|idle|Source is unavailable')
    expect(loader.calls).toHaveLength(0)
  })

  it('always releases the refreshing indicator, including for an aborted request', async () => {
    let inFlight = 0
    const trackRefresh = () => {
      inFlight += 1
      let released = false
      return () => {
        if (released) return
        released = true
        inFlight -= 1
      }
    }
    const loader = manualLoader()
    const { rerender, unmount } = render(<DashboardProvider value={context({ trackRefresh })}><View resourceKey="a" load={loader.load} /></DashboardProvider>)
    await waitFor(() => expect(inFlight).toBe(1))

    rerender(<DashboardProvider value={context({ trackRefresh })}><View resourceKey="b" load={loader.load} /></DashboardProvider>)
    await waitFor(() => expect(loader.calls).toHaveLength(2))
    expect(inFlight).toBe(1)

    await act(async () => loader.calls[1].resolve('rows'))
    expect(inFlight).toBe(0)
    unmount()
    expect(inFlight).toBe(0)
  })
})

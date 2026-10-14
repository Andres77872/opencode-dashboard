import { act, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DashboardProvider } from '../components/layout/dashboard-provider'
import type { DashboardContextValue } from '../components/layout/dashboard-context'
import { usePeriodResource } from './use-period-resource'
import { useOverviewAll } from './use-overview-all'

function context(revision = 0, enabled = true): DashboardContextValue {
  const source = { id: 'antigravity' as const, label: 'Antigravity', enabled, available: true, kind: 'sqlite', default: false, read_only: true, local_only: true, capabilities: [] }
  return {
    sources: [source], sourceMetadata: { sources: [source], default_source_id: 'opencode', sources_revision: revision },
    sourceMetadataLoading: false, sourceMetadataError: null, selectedSourceId: 'antigravity', selectedSourceInfo: source,
    sourceAvailable: enabled, sourceStateError: enabled ? null : { kind: 'disabled', message: 'Disabled' },
    setSelectedSourceId: vi.fn(), lastUpdatedAt: null, isRefreshing: false, refreshNonce: 0,
    requestRefresh: vi.fn(), setRefreshing: vi.fn(), setLastUpdatedAt: vi.fn(),
  }
}

describe('source revisions', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('aborts obsolete period requests and discards their late results', async () => {
    const pending: { resolve: (value: string) => void; signal?: AbortSignal }[] = []
    const fetcher = (_period: string, signal?: AbortSignal) => new Promise<string>(resolve => pending.push({ resolve, signal }))
    function View() {
      const { data } = usePeriodResource(fetcher, 'all')
      return <div>{data ?? 'Empty'}</div>
    }
    const { rerender } = render(<DashboardProvider value={context()}><View /></DashboardProvider>)
    await waitFor(() => expect(pending).toHaveLength(1))
    rerender(<DashboardProvider value={context(1)}><View /></DashboardProvider>)
    await waitFor(() => expect(pending).toHaveLength(2))
    expect(pending[0].signal?.aborted).toBe(true)
    await act(async () => pending[1].resolve('Current data'))
    expect(screen.getByText('Current data')).toBeTruthy()
    await act(async () => pending[0].resolve('Stale data'))
    expect(screen.queryByText('Stale data')).toBeNull()
    rerender(<DashboardProvider value={context(2, false)}><View /></DashboardProvider>)
    expect(screen.getByText('Empty')).toBeTruthy()
    expect(pending).toHaveLength(2)
  })

  it('clears combined totals and makes no analytics request with every source off', async () => {
    const fetch = vi.fn(async () => new Response(JSON.stringify({ total: { requests: 7 } }), { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    function View() {
      const { data } = useOverviewAll('all')
      return <div>{data ? `${data.total.requests} requests` : 'Empty'}</div>
    }
    const { rerender } = render(<DashboardProvider value={context()}><View /></DashboardProvider>)
    await screen.findByText('7 requests')
    rerender(<DashboardProvider value={context(1, false)}><View /></DashboardProvider>)
    expect(screen.getByText('Empty')).toBeTruthy()
    expect(fetch).toHaveBeenCalledTimes(1)
  })
})

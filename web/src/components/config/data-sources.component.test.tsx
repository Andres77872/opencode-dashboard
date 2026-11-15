import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DataSources } from './data-sources'
import { DashboardProvider } from '../layout/dashboard-provider'
import type { DashboardContextValue } from '../layout/dashboard-context'
import type { SourceInfo, SourceListResponse } from '../../types/api'

const sources: SourceInfo[] = ['codex', 'antigravity'].map((id) => ({
  id: id as SourceInfo['id'], label: id === 'codex' ? 'Codex' : 'Antigravity', kind: 'sqlite', enabled: true,
  available: true, default: false, read_only: true, local_only: true, capabilities: [], path: `/data/${id}`,
}))

function setup() {
  const update = vi.fn()
  const select = vi.fn()
  const value: DashboardContextValue = {
    sources, sourceMetadata: { sources, default_source_id: 'opencode', sources_revision: 0 },
    sourceMetadataLoading: false, sourceMetadataError: null, selectedSourceId: 'antigravity', selectedSourceInfo: sources[1],
    sourceAvailable: true, sourceStateError: null, setSelectedSourceId: select, updateSourceMetadata: update,
    lastUpdatedAt: null, isRefreshing: false, refreshNonce: 0, requestRefresh: vi.fn(), trackRefresh: () => () => {}, setLastUpdatedAt: vi.fn(),
  }
  render(<DashboardProvider value={value}><DataSources /></DashboardProvider>)
  return { update, select }
}

describe('DataSources', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('saves the switch and selects an enabled fallback', async () => {
    const next: SourceListResponse = { sources: [sources[0], { ...sources[1], enabled: false }], default_source_id: 'opencode', sources_revision: 1 }
    const fetch = vi.fn(async (input: RequestInfo | URL) => {
      expect(String(input)).toBe('/api/v1/sources/antigravity')
      return new Response(JSON.stringify(next), { status: 200 })
    })
    vi.stubGlobal('fetch', fetch)
    const { update, select } = setup()
    fireEvent.click(screen.getByRole('switch', { name: 'Enable Antigravity' }))
    await waitFor(() => expect(update).toHaveBeenCalledWith(next))
    expect(select).toHaveBeenCalledWith('codex')
    expect(fetch.mock.calls[0]?.[0]).toBe('/api/v1/sources/antigravity')
  })

  it('marks only the row being saved as busy and ignores a second click on it', async () => {
    let release: (response: Response) => void = () => {}
    const fetch = vi.fn(() => new Promise<Response>((resolve) => { release = resolve }))
    vi.stubGlobal('fetch', fetch)
    const { update } = setup()
    const antigravity = screen.getByRole('switch', { name: 'Enable Antigravity' })
    fireEvent.click(antigravity)
    await waitFor(() => expect(antigravity.getAttribute('aria-busy')).toBe('true'))
    fireEvent.click(antigravity)
    expect(fetch).toHaveBeenCalledTimes(1)
    const codex = screen.getByRole('switch', { name: 'Enable Codex' })
    expect(codex.getAttribute('aria-busy')).toBeNull()
    expect((codex as HTMLButtonElement).disabled).toBe(false)

    const next: SourceListResponse = { sources: [sources[0], { ...sources[1], enabled: false }], default_source_id: 'opencode', sources_revision: 1 }
    release(new Response(JSON.stringify(next), { status: 200 }))
    await waitFor(() => expect(update).toHaveBeenCalledWith(next))
    await waitFor(() => expect(antigravity.getAttribute('aria-busy')).toBeNull())
  })

  it('keeps the enabled value when saving fails', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ message: 'Could not save' }), { status: 500 })))
    const { update, select } = setup()
    fireEvent.click(screen.getByRole('switch', { name: 'Enable Antigravity' }))
    await screen.findByRole('alert')
    expect(screen.getByRole('switch', { name: 'Enable Antigravity' }).getAttribute('aria-checked')).toBe('true')
    expect(update).not.toHaveBeenCalled()
    expect(select).not.toHaveBeenCalled()
  })
})

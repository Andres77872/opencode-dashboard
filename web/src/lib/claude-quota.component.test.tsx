import { render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DashboardProvider } from '../components/layout/dashboard-provider'
import type { DashboardContextValue } from '../components/layout/dashboard-context'
import { QuotaSection } from '../components/layout/quota-section'

function context(openCodeEnabled: boolean): DashboardContextValue {
  const sources = (['opencode', 'claude_code'] as const).map(id => ({
    id, label: id, enabled: id === 'opencode' && openCodeEnabled, available: true,
    kind: 'sqlite', default: id === 'opencode', read_only: true, local_only: true, capabilities: [],
  }))
  return {
    sources, sourceMetadata: { sources, default_source_id: 'opencode' },
    sourceMetadataLoading: false, sourceMetadataError: null, selectedSourceId: 'opencode', selectedSourceInfo: sources[0],
    sourceAvailable: openCodeEnabled, sourceStateError: null,
    setSelectedSourceId: vi.fn(), lastUpdatedAt: null, isRefreshing: false, refreshNonce: 0,
    requestRefresh: vi.fn(), setRefreshing: vi.fn(), setLastUpdatedAt: vi.fn(),
  }
}

afterEach(() => vi.unstubAllGlobals())

it('renders one Claude Agent monitor with Claude Code analytics off and follows the OpenCode toggle', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
    fetched_at_ms: Date.now(), providers: [{
      provider: 'claude_code', label: 'Claude Agent (Claude Code)', source_id: 'opencode', source_provider_id: 'claude-agent',
      status: 'ok', windows: [{ id: 'weekly', used_percent: 7 }],
    }],
  }), { status: 200 })))
  const { rerender } = render(<DashboardProvider value={context(true)}><QuotaSection /></DashboardProvider>)
  await screen.findByText('Claude Agent (Claude Code)')
  expect(screen.getAllByText('Claude Agent (Claude Code)')).toHaveLength(1)
  expect(screen.getByText('7%')).toBeTruthy()
  rerender(<DashboardProvider value={context(false)}><QuotaSection /></DashboardProvider>)
  expect(screen.queryByText('Claude Agent (Claude Code)')).toBeNull()
})

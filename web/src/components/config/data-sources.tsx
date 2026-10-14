import { useState } from 'react'
import { Card } from '../vael'
import { useDashboardContext } from '../layout/dashboard-context'
import { setSourceEnabled } from '../../lib/api'
import type { SourceID } from '../../types/api'

export function DataSources() {
  const { sources, selectedSourceId, setSelectedSourceId, updateSourceMetadata } = useDashboardContext()
  const [pending, setPending] = useState<SourceID | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function toggle(id: SourceID, enabled: boolean) {
    setPending(id)
    setError(null)
    try {
      const next = await setSourceEnabled(id, enabled)
      if (!enabled && id === selectedSourceId) {
        const fallback = next.sources.find(source => source.enabled !== false && source.available)
        if (fallback) setSelectedSourceId(fallback.id)
      }
      updateSourceMetadata?.(next)
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Could not save source preference')
    } finally {
      setPending(null)
    }
  }

  return (
    <Card title="Data sources" subtitle="Choose which sources contribute to the dashboard. Disabling pauses sync and linked quotas; cached history is retained.">
      {error && <p role="alert" style={{ color: 'var(--danger)' }}>{error}</p>}
      {sources.map(source => {
        const enabled = source.enabled !== false
        return (
          <div key={source.id} style={{ display: 'flex', alignItems: 'center', gap: 16, padding: '14px 0', borderBottom: '1px solid var(--border-subtle)' }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontWeight: 600 }}>{source.label} <span style={{ color: 'var(--fg-muted)', fontWeight: 400 }}>{!enabled && 'Disabled · '}{source.diagnostics?.status === 'unchecked' ? 'Availability unchecked' : source.available ? 'Available' : 'Unavailable'}</span></div>
              {source.path && <div style={{ color: 'var(--fg-muted)', overflowWrap: 'anywhere', fontSize: 12, marginTop: 4 }}>{source.path}</div>}
              {source.diagnostics?.reason && <div style={{ color: 'var(--fg-muted)', fontSize: 12, marginTop: 4 }}>{source.diagnostics.reason}</div>}
            </div>
            <button type="button" role="switch" aria-label={`Enable ${source.label}`} aria-checked={enabled} disabled={pending !== null}
              onClick={() => void toggle(source.id, !enabled)}
              style={{ minWidth: 72, minHeight: 36, borderRadius: 'var(--radius-md)', border: '1px solid var(--border-default)', background: enabled ? 'var(--accent-soft)' : 'var(--ink-800)', color: enabled ? 'var(--accent)' : 'var(--fg-muted)', cursor: pending ? 'wait' : 'pointer' }}>
              {pending === source.id ? 'Saving…' : enabled ? 'On' : 'Off'}
            </button>
          </div>
        )
      })}
    </Card>
  )
}

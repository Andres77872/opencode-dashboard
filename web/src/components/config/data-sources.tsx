/* Data sources — which local tools feed the dashboard. Each row is a switch
   that applies immediately; the server's answer is the new source list, so
   nothing else is re-fetched here. */
import { useState } from 'react'
import { Badge, Card, EmptyState, Notice, Switch, VendorChip } from '../vael'
import type { BadgeTone } from '../vael'
import { useDashboardContext } from '../layout/dashboard-context'
import { setSourceEnabled } from '../../lib/api'
import { asSentence } from './text'
import type { SourceID, SourceInfo } from '../../types/api'

function availability(source: SourceInfo, enabled: boolean): { tone: BadgeTone; label: string } {
  if (!enabled) return { tone: 'neutral', label: 'Off' }
  if (source.diagnostics?.status === 'unchecked') return { tone: 'neutral', label: 'Not checked yet' }
  return source.available ? { tone: 'success', label: 'Available' } : { tone: 'warning', label: 'Unavailable' }
}

export function DataSources() {
  const { sources, sourceMetadataLoading, selectedSourceId, setSelectedSourceId, updateSourceMetadata } = useDashboardContext()
  const [pending, setPending] = useState<ReadonlySet<SourceID>>(() => new Set())
  const [error, setError] = useState<{ label: string; message: string } | null>(null)

  async function toggle(source: SourceInfo, enabled: boolean) {
    setPending((current) => new Set([...current, source.id]))
    setError(null)
    try {
      const next = await setSourceEnabled(source.id, enabled)
      if (!enabled && source.id === selectedSourceId) {
        const fallback = next.sources.find((candidate) => candidate.enabled !== false && candidate.available)
        if (fallback) setSelectedSourceId(fallback.id)
      }
      updateSourceMetadata?.(next)
    } catch (caught) {
      setError({
        label: source.label,
        message: caught instanceof Error ? caught.message : 'The source preference could not be saved.',
      })
    } finally {
      setPending((current) => {
        const next = new Set(current)
        next.delete(source.id)
        return next
      })
    }
  }

  return (
    <Card
      title="Data sources"
      subtitle="Choose which tools feed the dashboard. Turning a source off hides it from every view and pauses its sync and linked quota checks. Its cached history is kept and comes back when you turn it on again."
      flush
    >
      {error && (
        <div style={{ padding: '12px 16px 0' }}>
          <Notice tone="danger" title={`${error.label} could not be changed`}>{error.message}</Notice>
        </div>
      )}
      {sources.length === 0 ? (
        <div style={{ padding: 16 }}>
          <EmptyState
            icon="database"
            title={sourceMetadataLoading ? 'Loading sources…' : 'No sources reported'}
            description={sourceMetadataLoading ? undefined : 'The dashboard did not report any data sources.'}
          />
        </div>
      ) : (
        <ul style={{ listStyle: 'none', margin: 0, padding: 0 }}>
          {sources.map((source, index) => {
            const enabled = source.enabled !== false
            const busy = pending.has(source.id)
            const state = availability(source, enabled)
            const reason = asSentence(source.diagnostics?.reason)
            return (
              <li
                key={source.id}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 12,
                  padding: '12px 16px',
                  borderTop: index === 0 ? 'none' : '1px solid var(--border-subtle)',
                }}
              >
                <span style={{ opacity: enabled ? 1 : 0.5, display: 'inline-flex', flexShrink: 0 }}>
                  <VendorChip id={source.id} label={false} size={28} />
                </span>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, rowGap: 4, flexWrap: 'wrap' }}>
                    <span style={{ font: '600 13.5px/1.3 var(--font-ui)', color: enabled ? 'var(--fg-primary)' : 'var(--fg-secondary)' }}>{source.label}</span>
                    <Badge tone={state.tone} dot={enabled}>{state.label}</Badge>
                    {source.id === selectedSourceId && enabled && <Badge tone="accent">Selected</Badge>}
                  </div>
                  {source.path && (
                    <div title={source.path} style={{ font: '400 11.5px/1.45 var(--font-mono)', color: 'var(--fg-muted)', overflowWrap: 'anywhere', marginTop: 4 }}>
                      {source.path}
                    </div>
                  )}
                  {reason && (
                    <div style={{ font: '400 12px/1.45 var(--font-ui)', color: 'var(--fg-muted)', marginTop: 3 }}>{reason}</div>
                  )}
                </div>
                <span aria-hidden="true" style={{ font: '500 12px/1 var(--font-ui)', color: 'var(--fg-muted)', minWidth: 46, textAlign: 'right' }}>
                  {busy ? 'Saving…' : enabled ? 'On' : 'Off'}
                </span>
                <Switch
                  checked={enabled}
                  busy={busy}
                  label={`Enable ${source.label}`}
                  onChange={(next) => void toggle(source, next)}
                />
              </li>
            )
          })}
        </ul>
      )}
    </Card>
  )
}

/* Source context for the page body (Vael).
 *
 * SourceNotice — a one-line strip naming the selected source and how its costs
 *   are derived, with the path, cost policy and privacy notes one click away.
 *   It stays out of the way: reference detail is collapsed, and nothing is
 *   shown on the Overview (which spans every source) or on Config (which has
 *   its own source section).
 * SourceBlocked — the explanation shown in place of a view that has no source
 *   it can query. */
import { useId, useState, type ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { VendorChip } from '../vael/atoms'
import { Card } from '../vael/card'
import { Button } from '../vael/controls'
import { EmptyState, Notice } from '../vael/feedback'
import { Icon, type IconName } from '../vael/icon'
import { useDashboardContext } from '../layout/dashboard-context'
import type { SourceBlock } from './source-block'
import type { SourceInfo } from '../../types/api'

function kindLabel(kind: string | undefined) {
  return kind === 'sqlite' ? 'SQLite database' : kind === 'jsonl' ? 'Local transcripts' : null
}

function dataLayoutLabel(layout: string) {
  return layout === 'antigravity-sqlite-pb-v1' ? 'Antigravity conversations' : layout === 'opencode-v2' ? 'OpenCode 2 schema' : layout
}

/** What the reader needs to know about a cost figure before trusting it. */
function costPolicyLabel(status: string | undefined) {
  switch (status) {
    case 'reported':
      return 'Reported costs'
    case 'computed':
      return 'Costs computed from token prices'
    case 'mixed':
      return 'Reported and computed costs'
    case 'approximate':
      return 'Approximate costs'
    case 'estimated_api_equivalent':
      return 'API-equivalent cost estimates'
    case 'missing':
      return 'No cost data'
    default:
      return null
  }
}

function isEstimate(status: string | undefined) {
  return status === 'approximate' || status === 'estimated_api_equivalent' || status === 'missing'
}

/** Every caveat the backend attached to a source, de-duplicated. */
function sourceCaveats(source: SourceInfo) {
  return Array.from(new Set([...(source.warnings ?? []), ...(source.privacy?.warnings ?? [])]))
}

export function SourceNotice() {
  const { pathname } = useLocation()
  const { requestRefresh, selectedSourceInfo, sourceMetadataError, sourceStateError } = useDashboardContext()
  const [expanded, setExpanded] = useState(false)
  const detailsId = useId()
  const onOverview = pathname.startsWith('/overview')
  const onConfig = pathname.startsWith('/config')

  const metadataError = sourceMetadataError && (
    <Notice
      tone="warning"
      title="Couldn't load the data-source list"
      action={<Button size="sm" variant="secondary" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}
    >
      {sourceMetadataError}
    </Notice>
  )

  // Config has no view-level blocker, so a problem with the selected source is
  // reported here; everywhere else SourceBlocked takes over the page.
  const configProblem = onConfig && sourceStateError && (
    <Notice tone={sourceStateError.kind === 'invalid' || sourceStateError.kind === 'unsupported' ? 'danger' : 'warning'}>
      {sourceStateError.message}
    </Notice>
  )

  const source = selectedSourceInfo
  const showStrip = Boolean(source) && !onOverview && !onConfig && !sourceStateError
  if (!metadataError && !configProblem && !showStrip) return null

  const costStatus = source?.cost_policy?.status
  const facts = source
    ? [
        kindLabel(source.kind),
        source.read_only ? 'Read-only' : null,
        source.local_only ? 'Local only' : null,
      ].filter((fact): fact is string => Boolean(fact))
    : []
  const costLabel = costPolicyLabel(costStatus)
  const caveats = source ? sourceCaveats(source) : []
  const diagnostics = source?.diagnostics

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 12 }}>
      {metadataError}
      {configProblem}
      {showStrip && source && (
        <section aria-label="Data source details" style={{ border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-lg)', background: 'var(--ink-850)' }}>
          <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '6px 12px', padding: '6px 6px 6px 12px', minHeight: 40 }}>
            <VendorChip id={source.id} size={20} />
            <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '4px 12px', flex: '1 1 240px', minWidth: 0, font: '400 12px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>
              {/* Reference facts give way to the cost policy on a phone; they
                  are repeated under Details. */}
              {facts.map((fact) => (
                <span key={fact} className="hidden sm:inline" style={{ whiteSpace: 'nowrap' }}>{fact}</span>
              ))}
              {costLabel && (
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 5, whiteSpace: 'nowrap', color: isEstimate(costStatus) ? 'var(--warning)' : 'var(--fg-muted)' }}>
                  <Icon name={isEstimate(costStatus) ? 'alert-triangle' : 'check'} size={12} />
                  {costLabel}
                </span>
              )}
            </div>
            <button
              type="button"
              className="vael-link-button"
              aria-expanded={expanded}
              aria-controls={detailsId}
              onClick={() => setExpanded((value) => !value)}
            >
              {expanded ? 'Hide details' : 'Details'}
              <Icon name="chevron-down" size={14} style={{ transform: expanded ? 'rotate(180deg)' : 'none', transition: 'transform var(--dur-fast)' }} />
            </button>
          </div>

          {expanded && (
            <dl id={detailsId} className="vael-source-details">
              {source.path && (
                <DetailRow icon="folder" term="Location">
                  <code style={{ font: '400 12px/1.5 var(--font-mono)', color: 'var(--fg-primary)', overflowWrap: 'anywhere' }}>{source.path}</code>
                  {source.path_source && <span style={{ color: 'var(--fg-muted)' }}> · {source.path_source}</span>}
                </DetailRow>
              )}
              {facts.length > 0 && (
                <DetailRow icon="database" term="Access">
                  {facts.join(' · ')}
                  {source.read_only && source.local_only && ' — the dashboard never changes or uploads this data.'}
                </DetailRow>
              )}
              {source.data_layout && <DetailRow icon="layers" term="Format">{dataLayoutLabel(source.data_layout)}</DetailRow>}
              {source.cost_policy?.note && <DetailRow icon="dollar" term="Costs">{source.cost_policy.note}</DetailRow>}
              {(diagnostics?.reason || diagnostics?.scanned_files !== undefined) && (
                <DetailRow icon="activity" term="Last scan">
                  {[
                    diagnostics.reason,
                    diagnostics.scanned_files !== undefined ? `${diagnostics.scanned_files.toLocaleString('en-US')} files read` : null,
                    diagnostics.malformed_lines ? `${diagnostics.malformed_lines.toLocaleString('en-US')} malformed lines skipped` : null,
                    diagnostics.unsupported_events ? `${diagnostics.unsupported_events.toLocaleString('en-US')} unsupported events skipped` : null,
                  ].filter(Boolean).join(' · ')}
                </DetailRow>
              )}
              {(caveats.length > 0 || source.privacy?.plaintext_transcripts) && (
                <DetailRow icon="info" term="Privacy">
                  {caveats.length > 0 ? (
                    <ul style={{ margin: 0, paddingLeft: 16, listStyle: 'disc', display: 'flex', flexDirection: 'column', gap: 2 }}>
                      {caveats.map((caveat) => <li key={caveat}>{caveat}</li>)}
                    </ul>
                  ) : (
                    'Transcripts are plaintext local files and may contain sensitive prompts, file contents and tool output.'
                  )}
                </DetailRow>
              )}
            </dl>
          )}
        </section>
      )}
    </div>
  )
}

function DetailRow({ icon, term, children }: { icon: IconName; term: string; children: ReactNode }) {
  return (
    <div className="vael-source-detail">
      <dt>
        <Icon name={icon} size={13} color="var(--fg-faint)" />
        {term}
      </dt>
      <dd>{children}</dd>
    </div>
  )
}

export function SourceBlocked({ block }: { block: SourceBlock }) {
  const navigate = useNavigate()
  const { selectedSourceId, setSelectedSourceId, sources } = useDashboardContext()
  const fallback = sources.find((source) => source.enabled !== false && source.available && source.id !== selectedSourceId)

  return (
    <Card>
      <EmptyState
        icon={block.icon}
        title={block.title}
        description={block.description}
        action={
          <div style={{ display: 'flex', flexWrap: 'wrap', justifyContent: 'center', gap: 8 }}>
            {fallback && (
              <Button variant="primary" size="sm" onClick={() => setSelectedSourceId(fallback.id)}>
                Switch to {fallback.label}
              </Button>
            )}
            <Button variant="secondary" size="sm" iconLeft="settings" onClick={() => navigate('/config#data-sources')}>
              Manage sources
            </Button>
          </div>
        }
      />
    </Card>
  )
}

/* Source notice (Vael) — surfaces the selected source's path, cost policy, privacy
   warnings, and any source-state error. Rendered at the top of the page body. */
import { Badge } from '../vael/atoms'
import { Icon, type IconName } from '../vael/icon'
import { useDashboardContext } from '../layout/dashboard-context'

function compactPath(path: string) {
  if (path.length <= 96) return path
  return `…${path.slice(-93)}`
}

type Tone = 'info' | 'warning' | 'danger'

const TONE: Record<Tone, { fg: string; soft: string; icon: IconName }> = {
  info: { fg: 'var(--blue-300)', soft: 'var(--accent-soft)', icon: 'info' },
  warning: { fg: 'var(--warning)', soft: 'var(--warning-soft)', icon: 'alert-triangle' },
  danger: { fg: 'var(--danger)', soft: 'var(--danger-soft)', icon: 'alert-triangle' },
}

export function SourceNotice() {
  const { selectedSourceId, selectedSourceInfo, sourceMetadataError, sourceMetadataLoading, sourceStateError } = useDashboardContext()

  if (sourceMetadataLoading && !selectedSourceInfo) {
    return (
      <div style={{ display: 'flex', gap: 10, padding: '10px 12px', borderRadius: 'var(--radius-lg)', background: 'var(--accent-soft)', border: '1px solid var(--border-subtle)', marginBottom: 16 }}>
        <Icon name="info" size={16} color="var(--blue-300)" />
        <span style={{ font: '400 13px/1.4 var(--font-ui)', color: 'var(--fg-secondary)' }}>Loading data-source metadata…</span>
      </div>
    )
  }

  if (!selectedSourceInfo && !sourceMetadataError && !sourceStateError) return null

  const sourceLabel = selectedSourceInfo?.label ?? selectedSourceId
  const diagnosticsReason = selectedSourceInfo?.diagnostics?.reason
  const warnings = [
    ...(sourceMetadataError ? [`Source metadata warning: ${sourceMetadataError}`] : []),
    ...(sourceStateError ? [sourceStateError.message] : []),
    ...(selectedSourceInfo?.warnings ?? []),
    ...(selectedSourceInfo?.privacy?.warnings ?? []),
  ]
  if (selectedSourceInfo?.privacy?.plaintext_transcripts) {
    warnings.push('Plaintext local transcripts may include sensitive prompt, file, and tool-output content.')
  }

  const tone: Tone = sourceStateError
    ? sourceStateError.kind === 'invalid' || sourceStateError.kind === 'unsupported'
      ? 'danger'
      : 'warning'
    : warnings.length > 0
      ? 'warning'
      : 'info'
  const t = TONE[tone]

  return (
    <div style={{ display: 'flex', gap: 12, padding: '12px 14px', borderRadius: 'var(--radius-lg)', background: t.soft, border: '1px solid var(--border-subtle)', marginBottom: 16 }}>
      <Icon name={t.icon} size={17} color={t.fg} style={{ marginTop: 1 }} />
      <div style={{ minWidth: 0, flex: 1, display: 'flex', flexDirection: 'column', gap: 10 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
          <Badge tone={selectedSourceInfo?.available ? 'accent' : 'warning'}>{sourceLabel}</Badge>
          <Badge>{selectedSourceInfo?.kind ?? 'source'}</Badge>
          {selectedSourceInfo?.read_only && <Badge tone="success">read-only</Badge>}
          {selectedSourceInfo?.local_only && <Badge tone="success">local-only</Badge>}
          {selectedSourceInfo?.cost_policy?.status && <Badge>cost {selectedSourceInfo.cost_policy.status}</Badge>}
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 4, font: '400 13px/1.5 var(--font-ui)', color: 'var(--fg-secondary)' }}>
          {selectedSourceInfo?.path && (
            <div>
              <span style={{ color: 'var(--fg-primary)', fontWeight: 600 }}>Path:</span>{' '}
              <code style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--fg-primary)', background: 'var(--ink-850)', borderRadius: 'var(--radius-xs)', padding: '1px 6px' }}>{compactPath(selectedSourceInfo.path)}</code>
              {selectedSourceInfo.path_source && <span style={{ marginLeft: 8, fontSize: 12, color: 'var(--fg-muted)' }}>({selectedSourceInfo.path_source})</span>}
            </div>
          )}
          {diagnosticsReason && !sourceStateError && <div>{diagnosticsReason}</div>}
          {selectedSourceInfo?.cost_policy?.note && <div>{selectedSourceInfo.cost_policy.note}</div>}
        </div>

        {warnings.length > 0 && (
          <ul style={{ margin: 0, paddingLeft: 18, display: 'flex', flexDirection: 'column', gap: 4, font: '400 13px/1.5 var(--font-ui)', color: 'var(--fg-secondary)' }}>
            {Array.from(new Set(warnings)).map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

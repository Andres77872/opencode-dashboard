import { Alert } from '../ui/alert'
import { Badge } from '../ui/badge'
import { useDashboardContext } from '../layout/dashboard-context'

function compactPath(path: string) {
  if (path.length <= 96) {
    return path
  }
  return `…${path.slice(-93)}`
}

export function SourceNotice() {
  const {
    selectedSourceId,
    selectedSourceInfo,
    sourceMetadataError,
    sourceMetadataLoading,
    sourceStateError,
  } = useDashboardContext()

  if (sourceMetadataLoading && !selectedSourceInfo) {
    return (
      <Alert tone="info" className="mb-5">
        Loading data-source metadata…
      </Alert>
    )
  }

  if (!selectedSourceInfo && !sourceMetadataError && !sourceStateError) {
    return null
  }

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

  const tone = sourceStateError
    ? sourceStateError.kind === 'invalid' || sourceStateError.kind === 'unsupported'
      ? 'danger'
      : 'warning'
    : warnings.length > 0
      ? 'warning'
      : 'info'

  return (
    <Alert tone={tone} className="mb-5 space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={selectedSourceInfo?.available ? 'accent' : 'warning'}>{sourceLabel}</Badge>
        <Badge>{selectedSourceInfo?.kind ?? 'source'}</Badge>
        {selectedSourceInfo?.read_only ? <Badge tone="success">read-only</Badge> : null}
        {selectedSourceInfo?.local_only ? <Badge tone="success">local-only</Badge> : null}
        {selectedSourceInfo?.cost_policy?.status ? <Badge>cost {selectedSourceInfo.cost_policy.status}</Badge> : null}
      </div>

      <div className="space-y-1 text-sm leading-6">
        {selectedSourceInfo?.path ? (
          <div>
            <span className="font-medium text-foreground">Path:</span>{' '}
            <code className="rounded bg-background/45 px-1.5 py-0.5 font-mono text-xs text-foreground">
              {compactPath(selectedSourceInfo.path)}
            </code>
            {selectedSourceInfo.path_source ? (
              <span className="ml-2 text-xs opacity-80">({selectedSourceInfo.path_source})</span>
            ) : null}
          </div>
        ) : null}

        {diagnosticsReason && !sourceStateError ? <div>{diagnosticsReason}</div> : null}
        {selectedSourceInfo?.cost_policy?.note ? <div>{selectedSourceInfo.cost_policy.note}</div> : null}
      </div>

      {warnings.length > 0 ? (
        <ul className="list-disc space-y-1 pl-5 text-sm leading-6">
          {Array.from(new Set(warnings)).map((warning) => (
            <li key={warning}>{warning}</li>
          ))}
        </ul>
      ) : null}
    </Alert>
  )
}

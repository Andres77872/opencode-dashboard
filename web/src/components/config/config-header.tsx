/* Config header: path + format/source/redaction badges + copy actions on the
   first row; a compact meta strip (status · values · redacted — replaces the
   old StatCard row), filter input and Tree|Source toggle on the second. */
import { Badge, Icon, SearchInput, SegmentedControl } from '../vael'
import { Card } from '../vael'
import { CopyButton } from './copy-button'
import { formatInteger } from '../../lib/format'
import type { ConfigStats } from '../../types/api'
import type { ConfigDocument, ConfigInsights, ConfigViewMode } from '../../types/config'

export interface ConfigHeaderProps {
  data: ConfigStats
  doc: ConfigDocument
  insights: ConfigInsights
  status: 'present' | 'missing' | 'parse-error'
  searchValue: string
  onSearchChange: (value: string) => void
  viewMode: ConfigViewMode
  onViewModeChange: (mode: ConfigViewMode) => void
  showModeToggle: boolean
  showFilter: boolean
  matchSummary: string | null
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

const STATUS_LABELS: Record<ConfigHeaderProps['status'], string> = {
  present: 'Present',
  missing: 'Missing',
  'parse-error': 'Parse failed',
}

export function ConfigHeader({
  data,
  doc,
  insights,
  status,
  searchValue,
  onSearchChange,
  viewMode,
  onViewModeChange,
  showModeToggle,
  showFilter,
  matchSummary,
  copiedId,
  onCopy,
}: ConfigHeaderProps) {
  const statusColor =
    status === 'present' ? 'var(--success)' : status === 'missing' ? 'var(--fg-muted)' : 'var(--warning)'

  return (
    <Card pad={14}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {/* Row 1: path + badges + copy actions */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
          <Icon name="folder" size={15} color="var(--fg-faint)" />
          <span
            title={data.path}
            style={{
              font: '500 12.5px/1.4 var(--font-mono)',
              color: 'var(--fg-secondary)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              minWidth: 0,
              flex: '1 1 240px',
            }}
          >
            {data.path || 'Unavailable'}
          </span>
          <Badge tone="accent">{doc.format.toUpperCase()}</Badge>
          {data.source_id && <Badge>{data.source_id}</Badge>}
          {data.redacted && <Badge tone="warning" dot>redacted</Badge>}
          <div style={{ display: 'flex', gap: 6, flexShrink: 0 }}>
            <CopyButton copyId="config-path" copiedId={copiedId} label="Copy path" value={data.path || 'Unavailable'} onCopy={onCopy} />
            {doc.raw && (
              <CopyButton copyId="config-file" copiedId={copiedId} label="Copy file" value={doc.raw} onCopy={onCopy} />
            )}
          </div>
        </div>

        {/* Row 2: meta strip + filter + view toggle */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 8,
              font: '500 12px/1 var(--font-mono)',
              color: 'var(--fg-muted)',
              fontVariantNumeric: 'tabular-nums',
              whiteSpace: 'nowrap',
            }}
          >
            <span style={{ color: statusColor }}>{STATUS_LABELS[status]}</span>
            {status === 'present' && (
              <>
                <Dot />
                <span>{formatInteger(insights.leafValues)} values</span>
                <Dot />
                <span style={{ color: insights.redactedValues > 0 ? 'var(--warning)' : undefined }}>
                  {formatInteger(insights.redactedValues)} redacted
                </span>
              </>
            )}
            {doc.rawSynthesized && (
              <>
                <Dot />
                <span style={{ color: 'var(--fg-faint)' }}>raw synthesized</span>
              </>
            )}
          </span>

          <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
            {showFilter && (
              <SearchInput
                value={searchValue}
                onChange={onSearchChange}
                placeholder="Filter keys and values…"
                label="Filter config keys and values"
                width={240}
              />
            )}
            {matchSummary && (
              <span style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>
                {matchSummary}
              </span>
            )}
            {showModeToggle && (
              <SegmentedControl<ConfigViewMode>
                options={[
                  { value: 'tree', label: 'Tree' },
                  { value: 'source', label: 'Source' },
                ]}
                value={viewMode}
                onChange={onViewModeChange}
                size="sm"
              />
            )}
          </div>
        </div>
      </div>
    </Card>
  )
}

function Dot() {
  return <span style={{ color: 'var(--fg-faint)' }}>·</span>
}

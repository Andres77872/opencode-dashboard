/* Config header: the resolved path, format and redaction badges and the copy
   actions on the first row; a compact meta strip (status · values · redacted ·
   merged files), the filter and the Tree|Source toggle on the second. Each row
   is laid out so its groups wrap as whole units instead of breaking apart. */
import { Badge, Card, Icon, SearchInput, SegmentedControl, VendorChip } from '../vael'
import { CopyButton } from './copy-button'
import { formatCount } from '../../lib/format'
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
}

const STATUS_LABELS: Record<ConfigHeaderProps['status'], string> = {
  present: 'File found',
  missing: 'No file',
  'parse-error': 'Could not be parsed',
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
}: ConfigHeaderProps) {
  const statusColor =
    status === 'present' ? 'var(--success)' : status === 'missing' ? 'var(--fg-muted)' : 'var(--warning)'
  const mergedPaths = data.merged_paths ?? []
  const hasControls = showFilter || showModeToggle

  return (
    <Card pad={14}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {/* Row 1: path, then badges and copy actions as one trailing group. */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, rowGap: 8, flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0, flex: '1 1 260px' }}>
            {data.source_id ? <VendorChip id={data.source_id} label={false} size={20} /> : <Icon name="folder" size={15} color="var(--fg-faint)" />}
            <span
              title={data.path || undefined}
              style={{
                font: '500 12.5px/1.4 var(--font-mono)',
                color: data.path ? 'var(--fg-secondary)' : 'var(--fg-muted)',
                overflow: 'hidden',
                textOverflow: 'ellipsis',
                whiteSpace: 'nowrap',
                minWidth: 0,
              }}
            >
              {data.path || 'No config path resolved'}
            </span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
            <Badge tone="accent">{doc.format.toUpperCase()}</Badge>
            {data.redacted && (
              <span title="Secrets were replaced before this file left the server.">
                <Badge tone="warning" dot>Redacted</Badge>
              </span>
            )}
            <CopyButton
              label="Copy path"
              value={data.path ?? ''}
              disabled={!data.path}
              disabledReason="No config path was resolved for this source."
            />
            {doc.raw && <CopyButton label="Copy file" value={doc.raw} />}
          </div>
        </div>

        {/* Row 2: meta strip, then filter + view toggle as one trailing group. */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, rowGap: 10, flexWrap: 'wrap' }}>
          <span
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 8,
              rowGap: 4,
              flexWrap: 'wrap',
              flex: '1 1 220px',
              minWidth: 0,
              font: '500 12px/1.3 var(--font-mono)',
              color: 'var(--fg-muted)',
              fontVariantNumeric: 'tabular-nums',
            }}
          >
            <span style={{ color: statusColor, fontFamily: 'var(--font-ui)', fontWeight: 600 }}>{STATUS_LABELS[status]}</span>
            {status === 'present' && (
              <>
                <Dot />
                <span>{formatCount(insights.leafValues, 'value')}</span>
                <Dot />
                <span style={{ color: insights.redactedValues > 0 ? 'var(--warning)' : undefined }}>
                  {formatCount(insights.redactedValues, 'redacted value')}
                </span>
              </>
            )}
            {doc.rawSynthesized && (
              <>
                <Dot />
                <span title="The original file text is unavailable, so the source view is rebuilt from the parsed values." style={{ color: 'var(--fg-faint)' }}>source rebuilt</span>
              </>
            )}
            {mergedPaths.length > 1 && (
              <>
                <Dot />
                <span title={mergedPaths.join('\n')}>merged from {mergedPaths.map(fileName).join(' + ')}</span>
              </>
            )}
          </span>

          {hasControls && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flex: '0 1 auto', minWidth: 0, maxWidth: '100%' }}>
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
                <span aria-live="polite" style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}>
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
                  ariaLabel="Config view"
                />
              )}
            </div>
          )}
        </div>
      </div>
    </Card>
  )
}

function fileName(path: string) {
  return path.split(/[\\/]/).pop() || path
}

function Dot() {
  return <span aria-hidden="true" style={{ color: 'var(--fg-faint)' }}>·</span>
}

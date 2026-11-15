/* Global data-source switcher (Vael). Aggregate read-only state on /overview;
   per-source selector elsewhere. Pending/unavailable sources stay visible so the
   page can explain why they can't be queried. */
import { useMemo } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { PickerButton, Popover } from '../vael/controls'
import { Icon, type IconName } from '../vael/icon'
import { useDashboardContext } from '../layout/dashboard-context'
import type { SourceID, SourceInfo } from '../../types/api'

const KIND_ICON: Record<string, IconName> = {
  sqlite: 'database',
  jsonl: 'file-text',
}

function kindIcon(kind: string | undefined): IconName {
  return KIND_ICON[kind ?? ''] ?? 'database'
}

function kindLabel(kind: string | undefined): string {
  switch (kind) {
    case 'sqlite':
      return 'SQLite database'
    case 'jsonl':
      return 'JSONL transcripts'
    default:
      return kind ?? 'source'
  }
}

function sourceSubtitle(source: SourceInfo): string {
  if (!source.available) return source.diagnostics?.reason ?? 'Not available'
  if (source.path) return source.path_source ? `${source.path_source}: ${source.path}` : source.path
  return kindLabel(source.kind)
}

function StatusDot({ available }: { available: boolean }) {
  return (
    <span
      aria-hidden
      style={{
        width: 8,
        height: 8,
        borderRadius: '50%',
        flexShrink: 0,
        marginTop: 5,
        background: available ? 'var(--success)' : 'var(--warning)',
        boxShadow: available ? 'var(--glow-success)' : 'none',
      }}
    />
  )
}

export function SourcePicker() {
  const { selectedSourceId, selectedSourceInfo, setSelectedSourceId, sourceMetadataLoading, sources: allSources } = useDashboardContext()
  const sources = useMemo(() => allSources.filter(source => source.enabled !== false), [allSources])
  const { pathname } = useLocation()
  const aggregate = pathname.startsWith('/overview')
  const onConfig = pathname.startsWith('/config')

  const options = useMemo(() => {
    if (selectedSourceInfo && selectedSourceInfo.enabled !== false && !sources.some((s) => s.id === selectedSourceInfo.id)) {
      return [...sources, selectedSourceInfo]
    }
    return sources
  }, [selectedSourceInfo, sources])

  const listItems = aggregate ? sources : options
  const availableCount = listItems.filter((s) => s.available).length
  const loadingEmpty = sourceMetadataLoading && options.length === 0

  const triggerIcon: IconName = aggregate ? 'layers' : kindIcon(selectedSourceInfo?.kind)
  const triggerLabel = sources.length === 0 && !sourceMetadataLoading ? 'No active sources' : loadingEmpty ? 'Loading sources…' : aggregate ? 'All sources' : selectedSourceInfo?.label ?? selectedSourceId
  const triggerUnavailable = !aggregate && selectedSourceInfo?.available === false

  return (
    <Popover
      portal
      flush
      width={320}
      closeOnClick={false}
      label="Data source"
      trigger={(open, toggle) => (
        <PickerButton
          open={open}
          onClick={toggle}
          disabled={loadingEmpty}
          popup="dialog"
          icon={triggerIcon}
          iconColor={triggerUnavailable ? 'var(--warning)' : undefined}
          ariaLabel={`Data source: ${triggerLabel}`}
        >
          {triggerLabel}
        </PickerButton>
      )}
    >
      {(close) => (
        <>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, padding: '10px 12px', borderBottom: '1px solid var(--border-default)' }}>
            <span style={{ font: '600 12px/1 var(--font-ui)', color: 'var(--fg-secondary)' }}>{aggregate ? 'Included in the overview' : 'Data source'}</span>
            {availableCount > 0 && <span style={{ font: '500 11px/1 var(--font-mono)', color: 'var(--fg-muted)' }}>{availableCount} available</span>}
          </div>

          {/* A real radio group when choosing; a plain list on the Overview,
              where every source is included and there is nothing to pick. */}
          <div role={aggregate ? 'list' : 'radiogroup'} aria-label={aggregate ? 'Sources included' : 'Data source'} style={{ maxHeight: 288, overflowY: 'auto', padding: 5 }}>
            {listItems.length === 0 && (
              <div style={{ padding: '14px 10px', font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)' }}>No data sources are enabled.</div>
            )}
            {listItems.map((source) => {
              const selected = !aggregate && source.id === selectedSourceId
              const inner = (
                <>
                  <StatusDot available={source.available} />
                  <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0, flex: 1 }}>
                    <span style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                      <span style={{ font: '600 13px/1 var(--font-ui)', color: 'var(--fg-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{source.label}</span>
                      {source.default && <span style={{ font: '600 10px/1 var(--font-ui)', color: 'var(--fg-muted)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-pill)', padding: '2px 6px' }}>Default</span>}
                      {!source.available && <span style={{ font: '600 10px/1 var(--font-ui)', color: 'var(--warning)', background: 'var(--warning-soft)', borderRadius: 'var(--radius-pill)', padding: '2px 6px' }}>Unavailable</span>}
                    </span>
                    <span title={sourceSubtitle(source)} style={{ display: 'flex', alignItems: 'center', gap: 6, font: '400 12px/1.3 var(--font-mono)', color: 'var(--fg-muted)', minWidth: 0 }}>
                      <Icon name={kindIcon(source.kind)} size={12} color="var(--fg-faint)" style={{ flexShrink: 0 }} />
                      <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{sourceSubtitle(source)}</span>
                    </span>
                  </span>
                  {selected && <Icon name="check" size={16} color="var(--accent)" style={{ marginTop: 2 }} />}
                </>
              )

              if (aggregate) {
                return (
                  <div key={source.id} role="listitem" style={{ display: 'flex', alignItems: 'flex-start', gap: 10, padding: '8px 8px', borderRadius: 'var(--radius-md)' }}>
                    {inner}
                  </div>
                )
              }

              return (
                <button
                  key={source.id}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  onClick={() => {
                    if (source.id !== selectedSourceId) setSelectedSourceId(source.id as SourceID)
                    close()
                  }}
                  style={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 10,
                    width: '100%',
                    padding: '8px 8px',
                    border: 'none',
                    borderRadius: 'var(--radius-md)',
                    background: selected ? 'var(--accent-soft)' : 'transparent',
                    cursor: 'pointer',
                    textAlign: 'left',
                  }}
                  onMouseEnter={(e) => { if (!selected) e.currentTarget.style.background = 'var(--ink-650)' }}
                  onMouseLeave={(e) => { if (!selected) e.currentTarget.style.background = 'transparent' }}
                >
                  {inner}
                </button>
              )
            })}
          </div>

          <p style={{ margin: 0, borderTop: '1px solid var(--border-default)', padding: '9px 12px', font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)' }}>
            {aggregate
              ? 'The overview adds up every enabled source. Costs stay separate per source.'
              : 'Applies to every view except the overview.'}
            {!onConfig && (
              <>
                {' '}
                <Link to="/config#data-sources" onClick={close} style={{ color: 'var(--blue-300)' }}>Manage sources</Link>
              </>
            )}
          </p>
        </>
      )}
    </Popover>
  )
}

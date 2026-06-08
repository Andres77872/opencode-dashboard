/* Global data-source switcher (Vael). Aggregate read-only state on /overview;
   per-source selector elsewhere. Pending/unavailable sources stay visible so the
   SourceNotice can explain why they can't be queried. */
import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
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
  const { selectedSourceId, selectedSourceInfo, setSelectedSourceId, sourceMetadataLoading, sources } = useDashboardContext()
  const aggregate = useLocation().pathname.startsWith('/overview')
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const options = useMemo(() => {
    if (selectedSourceInfo && !sources.some((s) => s.id === selectedSourceInfo.id)) {
      return [...sources, selectedSourceInfo]
    }
    return sources
  }, [selectedSourceInfo, sources])

  const listItems = aggregate ? sources : options
  const availableCount = listItems.filter((s) => s.available).length
  const singleSource = options.length <= 1
  const loadingEmpty = sourceMetadataLoading && options.length === 0

  const selectRow = (sourceId: SourceID) => {
    if (sourceId !== selectedSourceId) setSelectedSourceId(sourceId)
    setOpen(false)
  }

  const triggerIcon: IconName = aggregate ? 'layers' : kindIcon(selectedSourceInfo?.kind)
  const triggerLabel = loadingEmpty ? 'Loading sources…' : aggregate ? 'All sources' : selectedSourceInfo?.label ?? selectedSourceId
  const triggerUnavailable = !aggregate && selectedSourceInfo?.available === false

  return (
    <div ref={ref} style={{ position: 'relative', display: 'inline-flex' }}>
      <button
        type="button"
        onClick={() => !loadingEmpty && setOpen((v) => !v)}
        disabled={loadingEmpty}
        aria-haspopup="dialog"
        aria-expanded={open}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: 8,
          height: 32,
          padding: '0 11px',
          background: open ? 'var(--ink-700)' : 'var(--ink-750)',
          border: `1px solid ${open ? 'var(--border-accent)' : 'var(--border-default)'}`,
          borderRadius: 'var(--radius-md)',
          cursor: loadingEmpty ? 'default' : 'pointer',
          font: '500 13px/1 var(--font-ui)',
          color: 'var(--fg-primary)',
          whiteSpace: 'nowrap',
          minWidth: 150,
        }}
      >
        <Icon name={triggerIcon} size={15} color={triggerUnavailable ? 'var(--warning)' : 'var(--fg-muted)'} />
        <span style={{ flex: 1, textAlign: 'left', overflow: 'hidden', textOverflow: 'ellipsis' }}>{triggerLabel}</span>
        <Icon name="chevron-down" size={14} color="var(--fg-muted)" style={{ transform: open ? 'rotate(180deg)' : 'none', transition: 'transform var(--dur-fast)' }} />
      </button>

      {open && (
        <div
          role="dialog"
          aria-label="Data source"
          style={{
            position: 'absolute',
            top: 'calc(100% + 6px)',
            right: 0,
            zIndex: 60,
            width: 320,
            maxWidth: 'calc(100vw - 2rem)',
            background: 'var(--ink-700)',
            border: '1px solid var(--border-strong)',
            borderRadius: 'var(--radius-lg)',
            boxShadow: 'var(--shadow-lg)',
            overflow: 'hidden',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, padding: '9px 12px', borderBottom: '1px solid var(--border-default)' }}>
            <span style={{ font: '600 11px/1 var(--font-ui)', letterSpacing: '0.12em', textTransform: 'uppercase', color: 'var(--fg-muted)' }}>{aggregate ? 'Overview · all sources' : 'Data source'}</span>
            {availableCount > 0 && <span style={{ font: '500 11px/1 var(--font-mono)', color: 'var(--fg-muted)' }}>{availableCount} available</span>}
          </div>

          <div style={{ maxHeight: 288, overflowY: 'auto', padding: 5 }}>
            {listItems.map((source) => {
              const selected = !aggregate && source.id === selectedSourceId
              const inner = (
                <>
                  <StatusDot available={source.available} />
                  <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0, flex: 1 }}>
                    <span style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
                      <span style={{ font: '600 13px/1 var(--font-ui)', color: 'var(--fg-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{source.label}</span>
                      {source.default && <span style={{ font: '600 9px/1 var(--font-ui)', letterSpacing: '0.1em', textTransform: 'uppercase', color: 'var(--fg-muted)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-pill)', padding: '2px 6px' }}>default</span>}
                      {!source.available && <span style={{ font: '600 9px/1 var(--font-ui)', letterSpacing: '0.1em', textTransform: 'uppercase', color: 'var(--warning)', background: 'var(--warning-soft)', borderRadius: 'var(--radius-pill)', padding: '2px 6px' }}>unavailable</span>}
                    </span>
                    <span style={{ display: 'flex', alignItems: 'center', gap: 6, font: '400 12px/1.3 var(--font-mono)', color: 'var(--fg-muted)', minWidth: 0 }}>
                      <Icon name={kindIcon(source.kind)} size={12} color="var(--fg-faint)" />
                      <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{sourceSubtitle(source)}</span>
                    </span>
                  </span>
                  {selected && <Icon name="check" size={16} color="var(--accent)" style={{ marginTop: 2 }} />}
                </>
              )

              if (aggregate) {
                return (
                  <div key={source.id} style={{ display: 'flex', alignItems: 'flex-start', gap: 10, padding: '8px 8px', borderRadius: 'var(--radius-md)' }}>
                    {inner}
                  </div>
                )
              }

              return (
                <button
                  key={source.id}
                  type="button"
                  onClick={() => selectRow(source.id)}
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
              ? 'The Overview aggregates every source. Costs are shown per source and never combined.'
              : singleSource
                ? 'Only one data source is configured. Start with --claude-home or --codex-home to add Claude Code or Codex.'
                : 'Applies to every view except the Overview, which always aggregates all sources.'}
          </p>
        </div>
      )}
    </div>
  )
}

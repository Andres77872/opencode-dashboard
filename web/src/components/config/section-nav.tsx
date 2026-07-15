/* Left-rail section navigator: "All sections" + one row per top-level config
   section with leaf-value counts and a redaction dot. Sticky in the two-pane
   layout; renders as a horizontal pill scroller when `horizontal`. During
   search, rows show match counts and non-matching rows dim. */
import { useState } from 'react'
import { Card } from '../vael'
import { ALL_SECTIONS } from '../../lib/config-utils'
import { formatInteger } from '../../lib/format'
import type { ConfigSectionProjection } from '../../types/config'

export interface SectionNavProps {
  projections: ConfigSectionProjection[]
  selectedKey: string
  onSelect: (key: string) => void
  searchActive: boolean
  horizontal?: boolean
}

interface NavEntry {
  key: string
  label: string
  count: number
  redacted: boolean
  dimmed: boolean
}

function buildEntries(projections: ConfigSectionProjection[], searchActive: boolean): NavEntry[] {
  const allCount = projections.reduce(
    (sum, p) => sum + (searchActive ? p.filteredInsights?.leafValues ?? 0 : p.insights.leafValues),
    0,
  )
  const entries: NavEntry[] = [
    {
      key: ALL_SECTIONS,
      label: 'All sections',
      count: allCount,
      redacted: false,
      dimmed: searchActive && allCount === 0,
    },
  ]
  for (const projection of projections) {
    const matches = projection.filteredValue !== null
    entries.push({
      key: projection.section.key,
      label: projection.section.key,
      count: searchActive ? projection.filteredInsights?.leafValues ?? 0 : projection.insights.leafValues,
      redacted: projection.insights.redactedValues > 0,
      dimmed: searchActive && !matches,
    })
  }
  return entries
}

export function SectionNav({ projections, selectedKey, onSelect, searchActive, horizontal }: SectionNavProps) {
  const entries = buildEntries(projections, searchActive)

  if (horizontal) {
    return (
      <div style={{ display: 'flex', gap: 6, overflowX: 'auto', paddingBottom: 4 }}>
        {entries.map((entry) => (
          <NavPill key={entry.key} entry={entry} active={entry.key === selectedKey} onSelect={onSelect} />
        ))}
      </div>
    )
  }

  return (
    <Card
      pad={6}
      style={{ position: 'sticky', top: 12, maxHeight: 'calc(100vh - 96px)', overflowY: 'auto' }}
    >
      <div
        style={{
          padding: '6px 10px 4px',
          font: '700 10px/1 var(--font-ui)',
          letterSpacing: '0.08em',
          textTransform: 'uppercase',
          color: 'var(--fg-muted)',
        }}
      >
        Sections
      </div>
      {entries.map((entry) => (
        <NavRow key={entry.key} entry={entry} active={entry.key === selectedKey} onSelect={onSelect} />
      ))}
    </Card>
  )
}

function NavRow({ entry, active, onSelect }: { entry: NavEntry; active: boolean; onSelect: (key: string) => void }) {
  const [hover, setHover] = useState(false)
  const isAll = entry.key === ALL_SECTIONS
  return (
    <button
      type="button"
      onClick={() => onSelect(entry.key)}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      aria-current={active ? 'true' : undefined}
      title={entry.label}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        width: '100%',
        minHeight: 30,
        padding: '5px 10px',
        textAlign: 'left',
        border: 'none',
        borderLeft: `2px solid ${active ? 'var(--accent)' : 'transparent'}`,
        background: active ? 'var(--accent-soft)' : hover ? 'var(--ink-750)' : 'transparent',
        borderRadius: 'var(--radius-sm)',
        cursor: 'pointer',
        opacity: entry.dimmed ? 0.4 : 1,
      }}
    >
      <span
        style={{
          minWidth: 0,
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          font: isAll ? '600 12.5px/1.4 var(--font-ui)' : '500 12.5px/1.4 var(--font-mono)',
          color: active ? 'var(--fg-primary)' : 'var(--fg-secondary)',
        }}
      >
        {entry.label}
      </span>
      {entry.redacted && (
        <span aria-label="Contains redacted values" style={{ width: 6, height: 6, borderRadius: 3, background: 'var(--warning)', flexShrink: 0 }} />
      )}
      <span
        style={{
          marginLeft: 'auto',
          flexShrink: 0,
          font: '500 11.5px/1 var(--font-mono)',
          color: 'var(--fg-muted)',
          fontVariantNumeric: 'tabular-nums',
        }}
      >
        {formatInteger(entry.count)}
      </span>
    </button>
  )
}

function NavPill({ entry, active, onSelect }: { entry: NavEntry; active: boolean; onSelect: (key: string) => void }) {
  return (
    <button
      type="button"
      onClick={() => onSelect(entry.key)}
      aria-current={active ? 'true' : undefined}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        flexShrink: 0,
        height: 28,
        padding: '0 10px',
        font: '500 12px/1 var(--font-mono)',
        color: active ? 'var(--fg-primary)' : 'var(--fg-secondary)',
        background: active ? 'var(--accent-soft)' : 'var(--ink-800)',
        border: `1px solid ${active ? 'var(--border-accent)' : 'var(--border-default)'}`,
        borderRadius: 'var(--radius-pill)',
        cursor: 'pointer',
        opacity: entry.dimmed ? 0.4 : 1,
        whiteSpace: 'nowrap',
      }}
    >
      {entry.label}
      {entry.redacted && <span style={{ width: 5, height: 5, borderRadius: 3, background: 'var(--warning)' }} />}
      <span style={{ color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>{formatInteger(entry.count)}</span>
    </button>
  )
}

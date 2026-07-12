import { Select } from 'web'
import type { ReactNode, CSSProperties } from 'react'

/* Vael is dark-first — every cell renders on the app's near-black canvas. */
function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, ...style }}>
      {children}
    </div>
  )
}

/* Closed trigger states — the open dropdown is exercised in the Popover previews. */
export const SourceFilter = () => (
  <Canvas>
    <Select
      label="Source"
      icon="filter"
      value="all"
      width={220}
      options={[
        { value: 'all', label: 'All sources' },
        { value: 'opencode', label: 'opencode', color: 'var(--vendor-opencode)' },
        { value: 'claude_code', label: 'claude_code', color: 'var(--vendor-claude)' },
        { value: 'codex', label: 'codex', color: 'var(--vendor-codex)' },
      ]}
    />
  </Canvas>
)

export const FilterBar = () => (
  <Canvas>
    <Select
      icon="calendar"
      value="30d"
      width={200}
      options={[
        { value: '7d', label: 'Last 7 days' },
        { value: '30d', label: 'Last 30 days' },
        { value: '90d', label: 'Last 90 days' },
      ]}
    />
    <Select
      label="Granularity"
      icon="clock"
      value="daily"
      width={180}
      options={[
        { value: 'hourly', label: 'Hourly' },
        { value: 'daily', label: 'Daily' },
        { value: 'weekly', label: 'Weekly' },
      ]}
    />
  </Canvas>
)

import { MenuItem } from 'web'
import type { ReactNode, CSSProperties } from 'react'

/* Vael is dark-first — every cell renders on the app's near-black canvas. */
function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', gap: 12, ...style }}>
      {children}
    </div>
  )
}

/* MenuItems live inside a Popover/Select panel — recreate that surface. */
function Panel({ children }: { children?: ReactNode }) {
  return (
    <div
      style={{
        width: 220,
        background: 'var(--ink-700)',
        border: '1px solid var(--border-strong)',
        borderRadius: 'var(--radius-lg)',
        boxShadow: 'var(--shadow-lg)',
        padding: 5,
      }}
    >
      {children}
    </div>
  )
}

export const SourceMenu = () => (
  <Canvas>
    <Panel>
      <MenuItem selected>All sources</MenuItem>
      <MenuItem color="var(--vendor-opencode)">opencode</MenuItem>
      <MenuItem color="var(--vendor-claude)">claude_code</MenuItem>
      <MenuItem color="var(--vendor-codex)">codex</MenuItem>
    </Panel>
  </Canvas>
)

export const ActionsMenu = () => (
  <Canvas>
    <Panel>
      <MenuItem>Refresh data</MenuItem>
      <MenuItem>Download CSV</MenuItem>
      <MenuItem selected>Group by model</MenuItem>
      <MenuItem>Copy share link</MenuItem>
    </Panel>
  </Canvas>
)

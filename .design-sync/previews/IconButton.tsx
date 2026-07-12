import { IconButton } from 'web'
import type { ReactNode, CSSProperties } from 'react'

/* Vael is dark-first — every cell renders on the app's near-black canvas. */
function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, ...style }}>
      {children}
    </div>
  )
}

function Tile({ caption, children }: { caption: string; children?: ReactNode }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 7 }}>
      {children}
      <span style={{ font: '400 10px/1 var(--font-mono)', color: 'var(--fg-faint)' }}>{caption}</span>
    </div>
  )
}

export const States = () => (
  <Canvas style={{ gap: 18 }}>
    <Tile caption="default">
      <IconButton name="settings" label="Settings" />
    </Tile>
    <Tile caption="active">
      <IconButton name="filter" label="Filters" active />
    </Tile>
    <Tile caption="spinning">
      <IconButton name="refresh" label="Refreshing data" spinning />
    </Tile>
    <Tile caption="disabled">
      <IconButton name="download" label="Export CSV" disabled />
    </Tile>
  </Canvas>
)

export const Toolbar = () => (
  <Canvas>
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 4,
        padding: '6px 10px',
        background: 'var(--ink-850)',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-lg)',
      }}
    >
      <span style={{ font: '600 13px/1 var(--font-ui)', color: 'var(--fg-primary)', marginRight: 8 }}>Token usage</span>
      <IconButton name="refresh" label="Refresh" />
      <IconButton name="download" label="Download CSV" />
      <IconButton name="line-chart" label="Line chart" active />
      <IconButton name="bar-chart" label="Bar chart" />
      <IconButton name="more-horizontal" label="More actions" />
    </div>
  </Canvas>
)

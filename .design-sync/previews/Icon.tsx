import { Icon } from 'web'
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
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 7, width: 88 }}>
      {children}
      <span style={{ font: '400 10px/1 var(--font-mono)', color: 'var(--fg-faint)', whiteSpace: 'nowrap' }}>{caption}</span>
    </div>
  )
}

const LIBRARY = [
  'dashboard',
  'line-chart',
  'bar-chart',
  'activity',
  'terminal',
  'cpu',
  'zap',
  'dollar',
  'database',
  'git-branch',
  'message-square',
  'hash',
  'filter',
  'search',
  'settings',
  'refresh',
  'download',
  'calendar',
  'clock',
  'layers',
] as const

export const Library = () => (
  <Canvas style={{ gap: 8, rowGap: 16, maxWidth: 640 }}>
    {LIBRARY.map((name) => (
      <Tile key={name} caption={name}>
        <Icon name={name} size={18} color="var(--fg-secondary)" />
      </Tile>
    ))}
  </Canvas>
)

export const Sizes = () => (
  <Canvas style={{ gap: 18 }}>
    {([14, 18, 24, 32] as const).map((s) => (
      <Tile key={s} caption={`${s}px`}>
        <div style={{ height: 32, display: 'flex', alignItems: 'center' }}>
          <Icon name="activity" size={s} color="var(--fg-secondary)" />
        </div>
      </Tile>
    ))}
  </Canvas>
)

export const Colors = () => (
  <Canvas style={{ gap: 18 }}>
    <Tile caption="accent">
      <Icon name="zap" size={20} color="var(--accent)" />
    </Tile>
    <Tile caption="success">
      <Icon name="trending-up" size={20} color="var(--success)" />
    </Tile>
    <Tile caption="danger">
      <Icon name="alert-triangle" size={20} color="var(--danger)" />
    </Tile>
    <Tile caption="warning">
      <Icon name="clock" size={20} color="var(--warning)" />
    </Tile>
  </Canvas>
)

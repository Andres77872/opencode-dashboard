import { Button } from 'web'
import type { ReactNode, CSSProperties } from 'react'

/* Vael is dark-first — every cell renders on the app's near-black canvas. */
function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, ...style }}>
      {children}
    </div>
  )
}

export const Variants = () => (
  <Canvas>
    <Button variant="primary">Add source</Button>
    <Button variant="secondary">Export CSV</Button>
    <Button variant="ghost">Cancel</Button>
  </Canvas>
)

export const WithIcons = () => (
  <Canvas>
    <Button variant="primary" iconLeft="plus">New budget</Button>
    <Button variant="secondary" iconLeft="download">Download report</Button>
    <Button variant="ghost" iconLeft="refresh">Refresh</Button>
  </Canvas>
)

export const Sizes = () => (
  <Canvas>
    <Button variant="secondary" size="md">Medium</Button>
    <Button variant="secondary" size="sm">Small</Button>
    <Button variant="primary" size="sm" iconLeft="filter">Apply filters</Button>
  </Canvas>
)

export const Disabled = () => (
  <Canvas>
    <Button variant="primary" disabled>Add source</Button>
    <Button variant="secondary" disabled iconLeft="download">Export CSV</Button>
  </Canvas>
)

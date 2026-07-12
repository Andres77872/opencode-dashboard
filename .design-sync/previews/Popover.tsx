import { Popover, MenuItem, Button } from 'web'
import { useEffect, useRef } from 'react'
import type { ReactNode, CSSProperties } from 'react'

/* Vael is dark-first — every cell renders on the app's near-black canvas. */
function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', gap: 12, ...style }}>
      {children}
    </div>
  )
}

export const Closed = () => (
  <Canvas style={{ alignItems: 'center' }}>
    <Popover
      width={220}
      trigger={(open, toggle) => (
        <Button variant="secondary" iconLeft="calendar" onClick={toggle}>
          Last 30 days
        </Button>
      )}
    >
      <MenuItem>Last 7 days</MenuItem>
      <MenuItem selected>Last 30 days</MenuItem>
      <MenuItem>Last 90 days</MenuItem>
    </Popover>
  </Canvas>
)

/* Opened for real: capture the render-prop toggle in a ref, fire it on mount. */
export const Open = () => {
  const t = useRef<(() => void) | null>(null)
  useEffect(() => {
    t.current?.()
  }, [])
  return (
    <Canvas style={{ minHeight: 260 }}>
      <Popover
        width={220}
        trigger={(open, toggle) => {
          t.current = toggle
          return (
            <Button variant="secondary" iconLeft="calendar" onClick={toggle}>
              Last 30 days
            </Button>
          )
        }}
      >
        <MenuItem>Last 7 days</MenuItem>
        <MenuItem selected>Last 30 days</MenuItem>
        <MenuItem>Last 90 days</MenuItem>
        <MenuItem>All time</MenuItem>
      </Popover>
    </Canvas>
  )
}

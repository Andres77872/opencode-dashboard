import { useEffect, useRef } from 'react'
import { Tooltip, Badge, Button } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

/* The tooltip only renders on hover (internal state), so force it visible by
   dispatching a real mouseover on the tooltip's wrapper span after mount. */
function ForceHover({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const span = ref.current?.querySelector('span')
    if (span) span.dispatchEvent(new MouseEvent('mouseover', { bubbles: true }))
  }, [])
  return (
    <div ref={ref} style={{ display: 'inline-flex' }}>
      {children}
    </div>
  )
}

export const Top = () => (
  <Canvas style={{ maxWidth: 420, paddingTop: 64, paddingBottom: 28, display: 'flex', justifyContent: 'center' }}>
    <ForceHover>
      <Tooltip content="9.4M cache reads · 74% hit ratio" side="top">
        <Badge tone="accent" dot>claude-sonnet-5</Badge>
      </Tooltip>
    </ForceHover>
  </Canvas>
)

export const Bottom = () => (
  <Canvas style={{ maxWidth: 420, paddingTop: 28, paddingBottom: 64, display: 'flex', justifyContent: 'center' }}>
    <ForceHover>
      <Tooltip content="Refreshed Jul 11, 14:32 · next sync in 4m" side="bottom">
        <Button variant="secondary" size="sm" iconLeft="refresh">Sync sources</Button>
      </Tooltip>
    </ForceHover>
  </Canvas>
)

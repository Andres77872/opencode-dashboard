import { Badge } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10, ...style }}>
      {children}
    </div>
  )
}

export const Tones = () => (
  <Canvas>
    <Badge>claude-sonnet-5</Badge>
    <Badge tone="accent">Default model</Badge>
    <Badge tone="success">Synced</Badge>
    <Badge tone="warning">Quota 82%</Badge>
    <Badge tone="danger">Rate limited</Badge>
  </Canvas>
)

export const WithDot = () => (
  <Canvas>
    <Badge tone="success" dot>Live</Badge>
    <Badge tone="warning" dot>Sync pending</Badge>
    <Badge tone="danger" dot>Collector down</Badge>
    <Badge dot>Idle</Badge>
  </Canvas>
)

export const Solid = () => (
  <Canvas>
    <Badge tone="accent" solid>New</Badge>
    <Badge tone="success" solid>Active</Badge>
    <Badge tone="warning" solid>Degraded</Badge>
    <Badge tone="danger" solid>Failing</Badge>
    <Badge tone="neutral" solid>Archived</Badge>
  </Canvas>
)

export const InListContext = () => (
  <Canvas style={{ display: 'block' }}>
    <div style={{ maxWidth: 420 }}>
      {[
        { name: 'opencode-dashboard', badge: <Badge tone="success" dot>Live</Badge>, meta: '18.4M tokens' },
        { name: 'vael-api', badge: <Badge tone="accent">Default</Badge>, meta: '12.1M tokens' },
        { name: 'argo-ui', badge: <Badge>Archived</Badge>, meta: '3.2M tokens' },
      ].map((r) => (
        <div key={r.name} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 0' }}>
          <span style={{ font: '500 13px/1 var(--font-ui)', color: 'var(--fg-secondary)' }}>{r.name}</span>
          {r.badge}
          <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-muted)', marginLeft: 'auto' }}>{r.meta}</span>
        </div>
      ))}
    </div>
  </Canvas>
)

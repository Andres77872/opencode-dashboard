import { Card, Badge, Button } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12, padding: '7px 0', borderBottom: '1px solid var(--border-subtle)' }}>
      <span style={{ font: '400 13px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>{label}</span>
      <span style={{ font: '500 13px/1.3 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{value}</span>
    </div>
  )
}

export const Header = () => (
  <Canvas style={{ maxWidth: 460 }}>
    <Card
      title="Token usage"
      subtitle="Last 30 days · all sources"
      action={<Button variant="ghost" size="sm" iconLeft="download">Export</Button>}
      style={{ width: 420 }}
    >
      <Row label="Total tokens" value="48.2M" />
      <Row label="Input / output" value="31.6M / 16.6M" />
      <Row label="Cache reads" value="9.4M" />
      <Row label="Est. cost" value="$212.40" />
    </Card>
  </Canvas>
)

export const Eyebrow = () => (
  <Canvas style={{ maxWidth: 460 }}>
    <Card
      eyebrow="Source"
      title="claude_code"
      subtitle="1,284 sessions · 9,312 messages"
      action={<Badge tone="success" dot>Synced</Badge>}
      style={{ width: 420 }}
    >
      <Row label="Top model" value="claude-sonnet-5" />
      <Row label="Top repo" value="opencode-dashboard" />
      <Row label="Top tool" value="edit (4,102 calls)" />
    </Card>
  </Canvas>
)

export const Plain = () => (
  <Canvas style={{ maxWidth: 460 }}>
    <Card style={{ width: 420 }}>
      <div style={{ font: '400 13px/1.6 var(--font-ui)', color: 'var(--fg-secondary)' }}>
        Pricing snapshots are refreshed every 24h from provider price sheets. Costs shown are
        estimates computed at collection time and may lag published rates by up to a day.
      </div>
    </Card>
  </Canvas>
)

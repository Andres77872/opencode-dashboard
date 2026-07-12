import { SectionTitle, Card } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

function MiniCard({ label, value }: { label: string; value: string }) {
  return (
    <Card pad={14}>
      <div style={{ font: '600 11px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)' }}>{label}</div>
      <div style={{ font: '700 22px/1 var(--font-mono)', color: 'var(--fg-primary)', marginTop: 9, fontVariantNumeric: 'tabular-nums' }}>{value}</div>
    </Card>
  )
}

export const WithSub = () => (
  <Canvas style={{ maxWidth: 480 }}>
    <SectionTitle sub="Last 7 days · all sources">Model usage</SectionTitle>
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
      <MiniCard label="claude-sonnet-5" value="31.6M" />
      <MiniCard label="gpt-5.6-codex" value="12.9M" />
    </div>
  </Canvas>
)

export const Plain = () => (
  <Canvas style={{ maxWidth: 480 }}>
    <SectionTitle>Sessions</SectionTitle>
    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
      <MiniCard label="Sessions" value="1,284" />
      <MiniCard label="Messages" value="9,312" />
    </div>
  </Canvas>
)

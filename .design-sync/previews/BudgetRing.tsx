import { BudgetRing } from 'web'
import type { ReactNode } from 'react'

function Canvas({ children }: { children?: ReactNode }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'inline-block' }}>
      {children}
    </div>
  )
}

export const Default = () => (
  <Canvas>
    <BudgetRing pct={42} size={132} thickness={12} tone="var(--accent)" label="of budget" />
  </Canvas>
)

export const Thresholds = () => (
  <Canvas>
    <div style={{ display: 'flex', gap: 32 }}>
      {[
        { pct: 42, tone: 'var(--accent)', caption: 'Anthropic · $84 of $200' },
        { pct: 87, tone: 'var(--warning)', caption: 'OpenAI · $130 of $150' },
        { pct: 104, tone: 'var(--danger)', caption: 'MiniMax · $52 of $50' },
      ].map((r) => (
        <div key={r.caption} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 12 }}>
          <BudgetRing pct={r.pct} size={132} thickness={12} tone={r.tone} label="of budget" />
          <div style={{ font: '400 11px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{r.caption}</div>
        </div>
      ))}
    </div>
  </Canvas>
)

export const Small = () => (
  <Canvas>
    <div style={{ display: 'flex', gap: 24, alignItems: 'center' }}>
      <BudgetRing pct={64} size={88} thickness={8} tone="var(--accent)" label="July" />
      <BudgetRing pct={91} size={88} thickness={8} tone="var(--warning)" label="Q3" />
    </div>
  </Canvas>
)

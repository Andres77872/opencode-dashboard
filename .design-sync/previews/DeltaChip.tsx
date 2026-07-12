import { DeltaChip } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 16, ...style }}>
      {children}
    </div>
  )
}

export const Directions = () => (
  <Canvas>
    <DeltaChip value="12.4%" dir="up" />
    <DeltaChip value="4.1%" dir="down" />
    <DeltaChip value="0.8%" dir="flat" />
  </Canvas>
)

export const ToneOverrides = () => (
  <Canvas>
    {/* cost went down — that's good */}
    <DeltaChip value="$18.20" dir="down" tone="pos" />
    {/* tokens went up but it's a spend alarm */}
    <DeltaChip value="9.6M" dir="up" tone="neg" />
    <DeltaChip value="2 sessions" dir="up" tone="neutral" mono={false} />
  </Canvas>
)

export const InStatContext = () => (
  <Canvas style={{ display: 'block' }}>
    <div style={{ maxWidth: 240 }}>
      <div style={{ font: '500 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>Total tokens</div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, marginTop: 8 }}>
        <span style={{ font: '600 24px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>48.2M</span>
        <DeltaChip value="12.4%" dir="up" />
      </div>
      <div style={{ font: '400 11px/1 var(--font-ui)', color: 'var(--fg-faint)', marginTop: 8 }}>vs previous 30 days</div>
    </div>
  </Canvas>
)

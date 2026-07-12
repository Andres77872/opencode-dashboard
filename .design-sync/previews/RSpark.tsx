import { RSpark } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

const tokens14 = [3.1, 4.4, 3.8, 5.2, 4.9, 6.1, 5.7, 7.3, 6.6, 8.1, 7.4, 8.8, 8.2, 9.6]
const cost14 = [12.4, 11.1, 11.8, 10.2, 9.4, 9.9, 8.7, 8.1, 8.6, 7.4, 7.9, 6.8, 6.2, 5.5]
const days = ['Jun 27', 'Jun 28', 'Jun 29', 'Jun 30', 'Jul 1', 'Jul 2', 'Jul 3', 'Jul 4', 'Jul 5', 'Jul 6', 'Jul 7', 'Jul 8', 'Jul 9', 'Jul 10']

export const Basic = () => (
  <Canvas style={{ maxWidth: 440 }}>
    <RSpark data={tokens14} />
  </Canvas>
)

export const TonesAndHeights = () => (
  <Canvas style={{ maxWidth: 440, display: 'grid', gap: 16 }}>
    <RSpark data={tokens14} tone="var(--success)" height={24} />
    <RSpark data={cost14} tone="var(--danger)" height={36} />
    <RSpark data={tokens14.map((v, i) => v + cost14[i])} tone="var(--cat-6)" height={48} />
  </Canvas>
)

export const InCardHeader = () => (
  <Canvas style={{ maxWidth: 400 }}>
    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
      <span style={{ font: '500 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>Tokens per day</span>
      <span style={{ font: '600 14px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>9.6M</span>
    </div>
    <div style={{ marginTop: 12 }}>
      {/* labels enable the hover crosshair + tooltip (interactive only; static capture shows the line) */}
      <RSpark data={tokens14} height={40} labels={days} fmt={(v) => `${v.toFixed(1)}M tokens`} />
    </div>
    <div style={{ display: 'flex', justifyContent: 'space-between', font: '400 10px/1 var(--font-ui)', color: 'var(--fg-faint)', marginTop: 6 }}>
      <span>Jun 27</span>
      <span>Jul 10</span>
    </div>
  </Canvas>
)

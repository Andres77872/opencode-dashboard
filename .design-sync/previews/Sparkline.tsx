import { Sparkline } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 20, ...style }}>
      {children}
    </div>
  )
}

const tokens = [42.1, 55.7, 48.2, 61.9, 58.4, 71.2, 68.5, 79.3, 74.1, 88.6]
const cost = [8.2, 7.4, 7.9, 6.8, 6.1, 6.4, 5.7, 5.2, 5.5, 4.9]
const flatish = [22, 24, 23, 25, 24, 23, 25, 24, 26, 25]

export const Basic = () => (
  <Canvas style={{ maxWidth: 'fit-content' }}>
    <Sparkline data={tokens} />
    <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>88.6M</span>
  </Canvas>
)

export const Tones = () => (
  <Canvas>
    <Sparkline data={tokens} tone="var(--success)" />
    <Sparkline data={cost} tone="var(--danger)" />
    <Sparkline data={flatish} tone="var(--cat-4)" />
  </Canvas>
)

export const SizesAndNoFill = () => (
  <Canvas>
    <Sparkline data={tokens} width={64} height={20} />
    <Sparkline data={tokens} width={96} height={28} />
    <Sparkline data={tokens} width={160} height={40} />
    <Sparkline data={tokens} width={96} height={28} fill={false} />
  </Canvas>
)

export const InTableCells = () => (
  <Canvas style={{ display: 'block' }}>
    <div style={{ maxWidth: 440 }}>
      {[
        { name: 'claude-sonnet-5', data: tokens, tone: 'var(--cat-1)', v: '31.8M' },
        { name: 'gpt-5.6-codex', data: cost.map((x) => 10 - x), tone: 'var(--cat-2)', v: '12.9M' },
        { name: 'claude-haiku-4.5', data: flatish, tone: 'var(--cat-3)', v: '3.5M' },
      ].map((r) => (
        <div key={r.name} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '7px 0' }}>
          <span style={{ font: '500 13px/1 var(--font-ui)', color: 'var(--fg-secondary)', width: 150 }}>{r.name}</span>
          <Sparkline data={r.data} tone={r.tone} width={110} height={24} />
          <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', marginLeft: 'auto', fontVariantNumeric: 'tabular-nums' }}>{r.v}</span>
        </div>
      ))}
    </div>
  </Canvas>
)

import { SegmentedControl } from 'web'
import type { ReactNode, CSSProperties } from 'react'

/* Vael is dark-first — every cell renders on the app's near-black canvas. */
function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, ...style }}>
      {children}
    </div>
  )
}

export const MetricSwitcher = () => (
  <Canvas>
    <SegmentedControl
      options={[
        { value: 'tokens', label: 'Tokens' },
        { value: 'cost', label: 'Cost' },
        { value: 'messages', label: 'Messages' },
      ]}
      value="tokens"
      size="md"
    />
  </Canvas>
)

export const RangeSwitcher = () => (
  <Canvas>
    <SegmentedControl options={['7d', '30d', '90d']} value="30d" size="sm" />
    <SegmentedControl
      options={[
        { value: 'hourly', label: 'Hourly' },
        { value: 'daily', label: 'Daily' },
        { value: 'weekly', label: 'Weekly' },
      ]}
      value="daily"
      size="sm"
    />
  </Canvas>
)

import { StatCard } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

const week = [42.1, 55.7, 48.2, 61.9, 58.4, 71.2, 68.5]

export const Basic = () => (
  <Canvas style={{ maxWidth: 320 }}>
    <StatCard label="Total tokens" value="48.2M" delta={{ value: '12.4%', dir: 'up' }} hint="vs previous 30 days" />
  </Canvas>
)

export const WithSparkline = () => (
  <Canvas style={{ maxWidth: 320 }}>
    <StatCard
      label="Est. cost"
      value="$212.40"
      delta={{ value: '4.1%', dir: 'down', tone: 'pos' }}
      hint="vs previous 30 days"
      spark={week}
      sparkLabels={['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']}
      sparkFmt={(v) => `$${v.toFixed(2)}`}
    />
  </Canvas>
)

export const Accent = () => (
  <Canvas style={{ maxWidth: 320 }}>
    <StatCard label="Sessions" value="1,284" unit="runs" accent delta={{ value: '8.9%', dir: 'up' }} spark={[12, 18, 14, 22, 26, 24, 31]} />
  </Canvas>
)

export const KpiRow = () => (
  <Canvas>
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(180px, 1fr))', gap: 12 }}>
      <StatCard label="Total tokens" value="48.2M" delta={{ value: '12.4%', dir: 'up' }} />
      <StatCard label="Est. cost" value="$212.40" delta={{ value: '4.1%', dir: 'down', tone: 'pos' }} />
      <StatCard label="Messages" value="9,312" delta={{ value: '0.8%', dir: 'flat' }} />
    </div>
  </Canvas>
)

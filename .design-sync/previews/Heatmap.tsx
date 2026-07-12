import { Heatmap } from 'web'
import type { ReactNode } from 'react'

function Canvas({ children, width }: { children?: ReactNode; width?: number }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, width }}>
      {children}
    </div>
  )
}

const hourly = [
  2, 1, 0, 0, 1, 3, 8, 14, 22, 31, 38, 34, 26, 29, 36, 42, 39, 28, 18, 12, 9, 6, 4, 3,
].map((value, i) => ({ key: `${String(i).padStart(2, '0')}:00`, value }))

export const HourlyActivity = () => (
  <Canvas width={500}>
    <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginBottom: 10 }}>
      Messages by hour · last 7 days
    </div>
    <Heatmap cells={hourly} color="var(--accent)" />
    <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 6 }}>
      {['00:00', '06:00', '12:00', '18:00', '23:00'].map((t) => (
        <span key={t} style={{ font: '400 10px/1 var(--font-mono)', color: 'var(--fg-faint)' }}>{t}</span>
      ))}
    </div>
  </Canvas>
)

const weekdays = [
  { key: 'Mon', value: 6.8 },
  { key: 'Tue', value: 8.4 },
  { key: 'Wed', value: 7.9 },
  { key: 'Thu', value: 9.6 },
  { key: 'Fri', value: 8.8 },
  { key: 'Sat', value: 3.1 },
  { key: 'Sun', value: 2.2 },
]

export const ByWeekday = () => (
  <Canvas width={340}>
    <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginBottom: 10 }}>
      Tokens by weekday (M)
    </div>
    <Heatmap cells={weekdays} color="var(--vendor-claude)" />
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 3, marginTop: 6 }}>
      {weekdays.map((d) => (
        <span key={d.key} style={{ font: '400 10px/1 var(--font-mono)', color: 'var(--fg-faint)', textAlign: 'center' }}>{d.key}</span>
      ))}
    </div>
  </Canvas>
)

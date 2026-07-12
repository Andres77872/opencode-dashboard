import { AreaChart, Legend } from 'web'
import type { ReactNode } from 'react'

function Canvas({ children }: { children?: ReactNode }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'inline-block' }}>
      {children}
    </div>
  )
}

const days14 = ['Jun 28', 'Jun 29', 'Jun 30', 'Jul 1', 'Jul 2', 'Jul 3', 'Jul 4', 'Jul 5', 'Jul 6', 'Jul 7', 'Jul 8', 'Jul 9', 'Jul 10', 'Jul 11']

const M = 1e6
const fmtTok = (v: number) => (v >= M ? `${(v / M).toFixed(1)}M` : v >= 1e3 ? `${Math.round(v / 1e3)}K` : String(Math.round(v)))

const opencode = [1.8, 2.1, 1.6, 2.4, 2.9, 1.2, 0.9, 2.6, 3.1, 2.8, 3.4, 2.2, 1.7, 2.9].map((v) => v * M)
const claudeCode = [1.1, 1.4, 1.2, 1.8, 2.0, 0.7, 0.5, 1.6, 2.2, 1.9, 2.4, 1.5, 1.1, 2.0].map((v) => v * M)
const codex = [0.4, 0.6, 0.5, 0.9, 1.1, 0.3, 0.2, 0.8, 1.2, 1.0, 1.4, 0.7, 0.5, 1.1].map((v) => v * M)

export const TokensBySource = () => (
  <Canvas>
    <AreaChart
      labels={days14}
      series={[
        { name: 'OpenCode', color: 'var(--vendor-opencode)', data: opencode, fmt: fmtTok },
        { name: 'Claude Code', color: 'var(--vendor-claude)', data: claudeCode, fmt: fmtTok },
        { name: 'Codex', color: 'var(--vendor-codex)', data: codex, fmt: fmtTok },
      ]}
      width={600}
      height={220}
      yFormat={fmtTok}
    />
    <div style={{ marginTop: 12 }}>
      <Legend
        items={[
          { label: 'OpenCode', color: 'var(--vendor-opencode)', value: '31.6M' },
          { label: 'Claude Code', color: 'var(--vendor-claude)', value: '21.4M' },
          { label: 'Codex', color: 'var(--vendor-codex)', value: '10.6M' },
        ]}
      />
    </div>
  </Canvas>
)

export const DailyCost = () => (
  <Canvas>
    <AreaChart
      labels={days14}
      series={[
        {
          name: 'Est. cost',
          color: 'var(--accent)',
          data: [6.4, 7.8, 5.9, 8.6, 10.2, 4.1, 3.2, 9.4, 11.8, 10.6, 12.4, 8.1, 6.2, 10.9],
          fmt: (v) => `$${v.toFixed(2)}`,
        },
      ]}
      width={600}
      height={220}
      yFormat={(v) => `$${v.toFixed(0)}`}
    />
  </Canvas>
)

export const Compact = () => (
  <Canvas>
    <AreaChart
      labels={['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']}
      series={[
        { name: 'Sessions', color: 'var(--cat-3)', data: [48, 61, 56, 72, 53, 44, 87], fmt: (v) => String(Math.round(v)) },
      ]}
      width={340}
      height={150}
      yFormat={(v) => String(Math.round(v))}
    />
  </Canvas>
)

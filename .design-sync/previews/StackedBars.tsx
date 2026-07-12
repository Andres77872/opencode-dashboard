import { StackedBars, Legend } from 'web'
import type { ReactNode } from 'react'

function Canvas({ children }: { children?: ReactNode }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'inline-block' }}>
      {children}
    </div>
  )
}

const sourceKeys = [
  { id: 'opencode', short: 'OpenCode', color: 'var(--vendor-opencode)' },
  { id: 'claude_code', short: 'Claude Code', color: 'var(--vendor-claude)' },
  { id: 'codex', short: 'Codex', color: 'var(--vendor-codex)' },
]

const M = 1e6
const fmtTok = (v: number) => (v >= M ? `${(v / M).toFixed(1)}M` : v >= 1e3 ? `${Math.round(v / 1e3)}K` : String(Math.round(v)))

const wds = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const tokenDays = [
  ['Jun 28', 1.8, 1.1, 0.4],
  ['Jun 29', 2.1, 1.4, 0.6],
  ['Jun 30', 1.6, 1.2, 0.5],
  ['Jul 1', 2.4, 1.8, 0.9],
  ['Jul 2', 2.9, 2.0, 1.1],
  ['Jul 3', 1.2, 0.7, 0.3],
  ['Jul 4', 0.9, 0.5, 0.2],
  ['Jul 5', 2.6, 1.6, 0.8],
  ['Jul 6', 3.1, 2.2, 1.2],
  ['Jul 7', 2.8, 1.9, 1.0],
  ['Jul 8', 3.4, 2.4, 1.4],
  ['Jul 9', 2.2, 1.5, 0.7],
  ['Jul 10', 1.7, 1.1, 0.5],
  ['Jul 11', 2.9, 2.0, 1.1],
].map(([key, oc, cc, cx], i) => ({
  key: key as string,
  wd: wds[(i + 0) % 7],
  per: { opencode: (oc as number) * M, claude_code: (cc as number) * M, codex: (cx as number) * M },
}))

export const TokensByDay = () => (
  <Canvas>
    <StackedBars days={tokenDays} keys={sourceKeys} width={600} height={220} valueFmt={fmtTok} label="Tokens" showTotal />
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

const sessionDays = [
  ['Jul 5', 'Sat', 14, 9, 5],
  ['Jul 6', 'Sun', 21, 13, 7],
  ['Jul 7', 'Mon', 34, 22, 11],
  ['Jul 8', 'Tue', 40, 26, 14],
  ['Jul 9', 'Wed', 30, 19, 10],
  ['Jul 10', 'Thu', 25, 16, 8],
  ['Jul 11', 'Fri', 36, 23, 12],
].map(([key, wd, oc, cc, cx]) => ({
  key: key as string,
  wd: wd as string,
  per: { opencode: oc as number, claude_code: cc as number, codex: cx as number },
}))

export const SessionsWeek = () => (
  <Canvas>
    <StackedBars days={sessionDays} keys={sourceKeys} width={480} height={180} valueFmt={(v) => String(Math.round(v))} label="Sessions" showTotal={false} />
  </Canvas>
)

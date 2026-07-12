import { SourceStack, Legend } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

const bySource = [
  { id: 'claude_code', label: 'Claude Code', color: 'var(--vendor-claude)', value: 21600000 },
  { id: 'opencode', label: 'OpenCode', color: 'var(--vendor-opencode)', value: 18400000 },
  { id: 'codex', label: 'Codex', color: 'var(--vendor-codex)', value: 8200000 },
]

export const Basic = () => (
  <Canvas style={{ display: 'flex', flexDirection: 'column', gap: 14, maxWidth: 'fit-content' }}>
    <SourceStack sources={bySource} />
    <SourceStack sources={bySource} width={200} height={9} />
    <SourceStack sources={bySource} width={200} height={5} />
  </Canvas>
)

export const FullWidthWithLegend = () => (
  <Canvas style={{ maxWidth: 420 }}>
    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
      <span style={{ font: '500 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>Tokens by source</span>
      <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>48.2M</span>
    </div>
    <div style={{ marginTop: 10 }}>
      <SourceStack sources={bySource} width="100%" height={9} />
    </div>
    <div style={{ marginTop: 12 }}>
      <Legend
        items={[
          { label: 'Claude Code', color: 'var(--vendor-claude)', value: '45%' },
          { label: 'OpenCode', color: 'var(--vendor-opencode)', value: '38%' },
          { label: 'Codex', color: 'var(--vendor-codex)', value: '17%' },
        ]}
      />
    </div>
  </Canvas>
)

export const InTableRows = () => (
  <Canvas style={{ maxWidth: 440 }}>
    {[
      { name: 'opencode-dashboard', tokens: '18.4M', mix: [{ id: 'oc', color: 'var(--vendor-opencode)', value: 71 }, { id: 'cc', color: 'var(--vendor-claude)', value: 29 }] },
      { name: 'vael-api', tokens: '12.1M', mix: [{ id: 'cc', color: 'var(--vendor-claude)', value: 64 }, { id: 'cx', color: 'var(--vendor-codex)', value: 36 }] },
      { name: 'argo-ui', tokens: '3.2M', mix: [{ id: 'oc', color: 'var(--vendor-opencode)', value: 22 }, { id: 'cc', color: 'var(--vendor-claude)', value: 48 }, { id: 'cx', color: 'var(--vendor-codex)', value: 30 }] },
    ].map((r) => (
      <div key={r.name} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '8px 0' }}>
        <span style={{ font: '500 13px/1 var(--font-ui)', color: 'var(--fg-secondary)', width: 160 }}>{r.name}</span>
        <SourceStack sources={r.mix} width={110} />
        <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-muted)', marginLeft: 'auto', fontVariantNumeric: 'tabular-nums' }}>{r.tokens}</span>
      </div>
    ))}
  </Canvas>
)

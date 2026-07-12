import { Notice } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

export const Tones = () => (
  <Canvas style={{ maxWidth: 560 }}>
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, width: 520 }}>
      <Notice tone="info" title="Pricing snapshot updated">
        Anthropic rates refreshed Jul 11, 06:00 — Sonnet 5 output now $18.00 / 1M tokens.
      </Notice>
      <Notice tone="warning" icon="clock" title="Pricing snapshot stale">
        Codex prices were last fetched 3 days ago. Costs for gpt-5.6-codex may be off.
      </Notice>
      <Notice tone="danger" icon="database" title="codex source unreachable">
        No response since 09:12 — 4 sync attempts failed. Sessions after that time are missing.
      </Notice>
      <Notice tone="success" title="Backfill complete">
        Re-collected 1,284 sessions from claude_code after the dataVersion migration.
      </Notice>
    </div>
  </Canvas>
)

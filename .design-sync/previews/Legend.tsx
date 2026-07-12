import { Legend } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

export const Sources = () => (
  <Canvas style={{ maxWidth: 440 }}>
    <Legend
      items={[
        { label: 'Claude Code', color: 'var(--vendor-claude)', value: '21.6M' },
        { label: 'OpenCode', color: 'var(--vendor-opencode)', value: '18.4M' },
        { label: 'Codex', color: 'var(--vendor-codex)', value: '8.2M' },
      ]}
    />
  </Canvas>
)

export const Models = () => (
  <Canvas style={{ maxWidth: 440 }}>
    <Legend
      items={[
        { label: 'claude-sonnet-5', color: 'var(--cat-1)' },
        { label: 'gpt-5.6-codex', color: 'var(--cat-2)' },
        { label: 'claude-haiku-4.5', color: 'var(--cat-3)' },
        { label: 'Other', color: 'var(--cat-8)' },
      ]}
    />
  </Canvas>
)

export const UnderChart = () => (
  <Canvas style={{ maxWidth: 440 }}>
    <div style={{ font: '600 13px/1 var(--font-ui)', color: 'var(--fg-primary)' }}>Tool calls by kind</div>
    <div style={{ display: 'flex', alignItems: 'flex-end', gap: 6, height: 64, marginTop: 14 }}>
      {[
        { h: 58, c: 'var(--cat-1)' },
        { h: 40, c: 'var(--cat-2)' },
        { h: 30, c: 'var(--cat-3)' },
        { h: 16, c: 'var(--cat-4)' },
      ].map((b, i) => (
        <div key={i} style={{ width: 42, height: b.h, background: b.c, borderRadius: '3px 3px 0 0', opacity: 0.9 }} />
      ))}
    </div>
    <div style={{ borderTop: '1px solid var(--ink-700)', marginTop: 0, paddingTop: 12 }}>
      <Legend
        items={[
          { label: 'read', color: 'var(--cat-1)', value: '4,812' },
          { label: 'edit', color: 'var(--cat-2)', value: '3,309' },
          { label: 'bash', color: 'var(--cat-3)', value: '2,488' },
          { label: 'grep', color: 'var(--cat-4)', value: '1,364' },
        ]}
      />
    </div>
  </Canvas>
)

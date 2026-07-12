import { Donut, Legend } from 'web'
import type { ReactNode } from 'react'

function Canvas({ children }: { children?: ReactNode }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'inline-block' }}>
      {children}
    </div>
  )
}

export const CostBySource = () => (
  <Canvas>
    <div style={{ display: 'flex', alignItems: 'center', gap: 28 }}>
      <Donut
        segments={[
          { value: 96.1, color: 'var(--vendor-opencode)', label: 'OpenCode', valueText: '$96.10', shareText: '45.2%' },
          { value: 78.4, color: 'var(--vendor-claude)', label: 'Claude Code', valueText: '$78.40', shareText: '36.9%' },
          { value: 37.9, color: 'var(--vendor-codex)', label: 'Codex', valueText: '$37.90', shareText: '17.8%' },
        ]}
        size={150}
        thickness={16}
        centerTop="$212.40"
        centerBottom="Est. cost"
      />
      <Legend
        items={[
          { label: 'OpenCode', color: 'var(--vendor-opencode)', value: '$96.10' },
          { label: 'Claude Code', color: 'var(--vendor-claude)', value: '$78.40' },
          { label: 'Codex', color: 'var(--vendor-codex)', value: '$37.90' },
        ]}
      />
    </div>
  </Canvas>
)

export const TokensByModel = () => (
  <Canvas>
    <div style={{ display: 'flex', alignItems: 'center', gap: 28 }}>
      <Donut
        segments={[
          { value: 24.6, color: 'var(--cat-1)', label: 'claude-sonnet-5', valueText: '24.6M' },
          { value: 12.4, color: 'var(--cat-2)', label: 'gpt-5.6-codex', valueText: '12.4M' },
          { value: 7.1, color: 'var(--cat-3)', label: 'claude-haiku-4.5', valueText: '7.1M' },
          { value: 4.1, color: 'var(--cat-4)', label: 'minimax-m2', valueText: '4.1M' },
        ]}
        size={150}
        thickness={16}
        centerTop="48.2M"
        centerBottom="Tokens"
      />
      <Legend
        items={[
          { label: 'claude-sonnet-5', color: 'var(--cat-1)', value: '24.6M' },
          { label: 'gpt-5.6-codex', color: 'var(--cat-2)', value: '12.4M' },
          { label: 'claude-haiku-4.5', color: 'var(--cat-3)', value: '7.1M' },
          { label: 'minimax-m2', color: 'var(--cat-4)', value: '4.1M' },
        ]}
      />
    </div>
  </Canvas>
)

export const Thin = () => (
  <Canvas>
    <Donut
      segments={[
        { value: 5812, color: 'var(--cat-5)', label: 'read', valueText: '5,812' },
        { value: 3247, color: 'var(--cat-6)', label: 'edit', valueText: '3,247' },
        { value: 2108, color: 'var(--cat-7)', label: 'bash', valueText: '2,108' },
        { value: 1466, color: 'var(--cat-8)', label: 'grep', valueText: '1,466' },
      ]}
      size={120}
      thickness={8}
      centerTop="12.6K"
      centerBottom="Tool calls"
    />
  </Canvas>
)

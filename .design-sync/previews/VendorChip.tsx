import { VendorChip } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 20, ...style }}>
      {children}
    </div>
  )
}

export const AllVendors = () => (
  <Canvas>
    <VendorChip id="opencode" />
    <VendorChip id="claude_code" />
    <VendorChip id="codex" />
  </Canvas>
)

export const MonogramOnly = () => (
  <Canvas style={{ gap: 10 }}>
    <VendorChip id="opencode" label={false} />
    <VendorChip id="claude_code" label={false} />
    <VendorChip id="codex" label={false} />
  </Canvas>
)

export const Sizes = () => (
  <Canvas>
    <VendorChip id="claude_code" size={16} />
    <VendorChip id="claude_code" size={22} />
    <VendorChip id="claude_code" size={28} />
  </Canvas>
)

export const InSourceList = () => (
  <Canvas style={{ display: 'block' }}>
    <div style={{ maxWidth: 380 }}>
      {[
        { id: 'claude_code' as const, tokens: '21.6M', cost: '$118.40' },
        { id: 'opencode' as const, tokens: '18.4M', cost: '$64.90' },
        { id: 'codex' as const, tokens: '8.2M', cost: '$29.10' },
      ].map((r) => (
        <div key={r.id} style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '8px 0' }}>
          <VendorChip id={r.id} />
          <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', marginLeft: 'auto', fontVariantNumeric: 'tabular-nums' }}>{r.tokens}</span>
          <span style={{ font: '500 12px/1 var(--font-mono)', color: 'var(--fg-muted)', width: 64, textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>{r.cost}</span>
        </div>
      ))}
    </div>
  </Canvas>
)

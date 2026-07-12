import { Drawer, Badge, DeltaChip } from 'web'
import type { ReactNode } from 'react'

function Row({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 12, padding: '8px 0', borderBottom: '1px solid var(--border-subtle)' }}>
      <span style={{ font: '400 13px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>{label}</span>
      <span style={{ font: '500 13px/1.3 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{value}</span>
    </div>
  )
}

export const Open = () => (
  <Drawer open onClose={() => {}} title="Session a81f-42c9" subtitle="opencode · vael-api · Jul 11, 14:32" width={560}>
    <div style={{ display: 'flex', gap: 8, marginBottom: 14 }}>
      <Badge tone="accent">claude-sonnet-5</Badge>
      <Badge tone="neutral">42 messages</Badge>
      <Badge tone="success" dot>Completed</Badge>
    </div>
    <Row label="Total tokens" value="612.4K" />
    <Row label="Input / output" value="418.2K / 194.2K" />
    <Row label="Cache reads" value="1.9M" />
    <Row label="Est. cost" value={<span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>$4.87 <DeltaChip value="18%" dir="up" tone="neg" /></span>} />
    <Row label="Duration" value="26m 41s" />
    <Row label="Tool calls" value="156 (read 84 · edit 47 · bash 25)" />
    <div style={{ marginTop: 18 }}>
      <div style={{ font: '600 11px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginBottom: 8 }}>First prompt</div>
      <div style={{ font: '400 13px/1.6 var(--font-ui)', color: 'var(--fg-secondary)', background: 'var(--ink-800)', border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-lg)', padding: '12px 14px' }}>
        Add live quota tracking for providers (Codex, Claude, MiniMax), including a sidebar strip
        and detailed overview cards. Wire the new endpoints into the web registry and cover the
        sync behavior with unit tests.
      </div>
    </div>
    <div style={{ marginTop: 14 }}>
      <div style={{ font: '600 11px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginBottom: 8 }}>Notes</div>
      <div style={{ font: '400 13px/1.6 var(--font-ui)', color: 'var(--fg-secondary)' }}>
        Cache hit ratio stayed above 74% for the whole run. Cost spike at 14:41 traces to a full
        re-read of the pricing snapshot after the schema migration touched dataVersion.
      </div>
    </div>
  </Drawer>
)

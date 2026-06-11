/* Vael Card / StatCard / SectionTitle. Ported from the Vael ui_kit. */
import type { CSSProperties, ReactNode } from 'react'
import { DeltaChip, RSpark, type DeltaChipProps } from './atoms'

export interface CardProps {
  title?: ReactNode
  subtitle?: ReactNode
  action?: ReactNode
  eyebrow?: ReactNode
  children?: ReactNode
  pad?: number
  style?: CSSProperties
  bodyStyle?: CSSProperties
}

export function Card({ title, subtitle, action, children, pad = 16, style, bodyStyle, eyebrow }: CardProps) {
  return (
    <section
      style={{
        background: 'var(--ink-800)',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-xl)',
        display: 'flex',
        flexDirection: 'column',
        minWidth: 0,
        ...style,
      }}
    >
      {(title || action) && (
        <header
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 12,
            rowGap: 8,
            flexWrap: 'wrap',
            padding: '14px 16px 12px',
            borderBottom: '1px solid var(--border-subtle)',
          }}
        >
          <div style={{ minWidth: 0, flexShrink: 0 }}>
            {eyebrow && (
              <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginBottom: 6 }}>{eyebrow}</div>
            )}
            <div style={{ font: '600 15px/1.2 var(--font-ui)', color: 'var(--fg-primary)', whiteSpace: 'nowrap' }}>{title}</div>
            {subtitle && <div style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)', marginTop: 3 }}>{subtitle}</div>}
          </div>
          {action}
        </header>
      )}
      <div style={{ padding: pad, flex: 1, minWidth: 0, ...bodyStyle }}>{children}</div>
    </section>
  )
}

export interface StatCardProps {
  label: ReactNode
  value: ReactNode
  unit?: ReactNode
  delta?: DeltaChipProps
  spark?: number[]
  sparkTone?: string
  sparkLabels?: string[]
  sparkFmt?: (v: number) => string
  hint?: ReactNode
  accent?: boolean
  title?: string
}

export function StatCard({ label, value, unit, delta, spark, sparkTone = 'var(--accent)', sparkLabels, sparkFmt, hint, accent = false, title }: StatCardProps) {
  return (
    <div
      title={title}
      style={{
        position: 'relative',
        background: 'var(--ink-800)',
        border: `1px solid ${accent ? 'var(--border-accent)' : 'var(--border-default)'}`,
        borderRadius: 'var(--radius-lg)',
        padding: '14px 16px',
        overflow: 'hidden',
        boxShadow: accent ? 'var(--glow-accent)' : 'none',
      }}
    >
      {accent && <div style={{ position: 'absolute', inset: 0, background: 'radial-gradient(220px 120px at 85% -10%, rgba(77,159,255,0.10), transparent 70%)', pointerEvents: 'none' }} />}
      <div style={{ position: 'relative' }}>
        <div style={{ font: '600 11px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{label}</div>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 5, marginTop: 11 }}>
          <span style={{ font: '700 29px/1 var(--font-mono)', letterSpacing: '-0.02em', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{value}</span>
          {unit && <span style={{ font: '500 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{unit}</span>}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 10, minHeight: 14, overflow: 'hidden' }}>
          {delta && <DeltaChip {...delta} />}
          {hint && <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-faint)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', minWidth: 0 }}>{hint}</span>}
        </div>
        {spark && spark.length > 0 && (
          <div style={{ marginTop: 12 }}>
            <RSpark data={spark} tone={sparkTone} height={30} labels={sparkLabels} fmt={sparkFmt} />
          </div>
        )}
      </div>
    </div>
  )
}

export function SectionTitle({ children, sub }: { children: ReactNode; sub?: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, marginBottom: 12 }}>
      <h2 style={{ margin: 0, font: '600 15px/1.2 var(--font-ui)', color: 'var(--fg-primary)' }}>{children}</h2>
      {sub && <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{sub}</span>}
    </div>
  )
}

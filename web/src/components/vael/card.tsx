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
  /** Edge-to-edge body (no padding) clipped to the card's corners — for a table or list that fills the card. */
  flush?: boolean
  style?: CSSProperties
  bodyStyle?: CSSProperties
}

/** Surface container with optional header (eyebrow/title/subtitle/action) and padded body. @category Cards */
export function Card({ title, subtitle, action, children, pad = 16, flush = false, style, bodyStyle, eyebrow }: CardProps) {
  return (
    <section
      style={{
        background: 'var(--ink-800)',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-xl)',
        display: 'flex',
        flexDirection: 'column',
        minWidth: 0,
        overflow: flush ? 'hidden' : undefined,
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
            rowGap: 10,
            flexWrap: 'wrap',
            padding: '14px 16px 12px',
            borderBottom: '1px solid var(--border-subtle)',
          }}
        >
          {/* The text block yields before the action does: it may shrink to a
              readable minimum, after which the action wraps to its own row. A
              rigid block here pushed long subtitles straight out of the card. */}
          <div style={{ minWidth: 0, flex: '1 1 160px' }}>
            {eyebrow && (
              <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginBottom: 6 }}>{eyebrow}</div>
            )}
            <div style={{ font: '600 15px/1.2 var(--font-ui)', color: 'var(--fg-primary)', overflowWrap: 'anywhere' }}>{title}</div>
            {subtitle && <div style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)', marginTop: 3 }}>{subtitle}</div>}
          </div>
          {action && <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', minWidth: 0, maxWidth: '100%' }}>{action}</div>}
        </header>
      )}
      <div style={{ padding: flush ? 0 : pad, flex: 1, minWidth: 0, ...bodyStyle }}>{children}</div>
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

/** KPI tile — uppercase label, big mono value, optional delta chip, hint, and sparkline; accent adds a glow. @category Cards */
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
      {accent && <div style={{ position: 'absolute', inset: 0, background: 'radial-gradient(220px 120px at 85% -10%, var(--accent-soft), transparent 70%)', pointerEvents: 'none' }} />}
      <div style={{ position: 'relative' }}>
        <div style={{ font: '600 11px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{label}</div>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 5, marginTop: 11 }}>
          {/* Fluid size: two tiles share a phone-width row, so the figure
              scales down there instead of clipping. */}
          <span style={{ font: '700 29px/1.1 var(--font-mono)', fontSize: 'clamp(21px, 5.4vw, 29px)', letterSpacing: '-0.02em', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums', minWidth: 0, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{value}</span>
          {unit && <span style={{ font: '500 13px/1 var(--font-ui)', color: 'var(--fg-muted)', flexShrink: 0 }}>{unit}</span>}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 10, minHeight: 14, overflow: 'hidden' }}>
          {delta && <DeltaChip {...delta} />}
          {hint && <span title={typeof hint === 'string' ? hint : undefined} style={{ font: '400 12px/1.2 var(--font-ui)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', minWidth: 0 }}>{hint}</span>}
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

/** Section heading with optional muted sub-note and trailing controls, for grouping cards on a view. @category Cards */
export function SectionTitle({ children, sub, action }: { children: ReactNode; sub?: ReactNode; action?: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, rowGap: 8, flexWrap: 'wrap', marginBottom: 12 }}>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, rowGap: 4, flexWrap: 'wrap', minWidth: 0 }}>
        <h2 style={{ margin: 0, font: '600 15px/1.2 var(--font-ui)', color: 'var(--fg-primary)' }}>{children}</h2>
        {sub && <span style={{ font: '400 12px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>{sub}</span>}
      </div>
      {action && <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>{action}</div>}
    </div>
  )
}

/** Responsive row of StatCards: as many as fit at desktop widths, two abreast on a phone. @category Cards */
export function StatGrid({ children }: { children: ReactNode }) {
  return <div className="vael-stat-grid">{children}</div>
}

/** Label / value line for compact fact lists; rows are separated by hairlines. @category Cards */
export function KeyValueRow({ label, value, last = false, title }: { label: ReactNode; value: ReactNode; last?: boolean; title?: string }) {
  return (
    <div title={title} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, padding: '10px 0', borderBottom: last ? 'none' : '1px solid var(--border-subtle)' }}>
      <span style={{ font: '400 13px/1.2 var(--font-ui)', color: 'var(--fg-muted)', flexShrink: 0, whiteSpace: 'nowrap' }}>{label}</span>
      <span style={{ font: '600 13px/1.2 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums', textAlign: 'right', minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{value}</span>
    </div>
  )
}

/* Vael feedback states — Skeleton, ViewSkeleton, EmptyState, ErrorState, Notice. */
import type { CSSProperties, ReactNode } from 'react'
import { Icon, type IconName } from './icon'
import { Button } from './controls'

/** Pulsing placeholder block shown while content loads. @category Feedback */
export function Skeleton({ width = '100%', height = 14, radius = 'var(--radius-sm)', style }: { width?: number | string; height?: number | string; radius?: string; style?: CSSProperties }) {
  return (
    <span
      style={{
        display: 'block',
        width,
        height,
        borderRadius: radius,
        background: 'var(--ink-700)',
        animation: 'vael-pulse 1.4s var(--ease-in-out) infinite',
        ...style,
      }}
    />
  )
}

export interface ViewSkeletonProps {
  /** Number of KPI tile placeholders in the top row. 0 omits the row. */
  stats?: number
  /** Heights of the panel placeholders stacked underneath. */
  panels?: number[]
  label?: string
}

/** Whole-view loading placeholder — a row of KPI tiles over one or more panels — so every view loads into the same shape. @category Feedback */
export function ViewSkeleton({ stats = 4, panels = [320], label = 'Loading' }: ViewSkeletonProps) {
  return (
    <div role="status" aria-busy="true" aria-label={label} style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {stats > 0 && (
        <div className="vael-stat-grid">
          {Array.from({ length: stats }).map((_, i) => (
            <div key={i} style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)', padding: '14px 16px' }}>
              <Skeleton width={90} height={11} />
              <Skeleton width={120} height={28} style={{ marginTop: 12 }} />
              <Skeleton width={70} height={10} style={{ marginTop: 12 }} />
            </div>
          ))}
        </div>
      )}
      {panels.map((height, i) => (
        <div key={i} style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height, padding: 16 }}>
          <Skeleton width={140} height={13} />
          <Skeleton width={220} height={10} style={{ marginTop: 10 }} />
        </div>
      ))}
    </div>
  )
}

export interface EmptyStateProps {
  icon?: IconName
  title: ReactNode
  description?: ReactNode
  action?: ReactNode
}

/** Centered empty-state block — icon tile, title, description, optional action. @category Feedback */
export function EmptyState({ icon = 'info', title, description, action }: EmptyStateProps) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', textAlign: 'center', gap: 10, padding: '48px 24px' }}>
      <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 44, height: 44, borderRadius: 'var(--radius-lg)', background: 'var(--ink-750)', border: '1px solid var(--border-subtle)', color: 'var(--fg-muted)' }}>
        <Icon name={icon} size={20} />
      </span>
      <div style={{ font: '600 15px/1.3 var(--font-ui)', color: 'var(--fg-primary)' }}>{title}</div>
      {description && <div style={{ font: '400 13px/1.5 var(--font-ui)', color: 'var(--fg-muted)', maxWidth: 420 }}>{description}</div>}
      {action && <div style={{ marginTop: 4 }}>{action}</div>}
    </div>
  )
}

export interface ErrorStateProps {
  title?: ReactNode
  message?: ReactNode
  onRetry?: () => void
}

/** Centered error block with danger icon, mono error message, and optional retry button. @category Feedback */
export function ErrorState({ title = 'Something went wrong', message, onRetry }: ErrorStateProps) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', textAlign: 'center', gap: 10, padding: '40px 24px' }}>
      <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 44, height: 44, borderRadius: 'var(--radius-lg)', background: 'var(--danger-soft)', border: '1px solid var(--border-subtle)', color: 'var(--danger)' }}>
        <Icon name="alert-triangle" size={20} />
      </span>
      <div style={{ font: '600 15px/1.3 var(--font-ui)', color: 'var(--fg-primary)' }}>{title}</div>
      {message && <div style={{ font: '400 13px/1.5 var(--font-mono)', color: 'var(--fg-muted)', maxWidth: 460 }}>{message}</div>}
      {onRetry && (
        <div style={{ marginTop: 4 }}>
          <Button variant="secondary" size="sm" iconLeft="refresh" onClick={onRetry}>Retry</Button>
        </div>
      )}
    </div>
  )
}

export type NoticeTone = 'info' | 'warning' | 'danger' | 'success'
/** Inline notice banner for warnings and source diagnostics, in info/warning/danger/success tones, with an optional trailing action. @category Feedback */
export function Notice({ tone = 'info', icon, title, children, action }: { tone?: NoticeTone; icon?: IconName; title?: ReactNode; children?: ReactNode; action?: ReactNode }) {
  const map: Record<NoticeTone, { fg: string; soft: string }> = {
    info: { fg: 'var(--blue-300)', soft: 'var(--accent-soft)' },
    warning: { fg: 'var(--warning)', soft: 'var(--warning-soft)' },
    danger: { fg: 'var(--danger)', soft: 'var(--danger-soft)' },
    success: { fg: 'var(--success)', soft: 'var(--success-soft)' },
  }
  const t = map[tone]
  const ico: IconName = icon || (tone === 'warning' || tone === 'danger' ? 'alert-triangle' : tone === 'success' ? 'check' : 'info')
  return (
    <div role={tone === 'danger' ? 'alert' : 'status'} style={{ display: 'flex', alignItems: 'flex-start', gap: 10, padding: '10px 12px', borderRadius: 'var(--radius-lg)', background: t.soft, border: '1px solid var(--border-subtle)' }}>
      <Icon name={ico} size={16} color={t.fg} style={{ marginTop: 1 }} />
      <div style={{ minWidth: 0, flex: 1 }}>
        {title && <div style={{ font: '600 13px/1.3 var(--font-ui)', color: 'var(--fg-primary)' }}>{title}</div>}
        {children && <div style={{ font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-secondary)', marginTop: title ? 3 : 0, overflowWrap: 'anywhere' }}>{children}</div>}
      </div>
      {action && <div style={{ flexShrink: 0 }}>{action}</div>}
    </div>
  )
}

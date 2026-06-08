/* Vael atoms — DeltaChip, Badge, VendorChip, Avatar, Legend, SourceStack,
   BarRow, Sparkline. Ported from the Vael ui_kit (inline styles + CSS vars). */
import type { CSSProperties, ReactNode } from 'react'
import type { SourceID } from '../../types/api'
import { vendorMeta } from './vendors'

export type DeltaDir = 'up' | 'down' | 'flat'
export type DeltaTone = 'pos' | 'neg' | 'neutral'

export interface DeltaChipProps {
  value: string
  dir?: DeltaDir
  tone?: DeltaTone
  mono?: boolean
}

export function DeltaChip({ value, dir = 'flat', tone, mono = true }: DeltaChipProps) {
  const colors: Record<DeltaTone, string> = {
    pos: 'var(--success)',
    neg: 'var(--danger)',
    neutral: 'var(--fg-muted)',
  }
  const t: DeltaTone = tone || (dir === 'up' ? 'pos' : dir === 'down' ? 'neg' : 'neutral')
  const arrow = dir === 'up' ? '↑' : dir === 'down' ? '↓' : '→'
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 3,
        font: `600 12px/1 ${mono ? 'var(--font-mono)' : 'var(--font-ui)'}`,
        color: colors[t],
        fontVariantNumeric: 'tabular-nums',
      }}
    >
      {arrow} {value}
    </span>
  )
}

export type BadgeTone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger'

export interface BadgeProps {
  children: ReactNode
  tone?: BadgeTone
  dot?: boolean
  solid?: boolean
}

export function Badge({ children, tone = 'neutral', dot = false, solid = false }: BadgeProps) {
  const tones: Record<BadgeTone, { fg: string; soft: string; solid: string }> = {
    neutral: { fg: 'var(--fg-secondary)', soft: 'var(--ink-700)', solid: 'var(--ink-600)' },
    accent: { fg: 'var(--blue-300)', soft: 'var(--accent-soft)', solid: 'var(--accent)' },
    success: { fg: 'var(--success)', soft: 'var(--success-soft)', solid: 'var(--success)' },
    warning: { fg: 'var(--warning)', soft: 'var(--warning-soft)', solid: 'var(--warning)' },
    danger: { fg: 'var(--danger)', soft: 'var(--danger-soft)', solid: 'var(--danger)' },
  }
  const t = tones[tone] || tones.neutral
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        height: 20,
        padding: '0 8px',
        font: '600 11px/1 var(--font-ui)',
        color: solid ? 'var(--fg-on-accent)' : t.fg,
        background: solid ? t.solid : t.soft,
        border: `1px solid ${solid ? 'transparent' : 'var(--border-subtle)'}`,
        borderRadius: 'var(--radius-pill)',
        whiteSpace: 'nowrap',
      }}
    >
      {dot && <span style={{ width: 6, height: 6, borderRadius: '50%', background: solid ? 'var(--fg-on-accent)' : t.fg }} />}
      {children}
    </span>
  )
}

export interface VendorChipProps {
  id: SourceID
  label?: boolean
  size?: number
}

export function VendorChip({ id, label = true, size = 22 }: VendorChipProps) {
  const v = vendorMeta(id)
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <span
        style={{
          width: size,
          height: size,
          borderRadius: 6,
          background: v.color,
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          font: '700 10px/1 var(--font-mono)',
          color: '#07101f',
          flexShrink: 0,
        }}
      >
        {v.mono}
      </span>
      {label && <span style={{ font: '500 13px/1 var(--font-ui)', color: 'var(--fg-secondary)', whiteSpace: 'nowrap' }}>{v.name}</span>}
    </span>
  )
}

export interface AvatarProps {
  initials?: string
  size?: number
  tone?: string
}

export function Avatar({ initials = 'AV', size = 28, tone = 'var(--cat-3)' }: AvatarProps) {
  return (
    <span
      style={{
        width: size,
        height: size,
        borderRadius: '50%',
        background: `color-mix(in srgb, ${tone} 30%, var(--ink-700))`,
        border: '1px solid var(--border-default)',
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        font: '700 11px/1 var(--font-ui)',
        color: 'var(--fg-primary)',
      }}
    >
      {initials}
    </span>
  )
}

export interface LegendItem {
  label: string
  color: string
  value?: string | number
}

export function Legend({ items = [] }: { items: LegendItem[] }) {
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px 16px' }}>
      {items.map((it, i) => (
        <span key={i} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, font: '500 12px/1 var(--font-ui)', color: 'var(--fg-secondary)' }}>
          <span style={{ width: 9, height: 9, borderRadius: 2, background: it.color }} />
          {it.label}
          {it.value != null && <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-muted)', marginLeft: 2 }}>{it.value}</span>}
        </span>
      ))}
    </div>
  )
}

export interface SourceStackItem {
  id: string
  label?: string
  color: string
  value: number
}

export function SourceStack({ sources = [], width = 120, height = 7, showTrack = true }: { sources: SourceStackItem[]; width?: number | string; height?: number; showTrack?: boolean }) {
  const total = sources.reduce((s, x) => s + x.value, 0) || 1
  return (
    <span
      style={{
        display: 'inline-flex',
        width,
        height,
        borderRadius: height / 2,
        overflow: 'hidden',
        background: showTrack ? 'var(--ink-700)' : 'transparent',
        gap: 1,
        verticalAlign: 'middle',
      }}
    >
      {sources.map((s) => (
        <span key={s.id} title={s.label ? `${s.label}: ${s.value}` : undefined} style={{ width: `${(s.value / total) * 100}%`, background: s.color }} />
      ))}
    </span>
  )
}

export interface BarRowProps {
  label: ReactNode
  value: string
  max: number
  rawValue: number
  color?: string
  sub?: ReactNode
  onClick?: () => void
}

export function BarRow({ label, value, max, rawValue, color = 'var(--accent)', sub, onClick }: BarRowProps) {
  return (
    <div
      onClick={onClick}
      style={{ display: 'grid', gridTemplateColumns: '1fr auto', gap: '2px 12px', padding: '9px 0', cursor: onClick ? 'pointer' : 'default' }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
        <span style={{ font: '500 13px/1 var(--font-ui)', color: 'var(--fg-secondary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{label}</span>
      </div>
      <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums', textAlign: 'right' }}>{value}</span>
      <div style={{ gridColumn: '1 / -1', height: 6, borderRadius: 3, background: 'var(--ink-700)', overflow: 'hidden', marginTop: 2 }}>
        <div style={{ width: `${Math.max(2, (max ? rawValue / max : 0) * 100)}%`, height: '100%', background: color, borderRadius: 3 }} />
      </div>
      {sub && <div style={{ gridColumn: '1 / -1', font: '400 11px/1 var(--font-ui)', color: 'var(--fg-faint)', marginTop: 4 }}>{sub}</div>}
    </div>
  )
}

/* responsive full-width sparkline strip (non-scaling stroke) */
export function RSpark({ data = [], tone = 'var(--accent)', height = 30 }: { data: number[]; tone?: string; height?: number }) {
  if (!data.length) return null
  const min = Math.min(...data)
  const max = Math.max(...data)
  const span = max - min || 1
  const n = data.length
  const W = 100
  const pts = data.map((d, i) => [(i / (n - 1 || 1)) * W, height - 2 - ((d - min) / span) * (height - 5)] as const)
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(2)} ${y.toFixed(2)}`).join(' ')
  const gid = `rs${Math.round(data[0])}_${n}_${height}`
  return (
    <svg width="100%" height={height} viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" style={{ display: 'block' }}>
      <defs>
        <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={tone} stopOpacity="0.20" />
          <stop offset="100%" stopColor={tone} stopOpacity="0" />
        </linearGradient>
      </defs>
      <path d={`${line} L${W} ${height} L0 ${height} Z`} fill={`url(#${gid})`} />
      <path d={line} fill="none" stroke={tone} strokeWidth="1.5" vectorEffect="non-scaling-stroke" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  )
}

export function Sparkline({ data = [], width = 96, height = 28, tone = 'var(--accent)', fill = true }: { data: number[]; width?: number; height?: number; tone?: string; fill?: boolean }) {
  if (!data.length) return <svg width={width} height={height} />
  const min = Math.min(...data)
  const max = Math.max(...data)
  const span = max - min || 1
  const sx = width / (data.length - 1 || 1)
  const pts = data.map((d, i) => [i * sx, height - 2 - ((d - min) / span) * (height - 4)] as const)
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' ')
  const gid = `sp${Math.round(width)}_${Math.round(data[0])}_${data.length}`
  const last = pts[pts.length - 1]
  const lineStyle: CSSProperties = { display: 'block', overflow: 'visible' }
  return (
    <svg width={width} height={height} style={lineStyle}>
      <defs>
        <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={tone} stopOpacity="0.22" />
          <stop offset="100%" stopColor={tone} stopOpacity="0" />
        </linearGradient>
      </defs>
      {fill && <path d={`${line} L${width} ${height} L0 ${height} Z`} fill={`url(#${gid})`} />}
      <path d={line} fill="none" stroke={tone} strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={last[0]} cy={last[1]} r="2" fill={tone} />
    </svg>
  )
}

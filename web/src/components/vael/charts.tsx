/* Vael charts — pure SVG, calm + exact. AreaChart, StackedBars, Donut,
   BudgetRing, Heatmap. Ported from the Vael ui_kit (replaces Recharts). */
import { useLayoutEffect, useRef, useState } from 'react'
import type { CSSProperties, MouseEvent, RefObject } from 'react'

export function niceMax(v: number): number {
  if (v <= 0) return 1
  const mag = Math.pow(10, Math.floor(Math.log10(v)))
  const n = v / mag
  const step = n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10
  return step * mag
}

/** Observe an element's width; returns [ref, width]. Attach ref to a block element. */
export function useWidth(initial = 600): [RefObject<HTMLDivElement | null>, number] {
  const ref = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(initial)
  useLayoutEffect(() => {
    if (!ref.current || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      const cw = entries[0].contentRect.width
      if (cw) setW(Math.round(cw))
    })
    ro.observe(ref.current)
    return () => ro.disconnect()
  }, [])
  return [ref, w]
}

export interface AreaSeries {
  name: string
  color: string
  data: number[]
  fmt?: (v: number) => string
}

export interface AreaChartProps {
  labels: string[]
  series: AreaSeries[]
  width?: number
  height?: number
  yFormat?: (v: number) => string
  fillFirst?: boolean
}

export function AreaChart({ labels = [], series = [], width = 600, height = 220, yFormat = (v) => String(v), fillFirst = true }: AreaChartProps) {
  const [hi, setHi] = useState(-1)
  const padL = 48
  const padR = 14
  const padT = 14
  const padB = 24
  const W = width
  const H = height
  const iw = W - padL - padR
  const ih = H - padT - padB
  const allVals = series.flatMap((s) => s.data)
  const max = niceMax(Math.max(1, ...allVals))
  const n = labels.length
  const x = (i: number) => padL + (n <= 1 ? 0 : (i / (n - 1)) * iw)
  const y = (v: number) => padT + ih - (v / max) * ih
  const ticks = 4

  const onMove = (e: MouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const rx = e.clientX - rect.left
    let idx = Math.round(((rx - padL) / iw) * (n - 1))
    idx = Math.max(0, Math.min(n - 1, idx))
    setHi(idx)
  }

  return (
    <div style={{ position: 'relative', width: W }}>
      <svg width={W} height={H} onMouseMove={onMove} onMouseLeave={() => setHi(-1)} style={{ display: 'block' }}>
        {Array.from({ length: ticks + 1 }).map((_, i) => {
          const v = (max / ticks) * i
          return (
            <g key={i}>
              <line x1={padL} x2={W - padR} y1={y(v)} y2={y(v)} stroke="var(--border-subtle)" />
              <text x={padL - 8} y={y(v) + 3.5} textAnchor="end" fontFamily="var(--font-mono)" fontSize="10" fill="var(--fg-faint)">{yFormat(v)}</text>
            </g>
          )
        })}
        {labels.map((l, i) =>
          n <= 12 || i % Math.ceil(n / 7) === 0 ? (
            <text key={i} x={x(i)} y={H - 7} textAnchor="middle" fontFamily="var(--font-mono)" fontSize="10" fill="var(--fg-faint)">{l}</text>
          ) : null,
        )}
        {series.map((s, si) => {
          const line = s.data.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)} ${y(v).toFixed(1)}`).join(' ')
          const area = `${line} L${x(n - 1)} ${y(0)} L${x(0)} ${y(0)} Z`
          const gid = `ac${si}_${width}`
          return (
            <g key={si}>
              <defs>
                <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={s.color} stopOpacity={si === 0 && fillFirst ? 0.2 : 0.06} />
                  <stop offset="100%" stopColor={s.color} stopOpacity="0" />
                </linearGradient>
              </defs>
              {(si === 0 ? fillFirst : false) && <path d={area} fill={`url(#${gid})`} />}
              <path d={line} fill="none" stroke={s.color} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
            </g>
          )
        })}
        {hi >= 0 && (
          <g>
            <line x1={x(hi)} x2={x(hi)} y1={padT} y2={padT + ih} stroke="var(--border-strong)" />
            {series.map((s, si) => (
              <circle key={si} cx={x(hi)} cy={y(s.data[hi])} r="3.5" fill="var(--ink-900)" stroke={s.color} strokeWidth="2" />
            ))}
          </g>
        )}
      </svg>
      {hi >= 0 && (
        <div
          style={{
            position: 'absolute',
            left: Math.min(W - 150, Math.max(0, x(hi) - 70)),
            top: 6,
            pointerEvents: 'none',
            background: 'var(--ink-700)',
            border: '1px solid var(--border-strong)',
            borderRadius: 'var(--radius-md)',
            boxShadow: 'var(--shadow-lg)',
            padding: '8px 10px',
            minWidth: 130,
            zIndex: 5,
          }}
        >
          <div style={{ font: '600 11px/1 var(--font-ui)', color: 'var(--fg-muted)', marginBottom: 6 }}>{labels[hi]}</div>
          {series.map((s, si) => (
            <div key={si} style={{ display: 'flex', alignItems: 'center', gap: 7, marginTop: si ? 4 : 0 }}>
              <span style={{ width: 8, height: 8, borderRadius: 2, background: s.color }} />
              <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-secondary)', flex: 1 }}>{s.name}</span>
              <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{s.fmt ? s.fmt(s.data[hi]) : s.data[hi]}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export interface StackedBarDay {
  key: string
  wd?: string
  per: Record<string, number>
}
export interface StackedBarKey {
  id: string
  short: string
  color: string
}

export interface StackedBarsProps {
  days: StackedBarDay[]
  keys: StackedBarKey[]
  width?: number
  height?: number
  valueFmt?: (v: number) => string
  /** Metric name shown as an uppercase eyebrow in the hover tooltip (e.g. "Tokens"),
      so it's always clear which metric the bars represent. */
  label?: string
  /** Overlay a combined-total trend line and a Total tooltip row. The Total is the
      per-day sum of the stacked segments on screen; shown for every metric. */
  showTotal?: boolean
}

export function StackedBars({ days = [], keys = [], width = 600, height = 220, valueFmt = (v) => String(v), label, showTotal = true }: StackedBarsProps) {
  const [hi, setHi] = useState(-1)
  const padL = 48
  const padR = 14
  const padT = 14
  const padB = 24
  const iw = width - padL - padR
  const ih = height - padT - padB
  const totals = days.map((d) => keys.reduce((s, k) => s + (d.per[k.id] || 0), 0))
  const max = niceMax(Math.max(1, ...totals))
  const n = days.length
  const gap = 3
  const slot = iw / (n || 1)
  const bw = Math.max(3, slot - gap)
  const y = (v: number) => padT + ih - (v / max) * ih
  const ticks = 4

  const onMove = (e: MouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const idx = Math.floor((e.clientX - rect.left - padL) / (slot || 1))
    setHi(Math.max(0, Math.min(n - 1, idx)))
  }

  return (
    <div style={{ position: 'relative', width }}>
      <svg width={width} height={height} onMouseMove={onMove} onMouseLeave={() => setHi(-1)} style={{ display: 'block' }}>
        {Array.from({ length: ticks + 1 }).map((_, i) => {
          const v = (max / ticks) * i
          return (
            <g key={i}>
              <line x1={padL} x2={width - padR} y1={y(v)} y2={y(v)} stroke="var(--border-subtle)" />
              <text x={padL - 8} y={y(v) + 3.5} textAnchor="end" fontFamily="var(--font-mono)" fontSize="10" fill="var(--fg-faint)">{valueFmt(v)}</text>
            </g>
          )
        })}
        {days.map((d, i) => {
          const cx = padL + i * slot + (slot - bw) / 2
          let acc = 0
          const active = hi === i
          return (
            <g key={i}>
              <rect x={cx - 1} y={padT} width={bw + 2} height={ih} fill={active ? 'var(--ink-750)' : 'transparent'} rx="2" />
              {keys.map((k) => {
                const v = d.per[k.id] || 0
                const h = (v / max) * ih
                const yy = y(acc + v)
                acc += v
                return (
                  <rect
                    key={k.id}
                    x={cx}
                    width={bw}
                    fill={k.color}
                    rx="1"
                    style={
                      {
                        // y/height live in style (not attributes) so the metric-switch
                        // transition fires reliably; SVG2 geometry props are CSS-transitionable
                        y: yy,
                        height: Math.max(0, h),
                        opacity: hi === -1 || active ? 1 : 0.45,
                        transition: 'y var(--dur-base) var(--ease-out), height var(--dur-base) var(--ease-out), opacity var(--dur-fast) var(--ease-out)',
                      } as CSSProperties
                    }
                  />
                )
              })}
              {(n <= 14 || i % Math.ceil(n / 8) === 0) && (
                <text x={cx + bw / 2} y={height - 7} textAnchor="middle" fontFamily="var(--font-mono)" fontSize="10" fill="var(--fg-faint)">{d.key.split(' ').slice(-1)[0]}</text>
              )}
            </g>
          )
        })}
        {showTotal && n > 1 && (
          <path
            d={totals.map((t, i) => `${i ? 'L' : 'M'}${(padL + (i + 0.5) * slot).toFixed(1)} ${y(t).toFixed(1)}`).join(' ')}
            fill="none"
            stroke="var(--fg-secondary)"
            strokeOpacity={0.55}
            strokeWidth={1.5}
            strokeLinejoin="round"
            strokeLinecap="round"
            style={{ pointerEvents: 'none' }}
          />
        )}
        {showTotal && n > 1 && hi >= 0 && (
          <circle cx={padL + (hi + 0.5) * slot} cy={y(totals[hi])} r={3} fill="var(--ink-900)" stroke="var(--fg-secondary)" strokeWidth={1.5} style={{ pointerEvents: 'none' }} />
        )}
      </svg>
      {hi >= 0 && days[hi] && (
        <div
          style={{
            position: 'absolute',
            left: Math.min(width - 170, Math.max(0, padL + hi * (iw / (n || 1)) - 70)),
            top: 6,
            pointerEvents: 'none',
            background: 'var(--ink-700)',
            border: '1px solid var(--border-strong)',
            borderRadius: 'var(--radius-md)',
            boxShadow: 'var(--shadow-lg)',
            padding: '8px 10px',
            minWidth: 150,
            zIndex: 5,
          }}
        >
          <div style={{ marginBottom: 6 }}>
            {label && <div style={{ font: '600 9px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-faint)', marginBottom: 4 }}>{label}</div>}
            <div style={{ font: '600 11px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>{days[hi].key}{days[hi].wd ? ` · ${days[hi].wd}` : ''}</div>
          </div>
          {keys.map((k) => (
            <div key={k.id} style={{ display: 'flex', alignItems: 'center', gap: 7, marginTop: 4 }}>
              <span style={{ width: 8, height: 8, borderRadius: 2, background: k.color }} />
              <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-secondary)', flex: 1 }}>{k.short}</span>
              <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{valueFmt(days[hi].per[k.id] || 0)}</span>
            </div>
          ))}
          {showTotal && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 7, marginTop: 6, paddingTop: 6, borderTop: '1px solid var(--border-subtle)' }}>
              <span style={{ width: 8 }} />
              <span style={{ font: '600 12px/1 var(--font-ui)', color: 'var(--fg-secondary)', flex: 1 }}>Total</span>
              <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{valueFmt(totals[hi])}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export interface DonutSegment {
  value: number
  color: string
  label?: string
  /** Optional secondary line in the tooltip (e.g. a cost-provenance note). */
  sub?: string
  /** Preformatted value for the tooltip (falls back to String(value)). */
  valueText?: string
  /** Preformatted share for the tooltip (falls back to a computed %). */
  shareText?: string
}

export interface DonutProps {
  segments: DonutSegment[]
  size?: number
  thickness?: number
  centerTop?: string
  centerBottom?: string
  /** Externally-controlled highlighted segment (enables legend↔arc cross-highlight).
      Pass `undefined` to let the donut manage hover on its own. */
  activeIndex?: number | null
  /** Reports the hovered segment index (or null) so a parent can sync the legend. */
  onHoverIndex?: (i: number | null) => void
}

export function Donut({ segments = [], size = 150, thickness = 16, centerTop = '', centerBottom = '', activeIndex, onHoverIndex }: DonutProps) {
  const [hovInternal, setHovInternal] = useState<number | null>(null)
  const total = segments.reduce((s, x) => s + x.value, 0) || 1
  const r = (size - thickness) / 2
  const c = 2 * Math.PI * r
  const active = activeIndex !== undefined ? activeIndex : hovInternal

  const report = (i: number | null) => {
    setHovInternal(i)
    onHoverIndex?.(i)
  }
  const onMove = (e: MouseEvent<HTMLDivElement>) => {
    if (!segments.length) return
    const rect = e.currentTarget.getBoundingClientRect()
    const dx = e.clientX - rect.left - size / 2
    const dy = e.clientY - rect.top - size / 2
    const dist = Math.hypot(dx, dy)
    if (dist < size / 2 - thickness - 2 || dist > size / 2 + 2) return report(null)
    // angle clockwise from 12 o'clock (the svg is rotate(-90deg), so the stroke
    // starts at the top); match the segment paint order.
    let ang = (Math.atan2(dy, dx) * 180) / Math.PI + 90
    if (ang < 0) ang += 360
    const frac = ang / 360
    let walk = 0
    let idx = -1
    for (let i = 0; i < segments.length; i++) {
      const f = segments[i].value / total
      if (frac >= walk && frac < walk + f) {
        idx = i
        break
      }
      walk += f
    }
    report(idx < 0 ? null : idx)
  }

  const act = active != null && active >= 0 && active < segments.length ? segments[active] : null
  return (
    <div style={{ position: 'relative', width: size, height: size }} onMouseMove={onMove} onMouseLeave={() => report(null)}>
      <svg width={size} height={size} style={{ transform: 'rotate(-90deg)', overflow: 'visible' }}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--ink-700)" strokeWidth={thickness} />
        {segments.map((s, i) => {
          const frac = s.value / total
          const dash = frac * c
          // cumulative fraction of all earlier segments (n is tiny → no mutable accumulator)
          const offset = segments.slice(0, i).reduce((a, x) => a + x.value, 0) / total
          const isActive = active === i
          const dimmed = active != null && active >= 0 && !isActive
          return (
            <circle
              key={i}
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              stroke={s.color}
              strokeWidth={isActive ? thickness + 2 : thickness}
              strokeDasharray={`${dash} ${c - dash}`}
              strokeDashoffset={-offset * c}
              strokeLinecap="butt"
              style={{ opacity: dimmed ? 0.4 : 1, transition: 'opacity var(--dur-fast) var(--ease-out), stroke-width var(--dur-fast) var(--ease-out)' }}
            />
          )
        })}
      </svg>
      {(centerTop || centerBottom) && (
        <div style={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', pointerEvents: 'none' }}>
          <div style={{ font: '700 22px/1 var(--font-mono)', color: 'var(--fg-primary)', letterSpacing: '-0.02em' }}>{centerTop}</div>
          <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginTop: 5 }}>{centerBottom}</div>
        </div>
      )}
      {act && (
        <div
          style={{
            position: 'absolute',
            left: '50%',
            bottom: '100%',
            transform: 'translateX(-50%)',
            marginBottom: 8,
            pointerEvents: 'none',
            background: 'var(--ink-700)',
            border: '1px solid var(--border-strong)',
            borderRadius: 'var(--radius-md)',
            boxShadow: 'var(--shadow-lg)',
            padding: '8px 10px',
            minWidth: 132,
            zIndex: 5,
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 7, whiteSpace: 'nowrap' }}>
            <span style={{ width: 8, height: 8, borderRadius: 2, background: act.color }} />
            <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-secondary)', flex: 1 }}>{act.label ?? ''}</span>
            <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{act.valueText ?? String(act.value)}</span>
          </div>
          <div style={{ font: '600 11px/1 var(--font-ui)', color: 'var(--fg-muted)', marginTop: 5, textAlign: 'right' }}>{act.shareText ?? `${Math.round((act.value / total) * 100)}%`}</div>
          {act.sub && <div style={{ font: '400 10px/1.35 var(--font-ui)', color: 'var(--fg-faint)', marginTop: 5, maxWidth: 200 }}>{act.sub}</div>}
        </div>
      )}
    </div>
  )
}

export function BudgetRing({ pct = 0, size = 132, thickness = 12, tone = 'var(--accent)', label = '' }: { pct?: number; size?: number; thickness?: number; tone?: string; label?: string }) {
  const r = (size - thickness) / 2
  const c = 2 * Math.PI * r
  const dash = (Math.min(100, pct) / 100) * c
  return (
    <div style={{ position: 'relative', width: size, height: size }}>
      <svg width={size} height={size} style={{ transform: 'rotate(-90deg)' }}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--ink-700)" strokeWidth={thickness} />
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke={tone} strokeWidth={thickness} strokeDasharray={`${dash} ${c - dash}`} strokeLinecap="round" />
      </svg>
      <div style={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center' }}>
        <div style={{ font: '700 24px/1 var(--font-mono)', color: 'var(--fg-primary)' }}>{pct}%</div>
        <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.06em', textTransform: 'uppercase', color: 'var(--fg-muted)', marginTop: 4 }}>{label}</div>
      </div>
    </div>
  )
}

export interface HeatmapCell {
  key: string
  value: number
}

export function Heatmap({ cells = [], color = 'var(--cat-1)' }: { cells: HeatmapCell[]; color?: string }) {
  const max = Math.max(1, ...cells.map((d) => d.value))
  return (
    <div style={{ display: 'grid', gridTemplateColumns: `repeat(${cells.length || 1}, 1fr)`, gap: 3 }}>
      {cells.map((d, i) => {
        const t = d.value / max
        return (
          <div
            key={i}
            title={`${d.key}: ${d.value}`}
            style={{
              height: 26,
              borderRadius: 3,
              background: `color-mix(in srgb, ${color} ${Math.round(12 + t * 88)}%, var(--ink-800))`,
              border: '1px solid var(--border-subtle)',
            }}
          />
        )
      })}
    </div>
  )
}

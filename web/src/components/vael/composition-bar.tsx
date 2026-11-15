/* Vael CompositionBar — one horizontal bar split into proportional segments,
   with the breakdown listed underneath. */
import { useState } from 'react'

export interface CompositionItem {
  key: string
  label: string
  value: number
  color: string
}

export interface CompositionBarProps {
  items: CompositionItem[]
  format?: (value: number) => string
  /** Accessible summary of what the bar divides, e.g. "Token composition". */
  ariaLabel?: string
}

/** Proportional segment bar with a per-part list (value and share); hovering or focusing a row highlights its segment. @category Charts */
export function CompositionBar({ items, format = (v) => String(v), ariaLabel = 'Composition' }: CompositionBarProps) {
  const [active, setActive] = useState<number | null>(null)
  const visible = items.filter((item) => item.value > 0)
  const total = visible.reduce((sum, item) => sum + item.value, 0)

  if (total <= 0) {
    return <div style={{ padding: '18px 0', textAlign: 'center', font: '400 13px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>No data in range.</div>
  }

  // A real part never reads as nothing: below one percent it is "<1%".
  const share = (value: number) => {
    const pct = (value / total) * 100
    return pct < 1 ? '<1%' : `${Math.round(pct)}%`
  }

  return (
    <div>
      <div
        role="img"
        aria-label={`${ariaLabel}: ${visible.map((item) => `${item.label} ${share(item.value)}`).join(', ')}`}
        style={{ display: 'flex', gap: 2, height: 12, borderRadius: 6, overflow: 'hidden', background: 'var(--ink-700)' }}
      >
        {visible.map((item, index) => (
          <div
            key={item.key}
            onMouseEnter={() => setActive(index)}
            onMouseLeave={() => setActive(null)}
            style={{
              // A floor keeps a sub-1% part from vanishing entirely.
              flex: `${item.value} 0 3px`,
              background: item.color,
              opacity: active == null || active === index ? 1 : 0.35,
              transition: 'opacity var(--dur-fast) var(--ease-out)',
            }}
          />
        ))}
      </div>
      <ul style={{ listStyle: 'none', margin: '12px 0 0', padding: 0, display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', columnGap: 20 }}>
        {visible.map((item, index) => (
          <li
            key={item.key}
            onMouseEnter={() => setActive(index)}
            onMouseLeave={() => setActive(null)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '7px 6px',
              borderRadius: 'var(--radius-sm)',
              background: active === index ? 'var(--ink-750)' : 'transparent',
              transition: 'background var(--dur-fast) var(--ease-out)',
            }}
          >
            <span aria-hidden="true" style={{ width: 9, height: 9, borderRadius: 2, background: item.color, flexShrink: 0 }} />
            <span style={{ flex: 1, minWidth: 0, font: '500 13px/1.2 var(--font-ui)', color: 'var(--fg-secondary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{item.label}</span>
            <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>{format(item.value)}</span>
            <span style={{ width: 38, textAlign: 'right', font: '500 12px/1 var(--font-mono)', color: 'var(--fg-muted)', fontVariantNumeric: 'tabular-nums' }}>
              {share(item.value)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}

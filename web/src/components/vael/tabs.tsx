/* Vael Tabs — inline underline tabs (ported from the screens2 detail pattern). */
import type { ReactNode } from 'react'

export interface TabItem<V extends string> {
  value: V
  label: ReactNode
  count?: ReactNode
}

export interface TabsProps<V extends string> {
  tabs: TabItem<V>[]
  value: V
  onChange: (value: V) => void
}

export function Tabs<V extends string>({ tabs, value, onChange }: TabsProps<V>) {
  return (
    <div style={{ display: 'flex', gap: 2, borderBottom: '1px solid var(--border-default)' }}>
      {tabs.map((t) => {
        const active = t.value === value
        return (
          <button
            key={t.value}
            type="button"
            onClick={() => onChange(t.value)}
            style={{
              position: 'relative',
              display: 'inline-flex',
              alignItems: 'center',
              gap: 7,
              height: 42,
              padding: '0 14px',
              border: 'none',
              background: 'transparent',
              color: active ? 'var(--fg-primary)' : 'var(--fg-muted)',
              font: `${active ? 600 : 500} 13px/1 var(--font-ui)`,
              cursor: 'pointer',
              whiteSpace: 'nowrap',
            }}
          >
            {t.label}
            {t.count != null && (
              <span style={{ font: '500 11px/1 var(--font-mono)', color: 'var(--fg-faint)' }}>{t.count}</span>
            )}
            {active && <span style={{ position: 'absolute', left: 8, right: 8, bottom: -1, height: 2, borderRadius: 2, background: 'var(--accent)' }} />}
          </button>
        )
      })}
    </div>
  )
}

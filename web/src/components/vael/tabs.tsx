/* Vael Tabs — inline underline tabs (ported from the screens2 detail pattern). */
import { useRef } from 'react'
import type { KeyboardEvent, ReactNode } from 'react'

export interface TabItem<V extends string> {
  value: V
  label: ReactNode
  count?: ReactNode
}

export interface TabsProps<V extends string> {
  tabs: TabItem<V>[]
  value: V
  onChange: (value: V) => void
  /** Accessible name for the tab list. */
  ariaLabel?: string
}

/** Underline tabs with optional mono count badges, for switching detail sections; arrow keys move between tabs. @category Navigation */
export function Tabs<V extends string>({ tabs, value, onChange, ariaLabel }: TabsProps<V>) {
  const listRef = useRef<HTMLDivElement>(null)

  // Roving tabindex: the list is one tab stop and the arrows walk it, which is
  // what a tablist is expected to do.
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const index = tabs.findIndex((t) => t.value === value)
    let next = index
    if (event.key === 'ArrowRight') next = (index + 1) % tabs.length
    else if (event.key === 'ArrowLeft') next = (index - 1 + tabs.length) % tabs.length
    else if (event.key === 'Home') next = 0
    else if (event.key === 'End') next = tabs.length - 1
    else return
    event.preventDefault()
    onChange(tabs[next].value)
    listRef.current?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus()
  }

  return (
    <div ref={listRef} role="tablist" aria-label={ariaLabel} onKeyDown={onKeyDown} style={{ display: 'flex', gap: 2, borderBottom: '1px solid var(--border-default)', overflowX: 'auto' }}>
      {tabs.map((t) => {
        const active = t.value === value
        return (
          <button
            key={t.value}
            type="button"
            role="tab"
            aria-selected={active}
            tabIndex={active ? 0 : -1}
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
              flexShrink: 0,
            }}
          >
            {t.label}
            {t.count != null && (
              <span style={{ font: '500 11px/1 var(--font-mono)', color: active ? 'var(--fg-muted)' : 'var(--fg-faint)' }}>{t.count}</span>
            )}
            {active && <span style={{ position: 'absolute', left: 8, right: 8, bottom: 0, height: 2, borderRadius: 2, background: 'var(--accent)' }} />}
          </button>
        )
      })}
    </div>
  )
}

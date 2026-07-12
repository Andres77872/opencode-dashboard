import { Avatar } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 12, ...style }}>
      {children}
    </div>
  )
}

export const Basic = () => (
  <Canvas>
    <Avatar initials="AL" />
    <Avatar initials="MK" tone="var(--cat-1)" />
    <Avatar initials="RS" tone="var(--cat-5)" />
    <Avatar initials="JD" tone="var(--cat-7)" />
  </Canvas>
)

export const Sizes = () => (
  <Canvas>
    <Avatar initials="AL" size={20} tone="var(--cat-2)" />
    <Avatar initials="AL" size={28} tone="var(--cat-2)" />
    <Avatar initials="AL" size={36} tone="var(--cat-2)" />
    <Avatar initials="AL" size={48} tone="var(--cat-2)" />
  </Canvas>
)

export const InUserRow = () => (
  <Canvas style={{ display: 'block' }}>
    <div style={{ maxWidth: 380 }}>
      {[
        { init: 'AL', tone: 'var(--cat-3)', name: 'andres', meta: '1,284 sessions' },
        { init: 'MK', tone: 'var(--cat-1)', name: 'marta.k', meta: '412 sessions' },
        { init: 'RS', tone: 'var(--cat-5)', name: 'r.santos', meta: '96 sessions' },
      ].map((u) => (
        <div key={u.init} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '7px 0' }}>
          <Avatar initials={u.init} tone={u.tone} />
          <span style={{ font: '500 13px/1 var(--font-ui)', color: 'var(--fg-secondary)' }}>{u.name}</span>
          <span style={{ font: '600 12px/1 var(--font-mono)', color: 'var(--fg-muted)', marginLeft: 'auto', fontVariantNumeric: 'tabular-nums' }}>{u.meta}</span>
        </div>
      ))}
    </div>
  </Canvas>
)

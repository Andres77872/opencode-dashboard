import { useState } from 'react'
import { Tabs } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

const items = [
  { value: 'overview', label: 'Overview' },
  { value: 'timeline', label: 'Timeline', count: 24 },
  { value: 'tools', label: 'Tool calls', count: 156 },
  { value: 'config', label: 'Config' },
]

export const Underline = () => {
  const [tab, setTab] = useState('tools')
  return (
    <Canvas style={{ maxWidth: 520 }}>
      <Tabs tabs={items} value={tab} onChange={setTab} />
      <div style={{ font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)', paddingTop: 14 }}>
        156 tool calls in this session — read 84 · edit 47 · bash 25.
      </div>
    </Canvas>
  )
}

import { BarRow } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

export const TopProjects = () => (
  <Canvas style={{ maxWidth: 420 }}>
    <BarRow label="opencode-dashboard" value="18.4M" max={18400000} rawValue={18400000} />
    <BarRow label="vael-api" value="12.1M" max={18400000} rawValue={12100000} />
    <BarRow label="argo-ui" value="3.2M" max={18400000} rawValue={3200000} />
    <BarRow label="infra-scripts" value="1.1M" max={18400000} rawValue={1100000} />
  </Canvas>
)

export const ToolsWithColors = () => (
  <Canvas style={{ maxWidth: 420 }}>
    <BarRow label="read" value="4,812" max={4812} rawValue={4812} color="var(--cat-1)" />
    <BarRow label="edit" value="3,309" max={4812} rawValue={3309} color="var(--cat-2)" />
    <BarRow label="bash" value="2,488" max={4812} rawValue={2488} color="var(--cat-3)" />
    <BarRow label="grep" value="1,364" max={4812} rawValue={1364} color="var(--cat-4)" />
  </Canvas>
)

export const WithSub = () => (
  <Canvas style={{ maxWidth: 420 }}>
    <BarRow
      label="claude-sonnet-5"
      value="$142.10"
      max={142.1}
      rawValue={142.1}
      sub="31.8M tokens · 812 sessions"
    />
    <BarRow
      label="gpt-5.6-codex"
      value="$54.60"
      max={142.1}
      rawValue={54.6}
      color="var(--cat-2)"
      sub="12.9M tokens · 344 sessions"
    />
    <BarRow
      label="claude-haiku-4.5"
      value="$15.70"
      max={142.1}
      rawValue={15.7}
      color="var(--cat-3)"
      sub="3.5M tokens · 128 sessions"
    />
  </Canvas>
)

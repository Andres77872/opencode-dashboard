import { Skeleton, Card } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

export const LoadingCard = () => (
  <Canvas style={{ maxWidth: 460 }}>
    <Card style={{ width: 420 }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <Skeleton width={160} height={16} />
        <Skeleton width="82%" height={12} />
        <Skeleton width="68%" height={12} />
        <Skeleton width="90%" height={12} />
        <Skeleton height={120} radius="var(--radius-lg)" style={{ marginTop: 4 }} />
      </div>
    </Card>
  </Canvas>
)

export const Bare = () => (
  <Canvas style={{ maxWidth: 460 }}>
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, width: 420 }}>
      <Skeleton width={220} height={14} />
      <Skeleton width="100%" height={14} />
      <Skeleton width="72%" height={14} />
    </div>
  </Canvas>
)

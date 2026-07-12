import { ErrorState, Card } from 'web'
import type { ReactNode, CSSProperties } from 'react'

function Canvas({ children, style }: { children?: ReactNode; style?: CSSProperties }) {
  return (
    <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10, ...style }}>
      {children}
    </div>
  )
}

export const DefaultRetry = () => (
  <Canvas style={{ maxWidth: 520 }}>
    <Card style={{ width: 480 }}>
      <ErrorState message="fetch aborted after 3 attempts" onRetry={() => {}} />
    </Card>
  </Canvas>
)

export const CustomMessage = () => (
  <Canvas style={{ maxWidth: 520 }}>
    <Card style={{ width: 480 }}>
      <ErrorState
        title="Failed to load sessions"
        message="GET /api/sessions 502 — upstream timed out"
        onRetry={() => {}}
      />
    </Card>
  </Canvas>
)

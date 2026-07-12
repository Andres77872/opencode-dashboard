import { Button, EmptyState } from 'web'
import type { ReactNode } from 'react'

function Canvas({ children }: { children?: ReactNode }) {
  return <div style={{ background: 'var(--ink-900)', padding: 20, borderRadius: 10 }}>{children}</div>
}

export const Default = () => (
  <Canvas>
    <EmptyState
      icon="database"
      title="No usage in this range"
      description="No agent sessions were recorded between Jun 12 and Jul 11. Widen the date range or check that your sources are still syncing."
      action={<Button variant="primary" iconLeft="refresh">Sync sources</Button>}
    />
  </Canvas>
)

export const SearchResults = () => (
  <Canvas>
    <EmptyState
      icon="search"
      title="No projects match “vael-api”"
      description="Try a shorter query, or clear the source filter."
      action={<Button variant="ghost">Clear filters</Button>}
    />
  </Canvas>
)

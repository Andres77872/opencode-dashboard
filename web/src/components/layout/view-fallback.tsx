import { Skeleton } from '../vael/feedback'

/* Shown while a route's code chunk is in flight. It deliberately mirrors the
   stat-tiles-over-a-panel skeleton the views render while their data loads, so
   arriving at a view looks like one continuous load rather than two. */
export function ViewFallback() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }} aria-busy="true" aria-live="polite" aria-label="Loading view">
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
        {Array.from({ length: 4 }).map((_, i) => (
          <div
            key={i}
            style={{
              background: 'var(--ink-800)',
              border: '1px solid var(--border-default)',
              borderRadius: 'var(--radius-lg)',
              padding: 16,
            }}
          >
            <Skeleton width={90} height={11} />
            <Skeleton width={120} height={28} style={{ marginTop: 12 }} />
          </div>
        ))}
      </div>
      <div
        style={{
          background: 'var(--ink-800)',
          border: '1px solid var(--border-default)',
          borderRadius: 'var(--radius-xl)',
          height: 320,
        }}
      />
    </div>
  )
}

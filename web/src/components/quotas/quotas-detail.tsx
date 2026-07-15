/* Detailed provider-quota cards for the Overview page: per provider, gauge
   rings for each window, reset times, plan and freshness badges, and setup
   guidance when a provider has no data source configured yet. */
import { Badge, BudgetRing, Card, ErrorState, SectionTitle, Skeleton } from '../vael'
import { refreshQuotas, useQuotas } from '../../lib/use-quotas'
import { clampPercent, formatResetCountdown, providerMeta, quotaTone, windowLabel } from '../../lib/quotas'
import { formatRelativeTime } from '../../lib/format'
import type { ProviderQuota, QuotaWindow } from '../../types/api'

function WindowRing({ window: win, now }: { window: QuotaWindow; now: number }) {
  const used = Math.round(clampPercent(win.used_percent))
  const resetAbsolute = win.resets_at ? new Date(win.resets_at * 1000).toLocaleString() : undefined
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8 }}>
      <BudgetRing pct={used} size={96} thickness={10} tone={quotaTone(used)} label={windowLabel(win)} />
      {win.resets_at ? (
        <span title={`resets ${resetAbsolute}`} style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>
          resets in {formatResetCountdown(win.resets_at, now)}
        </span>
      ) : (
        <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-faint)' }}>no reset time</span>
      )}
    </div>
  )
}

function statusBadge(quota: ProviderQuota) {
  if (quota.status === 'stale') {
    return <Badge tone="warning">stale</Badge>
  }
  if (quota.status === 'unavailable') {
    return <Badge>unavailable</Badge>
  }
  return <Badge tone="success">live</Badge>
}

function ProviderQuotaCard({ quota, now }: { quota: ProviderQuota; now: number }) {
  const meta = providerMeta(quota)
  const title = (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <span style={{ width: 9, height: 9, borderRadius: '50%', background: meta.color }} />
      {meta.label}
    </span>
  )
  const action = (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
      {quota.plan && <Badge>{quota.plan}</Badge>}
      {statusBadge(quota)}
    </span>
  )

  if (quota.status === 'unavailable') {
    return (
      <Card title={title} action={action}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <span style={{ font: '400 13px/1.5 var(--font-ui)', color: 'var(--fg-secondary)' }}>{quota.reason || 'No quota data available.'}</span>
          {quota.help && (
            <code style={{ display: 'block', padding: '10px 12px', borderRadius: 'var(--radius-md)', background: 'var(--ink-850)', border: '1px solid var(--border-subtle)', font: '400 11px/1.6 var(--font-mono)', color: 'var(--fg-muted)', whiteSpace: 'pre-wrap', overflowWrap: 'break-word' }}>
              {quota.help}
            </code>
          )}
        </div>
      </Card>
    )
  }

  const asOf = quota.as_of_ms ? formatRelativeTime(new Date(quota.as_of_ms)) : null
  return (
    <Card title={title} action={action}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        <div style={{ display: 'flex', justifyContent: 'space-evenly', gap: 12, flexWrap: 'wrap' }}>
          {(quota.windows ?? []).map((win) => (
            <WindowRing key={win.id} window={win} now={now} />
          ))}
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, borderTop: '1px solid var(--border-subtle)', paddingTop: 10 }}>
          {asOf && <span style={{ font: '400 11px/1 var(--font-ui)', color: 'var(--fg-faint)' }}>as of {asOf}</span>}
          {quota.status === 'stale' && (
            <span style={{ font: '400 11px/1.4 var(--font-ui)', color: 'var(--warning)' }}>
              may be out of date — updates while the CLI runs{quota.reason ? ` (${quota.reason})` : ''}
            </span>
          )}
        </div>
      </div>
    </Card>
  )
}

export function QuotasSection() {
  const { data, error } = useQuotas()

  if (!data && !error) {
    return (
      <div>
        <SectionTitle sub="Live subscription usage — not affected by the time range">Quotas</SectionTitle>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} height={180} />
          ))}
        </div>
      </div>
    )
  }
  // A failed fetch must say so. Returning null here (as this did) silently
  // removed the entire Quotas section from Overview, which is indistinguishable
  // from "you have no quota providers configured".
  if (!data && error) {
    return (
      <div>
        <SectionTitle sub="Live subscription usage — not affected by the time range">Quotas</SectionTitle>
        <Card>
          <ErrorState title="Quotas failed to load" message={error} onRetry={refreshQuotas} />
        </Card>
      </div>
    )
  }

  // Genuinely nothing to show: no provider is configured on this machine.
  if (!data || data.providers.length === 0) {
    return null
  }

  const now = data.fetched_at_ms
  return (
    <div>
      <SectionTitle sub="Live subscription usage — not affected by the time range">Quotas</SectionTitle>
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {data.providers.map((quota) => (
          <ProviderQuotaCard key={quota.provider} quota={quota} now={now} />
        ))}
      </div>
    </div>
  )
}

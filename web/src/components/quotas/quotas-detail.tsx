/* Detailed provider-quota cards for the Overview page: per provider, gauge
   rings for each window, reset times, plan and freshness badges, and setup
   guidance when a provider has no data source configured yet. */
import { Badge, BudgetRing, Card, ErrorState, Icon, SectionTitle, Skeleton } from '../vael'
import { refreshQuotas, useQuotas } from '../../lib/use-quotas'
import { clampPercent, formatQuotaCurrency, formatResetLabel, providerMeta, quotaTone, windowLabel } from '../../lib/quotas'
import { formatRelativeTime } from '../../lib/format'
import type { ProviderQuota, QuotaExtraUsage, QuotaWindow } from '../../types/api'

/** Provider strings arrive lower-case ("pro", "resets in 3d"); labels here are sentence case. */
function sentenceCase(text: string) {
  return text ? text.charAt(0).toUpperCase() + text.slice(1) : text
}

const SECTION_SUB = 'Live subscription usage — not affected by the time range'
// As many cards as fit, each at least wide enough for two gauges, so a row of
// three providers fills the width instead of leaving an empty fourth column.
const GRID_STYLE = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 260px), 1fr))', gap: 12 } as const

function WindowRing({ window: win, now }: { window: QuotaWindow; now: number }) {
  const used = Math.round(clampPercent(win.used_percent))
  const resetAbsolute = win.resets_at ? new Date(win.resets_at * 1000).toLocaleString() : undefined
  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 8 }}>
      <BudgetRing pct={used} size={96} thickness={10} tone={quotaTone(used)} label={windowLabel(win)} />
      {win.resets_at ? (
        <span title={`Resets ${resetAbsolute}`} style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>
          {sentenceCase(formatResetLabel(win.resets_at, now))}
        </span>
      ) : (
        <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-faint)' }}>No reset time</span>
      )}
    </div>
  )
}

function statusBadge(quota: ProviderQuota) {
  if (quota.status === 'stale') {
    return <Badge tone="warning" dot>Stale</Badge>
  }
  if (quota.status === 'unavailable') {
    return <Badge>Unavailable</Badge>
  }
  return <Badge tone="success" dot>Live</Badge>
}

function ExtraUsagePanel({ extra }: { extra: QuotaExtraUsage }) {
  const hasLimit = extra.monthly_charge_limit_enabled && extra.monthly_charge_limit_cents > 0
  const usedPercent = hasLimit
    ? clampPercent((extra.monthly_used_cents / extra.monthly_charge_limit_cents) * 100)
    : 0
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 9, borderTop: '1px solid var(--border-subtle)', paddingTop: 11 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
        <span style={{ font: '600 12px/1 var(--font-ui)', color: 'var(--fg-secondary)' }}>Extra usage</span>
        <span style={{ font: '600 13px/1 var(--font-mono)', color: 'var(--fg-primary)' }}>
          {formatQuotaCurrency(extra.balance_cents, extra.currency)} balance
        </span>
      </div>
      {hasLimit && (
        <div style={{ height: 5, borderRadius: 3, background: 'var(--ink-700)', overflow: 'hidden' }}>
          <div style={{ width: `${usedPercent}%`, height: '100%', borderRadius: 3, background: quotaTone(usedPercent) }} />
        </div>
      )}
      <div style={{ display: 'flex', justifyContent: 'space-between', flexWrap: 'wrap', gap: '4px 12px', font: '400 11px/1.3 var(--font-ui)', color: 'var(--fg-muted)' }}>
        <span>Used this month: {formatQuotaCurrency(extra.monthly_used_cents, extra.currency)}</span>
        <span>
          Monthly limit: {hasLimit ? formatQuotaCurrency(extra.monthly_charge_limit_cents, extra.currency) : 'none'}
        </span>
      </div>
    </div>
  )
}

function ProviderQuotaCard({ quota, now }: { quota: ProviderQuota; now: number }) {
  const meta = providerMeta(quota)
  const title = (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <span aria-hidden="true" style={{ width: 9, height: 9, borderRadius: '50%', background: meta.color, flexShrink: 0 }} />
      {meta.label}
    </span>
  )
  const action = (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
      {quota.plan && <Badge>{sentenceCase(quota.plan)}</Badge>}
      {statusBadge(quota)}
    </span>
  )

  if (quota.status === 'unavailable') {
    return (
      <Card title={title} action={action}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div style={{ display: 'flex', alignItems: 'flex-start', gap: 8, font: '400 13px/1.5 var(--font-ui)', color: 'var(--fg-secondary)' }}>
            <Icon name="info" size={15} color="var(--fg-faint)" style={{ marginTop: 2, flexShrink: 0 }} />
            <span style={{ minWidth: 0, overflowWrap: 'anywhere' }}>{quota.reason || 'No quota data is available for this provider.'}</span>
          </div>
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
        {quota.extra_usage && <ExtraUsagePanel extra={quota.extra_usage} />}
        <div style={{ borderTop: '1px solid var(--border-subtle)', paddingTop: 10, font: '400 11px/1.5 var(--font-ui)', color: 'var(--fg-faint)' }}>
          {asOf ? `Updated ${asOf}` : 'Update time unknown'}
          {quota.status === 'stale' && (
            <span style={{ color: 'var(--warning)' }}>
              {' '}· may be out of date; it updates while the CLI runs{quota.reason ? ` (${quota.reason})` : ''}
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
        <SectionTitle sub={SECTION_SUB}>Quotas</SectionTitle>
        <div style={GRID_STYLE}>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} height={220} radius="var(--radius-xl)" />
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
        <SectionTitle sub={SECTION_SUB}>Quotas</SectionTitle>
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
      <SectionTitle sub={SECTION_SUB}>Quotas</SectionTitle>
      <div style={GRID_STYLE}>
        {data.providers.map((quota) => (
          <ProviderQuotaCard key={quota.provider} quota={quota} now={now} />
        ))}
      </div>
    </div>
  )
}

/* Compact provider-quota strip for the sidebar: per provider, used% bars for
   the session and weekly windows with reset countdowns. Detailed view lives on
   the Overview page (QuotasSection). */
import { useQuotas } from '../../lib/use-quotas'
import { clampPercent, formatResetCountdown, providerMeta, quotaTone, windowLabel } from '../../lib/quotas'
import { formatRelativeTime } from '../../lib/format'
import type { ProviderQuota, QuotaWindow } from '../../types/api'

function QuotaBar({ window: win, now }: { window: QuotaWindow; now: number }) {
  const used = clampPercent(win.used_percent)
  const countdown = win.resets_at ? formatResetCountdown(win.resets_at, now) : ''
  const resetTitle = win.resets_at ? `resets ${new Date(win.resets_at * 1000).toLocaleString()}` : undefined
  return (
    <div title={resetTitle} style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
      <span style={{ font: '500 10px/1 var(--font-mono)', color: 'var(--fg-muted)', width: 26, flexShrink: 0 }}>{windowLabel(win)}</span>
      <span style={{ flex: 1, height: 4, borderRadius: 2, background: 'var(--ink-700)', overflow: 'hidden' }}>
        <span style={{ display: 'block', height: '100%', width: `${used}%`, borderRadius: 2, background: quotaTone(used) }} />
      </span>
      <span style={{ font: '400 10px/1 var(--font-mono)', color: 'var(--fg-faint)', whiteSpace: 'nowrap' }}>{countdown}</span>
    </div>
  )
}

function ProviderRow({ quota, now }: { quota: ProviderQuota; now: number }) {
  const meta = providerMeta(quota)
  const unavailable = quota.status === 'unavailable'
  const primary = quota.windows?.[0]
  const asOf = quota.as_of_ms ? formatRelativeTime(new Date(quota.as_of_ms)) : null
  return (
    <div
      title={unavailable ? quota.reason : asOf ? `as of ${asOf}` : undefined}
      style={{ display: 'flex', flexDirection: 'column', gap: 5, padding: '6px 11px', opacity: unavailable ? 0.55 : 1 }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
        <span style={{ width: 8, height: 8, borderRadius: '50%', background: meta.color, flexShrink: 0 }} />
        <span style={{ flex: 1, font: '600 12px/1 var(--font-ui)', color: 'var(--fg-secondary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {meta.label}
        </span>
        {quota.status === 'stale' && (
          <span title="may be out of date — updates while the CLI runs" style={{ width: 6, height: 6, borderRadius: '50%', background: 'var(--warning)', flexShrink: 0 }} />
        )}
        {unavailable ? (
          <span style={{ font: '400 10px/1 var(--font-ui)', color: 'var(--fg-faint)' }}>not set up</span>
        ) : (
          primary && (
            <span style={{ font: '600 11px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>
              {Math.round(clampPercent(primary.used_percent))}%
            </span>
          )
        )}
      </div>
      {!unavailable && (quota.windows ?? []).map((win) => <QuotaBar key={win.id} window={win} now={now} />)}
    </div>
  )
}

export function QuotaSection() {
  const { data } = useQuotas()
  if (!data || data.providers.length === 0) {
    return null
  }
  const now = data.fetched_at_ms
  return (
    <div style={{ padding: '4px 10px 8px', borderTop: '1px solid var(--border-subtle)' }}>
      <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-faint)', padding: '8px 11px 4px' }}>Quotas</div>
      {data.providers.map((quota) => (
        <ProviderRow key={quota.provider} quota={quota} now={now} />
      ))}
    </div>
  )
}

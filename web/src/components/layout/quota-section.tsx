/* Compact provider-quota strip for the sidebar: per provider, used% bars for
   the session and weekly windows with reset countdowns. Detailed view lives on
   the Overview page (QuotasSection). */
import { Icon } from '../vael'
import { refreshQuotas, useQuotas } from '../../lib/use-quotas'
import { clampPercent, formatQuotaCurrency, formatResetCountdown, providerMeta, quotaTone, windowLabel } from '../../lib/quotas'
import { formatRelativeTime } from '../../lib/format'
import type { ProviderQuota, QuotaWindow } from '../../types/api'

function QuotaBar({ window: win, now }: { window: QuotaWindow; now: number }) {
  const used = clampPercent(win.used_percent)
  const countdown = win.resets_at ? formatResetCountdown(win.resets_at, now) : ''
  const resetTitle = win.resets_at ? `resets ${new Date(win.resets_at * 1000).toLocaleString()}` : undefined
  const label = windowLabel(win)
  return (
    <div title={resetTitle} style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
      <span title={label} style={{ font: '500 10px/1 var(--font-mono)', color: 'var(--fg-muted)', width: 48, flexShrink: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
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
  const extraBalance = quota.extra_usage
    ? formatQuotaCurrency(quota.extra_usage.balance_cents, quota.extra_usage.currency)
    : null
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
          primary ? (
            <span style={{ font: '600 11px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>
              {Math.round(clampPercent(primary.used_percent))}%
            </span>
          ) : (
            extraBalance && (
              <span style={{ font: '600 10px/1 var(--font-mono)', color: 'var(--fg-primary)', fontVariantNumeric: 'tabular-nums' }}>
                {extraBalance}
              </span>
            )
          )
        )}
      </div>
      {!unavailable && (quota.windows ?? []).map((win) => <QuotaBar key={win.id} window={win} now={now} />)}
      {!unavailable && quota.extra_usage && (
        <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
          <span style={{ font: '500 10px/1 var(--font-mono)', color: 'var(--fg-muted)', width: 48, flexShrink: 0 }}>extra</span>
          <span style={{ flex: 1, font: '400 10px/1 var(--font-ui)', color: 'var(--fg-faint)' }}>balance</span>
          <span style={{ font: '500 10px/1 var(--font-mono)', color: 'var(--fg-secondary)' }}>{extraBalance}</span>
        </div>
      )}
    </div>
  )
}

const stripStyle = { padding: '4px 10px 8px', borderTop: '1px solid var(--border-subtle)' } as const
const stripHeadingStyle = {
  font: '600 10px/1 var(--font-ui)',
  letterSpacing: '0.08em',
  textTransform: 'uppercase',
  color: 'var(--fg-faint)',
  padding: '8px 11px 4px',
} as const

export function QuotaSection() {
  const { data, error } = useQuotas()

  // Say when the fetch failed rather than quietly dropping the strip — an empty
  // sidebar otherwise reads as "no quota providers configured".
  if (!data && error) {
    return (
      <div style={stripStyle}>
        <div style={stripHeadingStyle}>Quotas</div>
        <button
          type="button"
          onClick={refreshQuotas}
          title={error}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 6,
            width: '100%',
            padding: '6px 11px',
            background: 'transparent',
            border: 'none',
            borderRadius: 'var(--radius-md)',
            color: 'var(--danger)',
            font: '500 11px/1.3 var(--font-ui)',
            textAlign: 'left',
            cursor: 'pointer',
          }}
        >
          <Icon name="alert-triangle" size={13} />
          Unavailable — retry
        </button>
      </div>
    )
  }

  if (!data || data.providers.length === 0) {
    return null
  }

  const now = data.fetched_at_ms
  return (
    <div style={stripStyle}>
      <div style={stripHeadingStyle}>Quotas</div>
      {data.providers.map((quota) => (
        <ProviderRow key={quota.provider} quota={quota} now={now} />
      ))}
    </div>
  )
}

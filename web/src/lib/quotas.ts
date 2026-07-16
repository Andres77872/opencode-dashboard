/* Pure helpers for the provider-quota views (sidebar + overview). */
import type { ProviderQuota, QuotaProviderID, QuotaWindow } from '../types/api'

export interface QuotaProviderMeta {
  label: string
  color: string
}

export const QUOTA_PROVIDER_META: Record<QuotaProviderID, QuotaProviderMeta> = {
  codex: { label: 'Codex', color: 'var(--vendor-codex)' },
  claude_code: { label: 'Claude Code', color: 'var(--vendor-claude)' },
  kimi_code: { label: 'Kimi Code', color: 'var(--vendor-kimi)' },
  minimax: { label: 'MiniMax', color: 'var(--vendor-minimax)' },
}

/**
 * Human label for a quota window derived from its actual duration, so a
 * MiniMax 4-hour interval renders "4h" even though its id is "5h".
 */
export function windowLabel(window: QuotaWindow): string {
  if (window.label?.trim()) {
    const label = window.label.trim().replace(/\s+limit$/i, '')
    if (/^weekly$/i.test(label)) return 'week'
    if (/^monthly$/i.test(label)) return 'month'
    return label
  }
  const minutes = window.window_minutes ?? 0
  if (minutes <= 0) {
    return window.id === 'weekly' ? 'week' : window.id
  }
  if (minutes % 10080 === 0) {
    return minutes === 10080 ? 'week' : `${minutes / 10080}w`
  }
  if (minutes % 1440 === 0) {
    return `${minutes / 1440}d`
  }
  if (minutes % 60 === 0) {
    return `${minutes / 60}h`
  }
  return `${minutes}m`
}

/** Compact countdown until a reset epoch, e.g. "2h 10m", "5d 4h", "resets now". */
export function formatResetCountdown(resetsAtSec: number, nowMs: number): string {
  const remainingMs = resetsAtSec * 1000 - nowMs
  if (remainingMs <= 0) {
    return 'resets now'
  }
  const totalMinutes = Math.ceil(remainingMs / 60_000)
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  if (days > 0) {
    return hours > 0 ? `${days}d ${hours}h` : `${days}d`
  }
  if (hours > 0) {
    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`
  }
  return `${minutes}m`
}

/** Full reset copy for detail cards, avoiding phrases like "resets in resets now". */
export function formatResetLabel(resetsAtSec: number, nowMs: number): string {
  const countdown = formatResetCountdown(resetsAtSec, nowMs)
  return countdown === 'resets now' ? countdown : `resets in ${countdown}`
}

/** Traffic-light tone for a used-percent value. */
export function quotaTone(usedPercent: number): string {
  if (usedPercent >= 90) {
    return 'var(--danger)'
  }
  if (usedPercent >= 70) {
    return 'var(--warning)'
  }
  return 'var(--success)'
}

/** Clamp to the displayable 0..100 range (providers may report >100 or <0). */
export function clampPercent(value: number): number {
  if (!Number.isFinite(value)) {
    return 0
  }
  return Math.min(100, Math.max(0, value))
}

export function providerMeta(quota: ProviderQuota): QuotaProviderMeta {
  return QUOTA_PROVIDER_META[quota.provider] ?? { label: quota.label || quota.provider, color: 'var(--fg-muted)' }
}

/** Deterministic compact money formatting for Kimi Code Extra Usage. */
export function formatQuotaCurrency(cents: number, currency: string): string {
  const safeCents = Number.isFinite(cents) ? cents : 0
  const amount = (safeCents / 100).toFixed(2)
  switch (currency.trim().toUpperCase()) {
    case 'USD':
      return `$${amount}`
    case 'CNY':
      return `¥${amount}`
    default:
      return `${amount} ${currency.trim().toUpperCase() || 'USD'}`
  }
}

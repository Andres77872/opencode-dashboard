import type { CostProvenance, CostStatus } from '../types/api'

const integerFormatter = new Intl.NumberFormat('en-US')
const compactIntegerFormatter = new Intl.NumberFormat('en-US', {
  notation: 'compact',
  maximumFractionDigits: 1,
})
const currencyFormatter = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
  maximumFractionDigits: 6,
})
const compactCurrencyFormatter = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  notation: 'compact',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})
const shortDateFormatter = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  timeZone: 'UTC',
})
const shortWeekdayFormatter = new Intl.DateTimeFormat('en-US', {
  weekday: 'short',
  timeZone: 'UTC',
})
const dateTimeFormatter = new Intl.DateTimeFormat('en-US', {
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
  timeZone: 'UTC',
})
const hourFormatter = new Intl.DateTimeFormat('en-US', {
  hour: '2-digit',
  hour12: false,
  timeZone: 'UTC',
})

/**
 * Intl.DateTimeFormat.prototype.format() throws RangeError ("Invalid time value")
 * on an invalid Date — unlike Date.prototype.toLocaleDateString(), which returns
 * "Invalid Date". Guard every formatter call so bad/edge timestamps can't crash a view.
 */
function safeFormat(formatter: Intl.DateTimeFormat, date: Date, fallback = '—'): string {
  return Number.isNaN(date.getTime()) ? fallback : formatter.format(date)
}

export function isHourlyDate(value: string): boolean {
  return value.includes('T') && value.endsWith('Z')
}

export function formatHour(value: string): string {
  return safeFormat(hourFormatter, new Date(value))
}

export function formatInteger(value: number) {
  return integerFormatter.format(value)
}

export function formatCompactInteger(value: number) {
  if (Math.abs(value) < 1000) {
    return formatInteger(value)
  }

  return compactIntegerFormatter.format(value)
}

export function formatCurrency(value: number) {
  if (value == null || !Number.isFinite(value)) {
    return currencyFormatter.format(0)
  }
  return currencyFormatter.format(value)
}

export function formatCompactCurrency(value: number) {
  if (value == null || !Number.isFinite(value)) {
    return formatCurrency(0)
  }
  if (Math.abs(value) < 1000000) {
    return formatCurrency(value)
  }

  return compactCurrencyFormatter.format(value)
}

export function getCostStatus(status?: CostStatus, provenance?: CostProvenance): CostStatus | undefined {
  return status ?? provenance?.status
}

export function isMissingCost(status?: CostStatus, provenance?: CostProvenance): boolean {
  return getCostStatus(status, provenance) === 'missing'
}

export function formatCurrencyWithProvenance(value: number, status?: CostStatus, provenance?: CostProvenance) {
  const effectiveStatus = getCostStatus(status, provenance)

  if (effectiveStatus === 'missing') {
    return 'Unknown'
  }

  const formatted = formatCurrency(value)

  if (effectiveStatus === 'approximate' || effectiveStatus === 'estimated_api_equivalent') {
    return `≈ ${formatted}`
  }

  return formatted
}

export function formatCompactCurrencyWithProvenance(value: number, status?: CostStatus, provenance?: CostProvenance) {
  const effectiveStatus = getCostStatus(status, provenance)

  if (effectiveStatus === 'missing') {
    return 'Unknown'
  }

  const formatted = formatCompactCurrency(value)

  if (effectiveStatus === 'approximate' || effectiveStatus === 'estimated_api_equivalent') {
    return `≈ ${formatted}`
  }

  return formatted
}

export function formatCostProvenance(status?: CostStatus, provenance?: CostProvenance) {
  const effectiveStatus = getCostStatus(status, provenance)

  switch (effectiveStatus) {
    case 'reported':
      return 'reported cost'
    case 'computed':
      return provenance?.pricing_snapshot_id
        ? `computed from pricing snapshot ${provenance.pricing_snapshot_id}`
        : 'computed cost'
    case 'approximate':
      return provenance?.note ?? 'approximate cost'
    case 'estimated_api_equivalent':
      return provenance?.note ?? 'estimated API-equivalent cost; not actual subscription spend'
    case 'mixed':
      return provenance?.note ?? 'mixed cost provenance'
    case 'missing':
      return provenance?.note ?? 'cost unavailable'
    default:
      return null
  }
}

export function formatShortDate(value: string) {
  if (isHourlyDate(value)) {
    return formatHour(value)
  }
  return safeFormat(shortDateFormatter, new Date(`${value}T00:00:00Z`))
}

export function formatShortWeekday(value: string) {
  // Hourly ISO timestamps are already full dates; plain YYYY-MM-DD needs a time suffix.
  const date = isHourlyDate(value) ? new Date(value) : new Date(`${value}T00:00:00Z`)
  return safeFormat(shortWeekdayFormatter, date)
}

export function formatTokenCount(value: number) {
  return formatCompactInteger(value)
}

export function formatDateTime(value: string | Date) {
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return `${dateTimeFormatter.format(date)} UTC`
}

export function formatPercentage(value: number) {
  return `${Math.round(value)}%`
}

export function formatRelativeTime(value: Date | null) {
  if (!value) {
    return 'Awaiting first sync'
  }

  const deltaMs = Date.now() - value.getTime()
  const deltaSeconds = Math.max(0, Math.round(deltaMs / 1000))

  if (deltaSeconds < 10) {
    return 'Just now'
  }

  if (deltaSeconds < 60) {
    return `${deltaSeconds}s ago`
  }

  const deltaMinutes = Math.round(deltaSeconds / 60)
  if (deltaMinutes < 60) {
    return `${deltaMinutes}m ago`
  }

  const deltaHours = Math.round(deltaMinutes / 60)
  if (deltaHours < 24) {
    return `${deltaHours}h ago`
  }

  const deltaDays = Math.round(deltaHours / 24)
  return `${deltaDays}d ago`
}

export function safeDivide(numerator: number, denominator: number) {
  if (denominator === 0) {
    return 0
  }

  return numerator / denominator
}

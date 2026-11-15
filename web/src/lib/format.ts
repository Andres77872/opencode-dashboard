import type { CostProvenance, CostStatus } from '../types/api'

const integerFormatter = new Intl.NumberFormat('en-US')
const compactIntegerFormatter = new Intl.NumberFormat('en-US', {
  notation: 'compact',
  maximumFractionDigits: 1,
})
// Money is shown at the precision a reader can act on: cents from a dollar
// up, and two significant digits below that, so a fraction-of-a-cent request
// cost does not collapse to $0.00 while a column of small amounts stays even
// ($0.32, $0.031, $0.0012). The exact six-decimal figure stays available
// through formatCurrencyExact for tooltips.
const MIN_CURRENCY_DIGITS = 2
const MAX_CURRENCY_DIGITS = 6
const currencyFormatters = [2, 3, 4, 5, 6].map(
  (digits) =>
    new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency: 'USD',
      minimumFractionDigits: MIN_CURRENCY_DIGITS,
      maximumFractionDigits: digits,
    }),
)
const exactCurrencyFormatter = currencyFormatters[MAX_CURRENCY_DIGITS - MIN_CURRENCY_DIGITS]
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

function currencyFormatterFor(value: number): Intl.NumberFormat {
  const magnitude = Math.abs(value)
  if (magnitude === 0 || magnitude >= 1) return currencyFormatters[0]
  // Decimals needed to show two significant digits of a sub-dollar amount.
  const digits = Math.ceil(-Math.log10(magnitude)) + 1
  const clamped = Math.min(MAX_CURRENCY_DIGITS, Math.max(MIN_CURRENCY_DIGITS, digits))
  return currencyFormatters[clamped - MIN_CURRENCY_DIGITS]
}

export function formatCurrency(value: number) {
  if (value == null || !Number.isFinite(value)) {
    return currencyFormatters[0].format(0)
  }
  return currencyFormatterFor(value).format(value)
}

/** Full stored precision (up to six decimals), for tooltips and audits. */
export function formatCurrencyExact(value: number) {
  if (value == null || !Number.isFinite(value)) {
    return exactCurrencyFormatter.format(0)
  }
  return exactCurrencyFormatter.format(value)
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

const thousandsCurrencyFormatter = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', notation: 'compact', maximumFractionDigits: 2 })
const wholeDollarFormatter = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })

/**
 * Money for a chart axis, which has room for about seven characters:
 * "$1,250.00" would lose its leading digits. Amounts from a thousand up are
 * shortened ("$1.25K") and round hundreds drop their cents ("$500"), which is
 * what axis ticks are.
 */
export function formatAxisCurrency(value: number) {
  if (value == null || !Number.isFinite(value)) return formatCurrency(0)
  const magnitude = Math.abs(value)
  if (magnitude >= 1000) return thousandsCurrencyFormatter.format(value)
  if (magnitude >= 100 && Number.isInteger(value)) return wholeDollarFormatter.format(value)
  return formatCurrency(value)
}

export function getCostStatus(status?: CostStatus, provenance?: CostProvenance): CostStatus | undefined {
  return status ?? provenance?.status
}

export function isMissingCost(status?: CostStatus, provenance?: CostProvenance): boolean {
  return getCostStatus(status, provenance) === 'missing'
}

/**
 * The label for a spend figure: "Est. cost" when the amount is an estimate
 * (API-equivalent or approximate pricing), "Cost" otherwise. One wording for
 * every stat card, column header and metric toggle.
 */
export function getCostLabel(status?: CostStatus, provenance?: CostProvenance) {
  const effectiveStatus = getCostStatus(status, provenance)
  return effectiveStatus === 'approximate' || effectiveStatus === 'estimated_api_equivalent' ? 'Est. cost' : 'Cost'
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
      return provenance?.note ?? 'estimated API-equivalent USD cost; not actual billed spend'
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
    return 'never'
  }

  const deltaMs = Date.now() - value.getTime()
  const deltaSeconds = Math.max(0, Math.round(deltaMs / 1000))

  if (deltaSeconds < 10) {
    return 'just now'
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

/** "1 session" / "3 sessions" — counts are formatted, the noun agrees. */
export function formatCount(count: number, singular: string, plural = `${singular}s`) {
  return `${formatInteger(count)} ${count === 1 ? singular : plural}`
}

/** Elapsed time between two instants as "3d 4h" / "2h 30m" / "4m 12s" / "9s". */
export function formatDuration(start: string | number | Date, end: string | number | Date) {
  const ms = new Date(end).getTime() - new Date(start).getTime()
  if (!Number.isFinite(ms)) return '—'
  const total = Math.max(0, Math.round(ms / 1000))
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  if (minutes > 0) return `${minutes}m ${total % 60}s`
  return `${total}s`
}

/** Leading characters of an opaque id for display. Pair it with the full id in a `title`. */
export function formatShortId(id: string, length = 12) {
  return id.length > length ? `${id.slice(0, length)}…` : id
}

/** A part's share of a whole: "42%", "<1%" for a non-zero sliver, "—" when there is no whole. */
export function formatShare(part: number, whole: number) {
  if (!(whole > 0)) return '—'
  const percent = (part / whole) * 100
  if (percent > 0 && percent < 1) return '<1%'
  return `${Math.round(percent)}%`
}

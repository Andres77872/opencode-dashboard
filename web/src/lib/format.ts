const integerFormatter = new Intl.NumberFormat('en-US')
const compactIntegerFormatter = new Intl.NumberFormat('en-US', {
  notation: 'compact',
  maximumFractionDigits: 1,
})
const currencyFormatter = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
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

export function isHourlyDate(value: string): boolean {
  return value.includes('T') && value.endsWith('Z')
}

export function formatHour(value: string): string {
  const date = new Date(value)
  return hourFormatter.format(date)
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
  return currencyFormatter.format(value)
}

export function formatCompactCurrency(value: number) {
  if (Math.abs(value) < 1000) {
    return formatCurrency(value)
  }

  return compactCurrencyFormatter.format(value)
}

export function formatShortDate(value: string) {
  if (isHourlyDate(value)) {
    return formatHour(value)
  }
  return shortDateFormatter.format(new Date(`${value}T00:00:00Z`))
}

export function formatShortWeekday(value: string) {
  return shortWeekdayFormatter.format(new Date(`${value}T00:00:00Z`))
}

export function formatTokenCount(value: number) {
  return formatCompactInteger(value)
}

export function formatDateTime(value: string | Date) {
  const date = value instanceof Date ? value : new Date(value)
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

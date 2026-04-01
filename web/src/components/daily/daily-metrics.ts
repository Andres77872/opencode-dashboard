import { formatCompactCurrency, formatCompactInteger, formatCurrency, formatInteger, formatTokenCount } from '../../lib/format'
import { getTokenTotal } from '../../lib/token-breakdown'
import type { DayStats } from '../../types/api'

export type DailyMetric = 'cost' | 'requests' | 'tokens'

export const dailyMetricOptions = [
  { label: 'Cost', value: 'cost' },
  { label: 'Requests', value: 'requests' },
  { label: 'Tokens', value: 'tokens' },
] as const satisfies ReadonlyArray<{ label: string; value: DailyMetric }>

const DAILY_METRIC_META: Record<
  DailyMetric,
  {
    chartDescription: string
    chartTitle: string
    label: string
  }
> = {
  cost: {
    chartDescription: 'Daily spend stays calendar-based so zero-filled days remain visible in the window.',
    chartTitle: 'Daily spend',
    label: 'Cost',
  },
  requests: {
    chartDescription: 'Requests are backed by the API `messages` count, which is the available daily throughput signal today.',
    chartTitle: 'Daily request volume',
    label: 'Requests',
  },
  tokens: {
    chartDescription: 'Token mode switches to stacked bars so input, cache, output, reasoning, and cache write stay visible.',
    chartTitle: 'Daily token distribution',
    label: 'Tokens',
  },
}

export function getDailyMetricMeta(metric: DailyMetric) {
  return DAILY_METRIC_META[metric]
}

export function getDailyMetricValue(day: DayStats, metric: DailyMetric) {
  switch (metric) {
    case 'requests':
      return day.messages
    case 'tokens':
      return getTokenTotal(day.tokens)
    default:
      return day.cost
  }
}

export function formatDailyMetricValue(metric: DailyMetric, value: number, compact = false) {
  switch (metric) {
    case 'cost':
      return compact ? formatCompactCurrency(value) : formatCurrency(value)
    case 'requests':
      return compact ? formatCompactInteger(value) : formatInteger(value)
    default:
      return formatTokenCount(value)
  }
}

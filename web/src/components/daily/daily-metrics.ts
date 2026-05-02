import { formatCompactCurrency, formatCompactInteger, formatCurrency, formatInteger, formatTokenCount } from '../../lib/format'
import { getTokenTotal } from '../../lib/token-breakdown'
import type { DayStats } from '../../types/api'

export type DailyMetric = 'cost' | 'requests' | 'tokens'

export const dailyMetricOptions = [
  { label: 'Cost', value: 'cost' },
  { label: 'Messages', value: 'requests' },
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
    chartDescription: 'Zero-filled days stay visible so gaps in the window are honest.',
    chartTitle: 'Daily spend',
    label: 'Cost',
  },
  requests: {
    chartDescription: 'Message count per calendar day — the available daily throughput signal.',
    chartTitle: 'Daily messages',
    label: 'Messages',
  },
  tokens: {
    chartDescription: 'Volume (absolute) or share (normalized) — toggle chart mode above the graph.',
    chartTitle: 'Daily token mix',
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

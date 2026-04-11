import {
  formatCompactCurrency,
  formatCompactInteger,
  formatCurrency,
  formatInteger,
  formatPercentage,
  formatTokenCount,
} from '../../lib/format'
import type { ModelEntry } from '../../types/api'

export type ModelsMetric = 'cost' | 'sessions' | 'messages' | 'tokens'

export interface EnrichedModelRow extends ModelEntry {
  totalTokens: number
  avgCostPerMessage: number
  costShare: number
}

export const modelsMetricOptions = [
  { label: 'Cost', value: 'cost' },
  { label: 'Sessions', value: 'sessions' },
  { label: 'Messages', value: 'messages' },
  { label: 'Tokens', value: 'tokens' },
] as const satisfies ReadonlyArray<{ label: string; value: ModelsMetric }>

const MODELS_METRIC_META: Record<
  ModelsMetric,
  {
    description: string
    label: string
    progressLabel: string
  }
> = {
  cost: {
    description: 'Progress bars show cost share — how much each model contributes to total spend.',
    label: 'Cost',
    progressLabel: 'Cost share',
  },
  sessions: {
    description: 'Progress bars show session share — how many coding sessions each model touched.',
    label: 'Sessions',
    progressLabel: 'Session share',
  },
  messages: {
    description: 'Progress bars show message share — assistant response volume per model. One API request may produce multiple messages.',
    label: 'Messages',
    progressLabel: 'Message share',
  },
  tokens: {
    description: 'Progress bars show token share — cumulative input, output, reasoning, and cache tokens per model.',
    label: 'Tokens',
    progressLabel: 'Token share',
  },
}

export function getModelsMetricMeta(metric: ModelsMetric) {
  return MODELS_METRIC_META[metric]
}

export function getModelsMetricValue(row: EnrichedModelRow, metric: ModelsMetric): number {
  switch (metric) {
    case 'sessions':
      return row.sessions
    case 'messages':
      return row.messages
    case 'tokens':
      return row.totalTokens
    default:
      return row.cost
  }
}

export function formatModelsMetricValue(metric: ModelsMetric, value: number, compact = false): string {
  switch (metric) {
    case 'cost':
      return compact ? formatCompactCurrency(value) : formatCurrency(value)
    case 'sessions':
      return compact ? formatCompactInteger(value) : formatInteger(value)
    case 'messages':
      return compact ? formatCompactInteger(value) : formatInteger(value)
    default:
      return formatTokenCount(value)
  }
}

export function getModelsMetricShare(row: EnrichedModelRow, metric: ModelsMetric, total: number): number {
  const value = getModelsMetricValue(row, metric)
  if (total === 0) return 0
  return (value / total) * 100
}

export function formatModelsMetricShare(share: number): string {
  return formatPercentage(share)
}

const MIN_VISIBLE_PROGRESS_PERCENT = 4

export function getProgressValue(share: number, hasValue: boolean): number {
  return Math.max(share, hasValue ? MIN_VISIBLE_PROGRESS_PERCENT : 0)
}
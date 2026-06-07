import type { DayStats, SourceOverview } from '../types/api'
import { getTokenTotal } from './token-breakdown.ts'

export type TrendMetric = 'messages' | 'cost' | 'tokens'

export function trendMetricValue(day: DayStats, metric: TrendMetric): number {
  switch (metric) {
    case 'cost':
      return day.cost
    case 'tokens':
      return getTokenTotal(day.tokens)
    default:
      return day.messages
  }
}

/** A merged trend row: a date plus one numeric column per source_id. */
export type TrendRow = { date: string } & Record<string, number | string>

/**
 * Merges each source's daily trend into a single ascending-by-date series with
 * one column per source_id, suitable for a stacked Recharts bar chart. Each
 * column holds that source's own value for the chosen metric (no cross-source
 * cost is ever summed into a single headline number).
 */
export function buildSourceTrendData(sources: SourceOverview[], metric: TrendMetric): TrendRow[] {
  const byDate = new Map<string, TrendRow>()
  for (const src of sources) {
    for (const day of src.trend ?? []) {
      const row = byDate.get(day.date) ?? { date: day.date }
      const prev = (row[src.source_id] as number | undefined) ?? 0
      row[src.source_id] = prev + trendMetricValue(day, metric)
      byDate.set(day.date, row)
    }
  }
  return [...byDate.values()].sort((a, b) => String(a.date).localeCompare(String(b.date)))
}

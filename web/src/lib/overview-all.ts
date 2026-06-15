import type { DayStats, OverviewStats, SourceOverview } from '../types/api'
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

/** The selected metric's scalar from a source's roll-up totals (s.overview).
    Mirrors trendMetricValue, but for the per-source totals used by the donut. */
export function overviewMetricValue(o: OverviewStats, metric: TrendMetric): number {
  switch (metric) {
    case 'cost':
      return o.cost
    case 'tokens':
      return getTokenTotal(o.tokens)
    default:
      return o.messages
  }
}

export interface SourceMetricShare {
  source: SourceOverview
  value: number
  /** 0..1 fraction of the selected metric across all sources. */
  share: number
}

/** Per-source value + share for the selected metric, used by the "Usage by
    source" donut + legend. Cost share is computed here client-side because the
    backend exposes token_share/message_share only (no cost_share); centralizing
    it also keeps the divide-by-zero guard in one place. */
export function buildSourceMetricShares(sources: SourceOverview[], metric: TrendMetric): SourceMetricShare[] {
  const vals = sources.map((s) => ({ source: s, value: overviewMetricValue(s.overview, metric) }))
  const total = vals.reduce((a, b) => a + b.value, 0)
  return vals.map((v) => ({ ...v, share: total > 0 ? v.value / total : 0 }))
}

/** Per-day totals combined across sources. Cost is intentionally excluded:
    costs are reported per source and are never combined into one number. */
export interface CombinedDayTotals {
  date: string
  tokens: number
  sessions: number
  messages: number
}

/** Merges each source's daily trend into one ascending-by-date series of
    combined totals, used by the KPI sparklines. */
export function buildCombinedDailyTotals(sources: SourceOverview[]): CombinedDayTotals[] {
  const byDate = new Map<string, CombinedDayTotals>()
  for (const src of sources) {
    for (const day of src.trend ?? []) {
      const row = byDate.get(day.date) ?? { date: day.date, tokens: 0, sessions: 0, messages: 0 }
      row.tokens += getTokenTotal(day.tokens)
      row.sessions += day.sessions
      row.messages += day.messages
      byDate.set(day.date, row)
    }
  }
  return [...byDate.values()].sort((a, b) => a.date.localeCompare(b.date))
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

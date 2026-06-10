import type { DimensionDayStats } from '../types/api'

/**
 * Groups per-day per-model dimension rows (daily?dimension=model) by date,
 * sorting each day's models by message count descending so the daily
 * breakdown table can render the busiest models first.
 */
export function groupModelDaysByDate(days: DimensionDayStats[] | undefined): Map<string, DimensionDayStats[]> {
  const map = new Map<string, DimensionDayStats[]>()
  for (const row of days ?? []) {
    const list = map.get(row.date)
    if (list) list.push(row)
    else map.set(row.date, [row])
  }
  for (const list of map.values()) {
    list.sort((a, b) => b.messages - a.messages)
  }
  return map
}

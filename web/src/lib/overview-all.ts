import type {
  CostProvenance,
  CostStatus,
  DayStats,
  DimensionDayStats,
  ModelEntry,
  OverviewStats,
  SourceID,
  SourceOverview,
} from '../types/api'
import { getTokenTotal } from './token-breakdown.ts'

export type TrendMetric = 'messages' | 'cost' | 'tokens'

export type UsageGrouping = 'source' | 'model'

export interface UsageMetricCopy {
  label: string
  noun: string
  explanation?: string
}

/** User-facing vocabulary for a metric in its active grouping. Model rows only
    contain assistant messages that can be attributed to a model, while source
    overview rows count every recorded message role. Calling both "Messages"
    would imply a shared denominator that the data does not have. */
export function usageMetricCopy(metric: TrendMetric, grouping: UsageGrouping): UsageMetricCopy {
  if (metric === 'tokens' && grouping === 'model') {
    return {
      label: 'Tokens',
      noun: 'tokens',
      explanation: 'Model Tokens use additive per-step usage when a source provides it. Source Tokens use recorded message snapshots, so OpenCode totals can differ.',
    }
  }
  if (metric === 'messages' && grouping === 'model') {
    return {
      label: 'Model calls',
      noun: 'model calls',
      explanation: 'Model calls count assistant messages attributed to a model. Source Messages counts every recorded message, so the totals are intentionally different.',
    }
  }
  if (metric === 'messages') return { label: 'Messages', noun: 'messages' }
  if (metric === 'cost') {
    return {
      label: 'Cost',
      noun: 'cost',
      explanation: 'Combined cost totals can mix reported spend with computed or estimated API-equivalent values. Per-item provenance is shown in the breakdown.',
    }
  }
  return { label: 'Tokens', noun: 'tokens' }
}

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

/** The selected metric's scalar from a model total. */
export function modelMetricValue(model: ModelEntry, metric: TrendMetric): number {
  switch (metric) {
    case 'cost':
      return model.cost
    case 'tokens':
      return getTokenTotal(model.tokens)
    default:
      return model.messages
  }
}

/** The selected metric's scalar from a per-model daily row. */
export function dimensionMetricValue(day: DimensionDayStats, metric: TrendMetric): number {
  switch (metric) {
    case 'cost':
      return day.cost
    case 'tokens':
      return getTokenTotal(day.tokens)
    default:
      return day.messages
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

const OTHER_MODELS_ID = '__other_models__'

interface RawModelGroup {
  id: string
  scopeKey: string
  sourceId?: SourceID
  modelId: string
  providerIds: string[]
  entries: ModelEntry[]
  trend: DimensionDayStats[]
  value: number
  costStatus?: CostStatus
  costProvenance?: CostProvenance
}

export interface ModelMetricSeries {
  id: string
  sourceId?: SourceID
  modelId: string
  providerIds: string[]
  value: number
  /** 0..1 fraction of the selected metric across every returned model. */
  share: number
  costStatus?: CostStatus
  costProvenance?: CostProvenance
  isOther: boolean
  memberCount: number
}

export interface ModelMetricBreakdown {
  series: ModelMetricSeries[]
  trend: TrendRow[]
  total: number
}

function normalizeModelId(value: string): string {
  return value.trim() || 'Unknown model'
}

function modelScopeKey(sourceId: SourceID | undefined, modelId: string): string {
  return JSON.stringify([sourceId ?? 'unknown', normalizeModelId(modelId)])
}

/** Stable identity for a source-scoped model, including provider metadata when
    the source reports it. JSON encoding avoids delimiter collisions in IDs. */
export function modelUsageKey(sourceId: SourceID | undefined, providerIds: string[], modelId: string): string {
  return JSON.stringify([sourceId ?? 'unknown', [...providerIds].sort(), normalizeModelId(modelId)])
}

function costMetadata(items: Array<Pick<ModelEntry | DimensionDayStats, 'cost_status' | 'cost_provenance'>>): {
  status?: CostStatus
  provenance?: CostProvenance
} {
  const statuses = items
    .map((item) => item.cost_status ?? item.cost_provenance?.status)
    .filter((status): status is CostStatus => Boolean(status))
  if (statuses.length === 0) return {}

  const status = statuses.every((candidate) => candidate === statuses[0]) ? statuses[0] : 'mixed'
  return {
    status,
    provenance: items.length === 1 ? items[0].cost_provenance : undefined,
  }
}

/**
 * Builds a bounded, chart-ready model breakdown from the complete model totals
 * and daily rows returned by overview/all?dimension=model.
 *
 * Models stay scoped to their source so two agents using the same public model
 * are not conflated (especially important for cost provenance). If cardinality
 * exceeds maxSeries, lower-ranked models are rolled into one honest "Other"
 * bucket in both the donut and every daily bar. Model totals are authoritative;
 * daily rows are used as a fallback only when the totals dimension failed.
 */
export function buildModelMetricBreakdown(
  models: ModelEntry[],
  modelTrend: DimensionDayStats[],
  metric: TrendMetric,
  maxSeries = 8,
): ModelMetricBreakdown {
  const groups = new Map<string, Omit<RawModelGroup, 'id' | 'providerIds' | 'value' | 'costStatus' | 'costProvenance'> & { providers: Set<string> }>()

  const ensureGroup = (sourceId: SourceID | undefined, modelIdValue: string) => {
    const modelId = normalizeModelId(modelIdValue)
    const scopeKey = modelScopeKey(sourceId, modelId)
    let group = groups.get(scopeKey)
    if (!group) {
      group = { scopeKey, sourceId, modelId, providers: new Set(), entries: [], trend: [] }
      groups.set(scopeKey, group)
    }
    return group
  }

  for (const model of models) {
    const group = ensureGroup(model.source_id, model.model_id)
    const provider = model.provider_id.trim()
    if (provider) group.providers.add(provider)
    group.entries.push(model)
  }

  for (const day of modelTrend) {
    const group = ensureGroup(day.source_id, day.dimension_key)
    const provider = day.provider_id?.trim()
    if (provider) group.providers.add(provider)
    group.trend.push(day)
  }

  const ranked: RawModelGroup[] = [...groups.values()].map((group) => {
    const providerIds = [...group.providers].sort()
    const costItems = group.entries.length > 0 ? group.entries : group.trend
    const metadata = costMetadata(costItems)
    return {
      ...group,
      id: modelUsageKey(group.sourceId, providerIds, group.modelId),
      providerIds,
      value: group.entries.length > 0
        ? group.entries.reduce((sum, model) => sum + modelMetricValue(model, metric), 0)
        : group.trend.reduce((sum, day) => sum + dimensionMetricValue(day, metric), 0),
      costStatus: metadata.status,
      costProvenance: metadata.provenance,
    }
  }).filter((group) => group.value > 0 || group.trend.some((day) => dimensionMetricValue(day, metric) > 0))

  ranked.sort((a, b) => b.value - a.value || a.modelId.localeCompare(b.modelId) || a.id.localeCompare(b.id))

  const boundedMax = Math.max(2, Math.floor(maxSeries))
  const keepCount = ranked.length > boundedMax ? boundedMax - 1 : ranked.length
  const visible = ranked.slice(0, keepCount)
  const remainder = ranked.slice(keepCount)
  const total = ranked.reduce((sum, group) => sum + group.value, 0)

  const series: ModelMetricSeries[] = visible.map((group) => ({
    id: group.id,
    sourceId: group.sourceId,
    modelId: group.modelId,
    providerIds: group.providerIds,
    value: group.value,
    share: total > 0 ? group.value / total : 0,
    costStatus: group.costStatus,
    costProvenance: group.costProvenance,
    isOther: false,
    memberCount: 1,
  }))

  if (remainder.length > 0) {
    const remainderStatuses = remainder
      .map((group) => group.costStatus)
      .filter((status): status is CostStatus => Boolean(status))
    const remainderStatus = remainderStatuses.length === 0
      ? undefined
      : remainderStatuses.every((candidate) => candidate === remainderStatuses[0])
        ? remainderStatuses[0]
        : 'mixed'
    const value = remainder.reduce((sum, group) => sum + group.value, 0)
    series.push({
      id: OTHER_MODELS_ID,
      modelId: 'Other models',
      providerIds: [],
      value,
      share: total > 0 ? value / total : 0,
      costStatus: remainderStatus,
      isOther: true,
      memberCount: remainder.length,
    })
  }

  const visibleIds = new Set(visible.map((group) => group.id))
  const byDate = new Map<string, TrendRow>()
  for (const group of ranked) {
    const targetId = visibleIds.has(group.id) ? group.id : OTHER_MODELS_ID
    for (const day of group.trend) {
      const row = byDate.get(day.date) ?? { date: day.date }
      row[targetId] = Number(row[targetId] ?? 0) + dimensionMetricValue(day, metric)
      byDate.set(day.date, row)
    }
  }

  return {
    series,
    trend: [...byDate.values()].sort((a, b) => String(a.date).localeCompare(String(b.date))),
    total,
  }
}

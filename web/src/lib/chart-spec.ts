/**
 * Parses the ```chart / ```plot artifact fences the analytics assistant writes
 * into a canonical, render-ready chart model.
 *
 * The contract deliberately has two halves with different temperaments:
 *
 *  - **Liberal on the way in.** A model writes JSON from memory under a token
 *    budget, so the accepted surface covers the shapes it actually produces:
 *    `data: [{label, value}]`, `series: [{name, values}]`, tuple rows, a bare
 *    `values` array, numbers written as `"1,234"` or `"$0.42"`, and a type given
 *    either in the fence info string or in the object.
 *  - **Strict on the way out.** Everything that survives is a `ChartSpec` with
 *    equal-length series, finite numbers, bounded sizes, and sanitized text, so
 *    the renderer is a pure projection with no defensive branches.
 *
 * Two project-specific rules are load-bearing:
 *
 *  - `null` is *unknown*, never zero. The dashboard's whole accounting model
 *    refuses to turn missing evidence into a zero, and a chart is the easiest
 *    place to accidentally do it, so nulls survive parsing and are rendered as
 *    gaps that the table view names.
 *  - Colors are never taken from the spec. Series are painted from the design
 *    system's fixed categorical order, so a model cannot produce an
 *    unreadable or inaccessible palette. Any `color` key is ignored.
 *
 * A failed parse is not an exception: `parseChartSpec` returns a diagnostic and
 * the renderer falls back to showing the fence as code, so the numbers the model
 * computed are never lost to a syntax slip.
 */

import { formatCompactCurrency, formatCompactInteger, formatCurrency, formatInteger } from './format.ts'

export type ChartKind = 'bar' | 'column' | 'line' | 'area' | 'donut' | 'heatmap'

export type ChartUnit = 'tokens' | 'usd' | 'count' | 'percent' | 'ms' | 'seconds' | 'none'

export interface ChartSeries {
  name: string
  /** One value per label. `null` means unknown evidence, never zero. */
  values: (number | null)[]
}

export interface ChartSpec {
  kind: ChartKind
  /** Bars/columns share a baseline instead of sitting side by side. */
  stacked: boolean
  title: string | null
  unit: ChartUnit
  labels: string[]
  series: ChartSeries[]
  /** Provenance line: the source and period the numbers came from. */
  source: string | null
  period: string | null
  /** Free-text qualifier, typically cost provenance or a coverage caveat. */
  note: string | null
  /** True when any value is unknown, so the renderer can disclose it once. */
  hasUnknown: boolean
}

export interface ChartSpecError {
  ok: false
  /** One sentence naming what is wrong, safe to show in the panel. */
  error: string
  /** How to write it correctly, when a concrete correction exists. */
  hint: string | null
}

export type ChartSpecResult = { ok: true; spec: ChartSpec } | ChartSpecError

/* ------------------------------------------------------------------ *
 * Limits
 * ------------------------------------------------------------------ */

/** The categorical token ceiling. A 9th series has no distinguishable hue. */
export const MAX_SERIES = 8
/** Enough for 120 daily buckets or an hourly day; beyond it a chart is unreadable. */
export const MAX_POINTS = 200
const MAX_CELLS = 1200
const MAX_DONUT_SLICES = 8
const MAX_TITLE_CHARS = 160
const MAX_NOTE_CHARS = 320
const MAX_LABEL_CHARS = 120
const MAX_NAME_CHARS = 64
const MAX_META_CHARS = 64

/* ------------------------------------------------------------------ *
 * Fence info strings
 * ------------------------------------------------------------------ */

const CHART_FENCE_NAMES = new Set(['chart', 'plot'])

/** Splits a fence info string into its language and optional arguments. */
function splitInfo(info: string): string[] {
  return info
    .trim()
    .toLowerCase()
    .split(/[\s|:,]+/)
    .filter((part) => part !== '')
}

/**
 * True when a fence info string opens a chart artifact. Both `chart` and `plot`
 * are accepted, with or without a type argument (```chart bar, ```plot|line).
 */
export function isChartFence(info: string | null): boolean {
  if (info === null) return false
  const parts = splitInfo(info)
  return parts.length > 0 && CHART_FENCE_NAMES.has(parts[0])
}

/** The type argument of a chart fence (```chart bar → "bar"), if present. */
function fenceKindHint(info: string | null): string | null {
  if (info === null) return null
  const parts = splitInfo(info)
  return parts.length > 1 ? parts.slice(1).join('-') : null
}

/* ------------------------------------------------------------------ *
 * Kind and unit vocabularies
 * ------------------------------------------------------------------ */

interface KindResolution {
  kind: ChartKind
  stacked: boolean
}

/**
 * Chart-type aliases. Horizontal `bar` and vertical `column` are distinct forms
 * — in a 420px panel a ranking of long model IDs is only readable horizontally —
 * so the vocabulary keeps them separate and maps every common synonym onto one.
 */
const KIND_ALIASES: Record<string, KindResolution> = {
  bar: { kind: 'bar', stacked: false },
  barh: { kind: 'bar', stacked: false },
  hbar: { kind: 'bar', stacked: false },
  'horizontal-bar': { kind: 'bar', stacked: false },
  ranking: { kind: 'bar', stacked: false },
  column: { kind: 'column', stacked: false },
  columns: { kind: 'column', stacked: false },
  'vertical-bar': { kind: 'column', stacked: false },
  histogram: { kind: 'column', stacked: false },
  line: { kind: 'line', stacked: false },
  lines: { kind: 'line', stacked: false },
  spline: { kind: 'line', stacked: false },
  trend: { kind: 'line', stacked: false },
  area: { kind: 'area', stacked: false },
  donut: { kind: 'donut', stacked: false },
  doughnut: { kind: 'donut', stacked: false },
  pie: { kind: 'donut', stacked: false },
  share: { kind: 'donut', stacked: false },
  heatmap: { kind: 'heatmap', stacked: false },
  matrix: { kind: 'heatmap', stacked: false },
  stacked: { kind: 'column', stacked: true },
  'stacked-bar': { kind: 'bar', stacked: true },
  'stacked-bars': { kind: 'bar', stacked: true },
  'stacked-column': { kind: 'column', stacked: true },
  'stacked-columns': { kind: 'column', stacked: true },
  'stacked-area': { kind: 'area', stacked: true },
}

export const SUPPORTED_CHART_KINDS = [
  'bar',
  'column',
  'line',
  'area',
  'stacked-bar',
  'stacked-column',
  'donut',
  'heatmap',
] as const

const UNIT_ALIASES: Record<string, ChartUnit> = {
  token: 'tokens',
  tokens: 'tokens',
  usd: 'usd',
  dollars: 'usd',
  dollar: 'usd',
  cost: 'usd',
  currency: 'usd',
  $: 'usd',
  count: 'count',
  counts: 'count',
  requests: 'count',
  request: 'count',
  messages: 'count',
  sessions: 'count',
  invocations: 'count',
  calls: 'count',
  percent: 'percent',
  percentage: 'percent',
  '%': 'percent',
  ratio: 'percent',
  ms: 'ms',
  milliseconds: 'ms',
  s: 'seconds',
  sec: 'seconds',
  secs: 'seconds',
  seconds: 'seconds',
  duration: 'seconds',
  none: 'none',
  '': 'none',
}

function resolveKind(raw: unknown, fallback: string | null): KindResolution | null {
  const candidates = [raw, fallback]
  for (const candidate of candidates) {
    if (typeof candidate !== 'string') continue
    const key = candidate.trim().toLowerCase().replace(/[\s_]+/g, '-')
    const found = KIND_ALIASES[key]
    if (found) return { ...found }
  }
  return null
}

function resolveUnit(raw: unknown): ChartUnit {
  if (typeof raw !== 'string') return 'none'
  const key = raw.trim().toLowerCase()
  return UNIT_ALIASES[key] ?? 'none'
}

/* ------------------------------------------------------------------ *
 * Scalar coercion
 * ------------------------------------------------------------------ */

/**
 * Code points that must never reach the panel: C0/C1 controls, and the
 * zero-width and bidirectional-override characters that could reorder or hide
 * neighbouring text. Written as ranges rather than a regex so the source file
 * itself stays free of the characters it defends against.
 */
function isDisplayHostile(code: number): boolean {
  return (
    code < 0x20 ||
    (code >= 0x7f && code <= 0x9f) ||
    code === 0xad ||
    (code >= 0x200b && code <= 0x200f) ||
    code === 0x2028 ||
    code === 0x2029 ||
    (code >= 0x202a && code <= 0x202e) ||
    (code >= 0x2060 && code <= 0x2064) ||
    (code >= 0x2066 && code <= 0x2069) ||
    code === 0xfeff
  )
}

/**
 * Neutralizes display-hostile characters and collapses runs of whitespace, then
 * bounds the length. React escapes markup already; this is about model output
 * that could otherwise reorder or hide neighbouring text in the panel.
 */
export function sanitizeChartText(value: string, max: number): string {
  let scrubbed = ''
  for (const character of value) {
    scrubbed += isDisplayHostile(character.codePointAt(0) ?? 0) ? ' ' : character
  }
  const cleaned = scrubbed.replace(/\s+/g, ' ').trim()
  return cleaned.length > max ? `${cleaned.slice(0, max - 1)}…` : cleaned
}

function optionalText(value: unknown, max: number): string | null {
  if (typeof value !== 'string') return null
  const cleaned = sanitizeChartText(value, max)
  return cleaned === '' ? null : cleaned
}

const UNKNOWN_TOKENS = new Set(['', 'unknown', 'n/a', 'na', 'null', 'none', '-', '—', '–', '?'])

/**
 * Coerces one cell into a number, `null` for an explicit unknown, or
 * `undefined` when the value is not numeric at all (a hard error).
 *
 * Numeric strings are accepted only where the reading is unambiguous —
 * thousands separators, a currency prefix, a percent suffix. A compact form
 * such as "1.2M" is rejected on purpose: guessing its magnitude would invent
 * evidence, which is exactly what these charts must never do.
 */
function coerceValue(raw: unknown): number | null | undefined {
  if (raw === null || raw === undefined) return null
  if (typeof raw === 'number') return Number.isFinite(raw) ? raw : null
  if (typeof raw === 'boolean') return undefined
  if (typeof raw !== 'string') return undefined
  const trimmed = raw.trim()
  if (UNKNOWN_TOKENS.has(trimmed.toLowerCase())) return null
  const stripped = trimmed.replace(/[$\s,]/g, '').replace(/%$/, '')
  if (!/^[+-]?\d*\.?\d+(?:[eE][+-]?\d+)?$/.test(stripped)) return undefined
  const parsed = Number(stripped)
  return Number.isFinite(parsed) ? parsed : undefined
}

/* ------------------------------------------------------------------ *
 * Lenient JSON
 * ------------------------------------------------------------------ */

/**
 * Parses the fence body as JSON, retrying once with the repairs that show up in
 * real model output: a trailing comma, `//` comments, or prose wrapped around
 * the object. Nothing here rewrites values — a repair that could change a number
 * would defeat the point of grounding the chart in tool evidence.
 */
function parseLenientJson(source: string): unknown | undefined {
  const trimmed = source.trim()
  if (trimmed === '') return undefined
  try {
    return JSON.parse(trimmed)
  } catch {
    // fall through to the bounded repair below
  }
  const start = trimmed.indexOf('{')
  const end = trimmed.lastIndexOf('}')
  if (start < 0 || end <= start) return undefined
  const repaired = trimmed
    .slice(start, end + 1)
    .replace(/^[ \t]*\/\/.*$/gm, '')
    .replace(/,(\s*[}\]])/g, '$1')
  try {
    return JSON.parse(repaired)
  } catch {
    return undefined
  }
}

/* ------------------------------------------------------------------ *
 * Data-shape normalization
 * ------------------------------------------------------------------ */

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function firstKey(record: Record<string, unknown>, keys: string[]): unknown {
  for (const key of keys) {
    if (record[key] !== undefined) return record[key]
  }
  return undefined
}

const LABEL_KEYS = ['label', 'name', 'category', 'key', 'x', 'bucket', 'day', 'date', 'model', 'tool', 'project']
const VALUE_KEYS = ['value', 'y', 'count', 'total', 'amount', 'v']
const SERIES_VALUE_KEYS = ['values', 'data', 'points', 'y', 'series']

interface DraftSeries {
  name: string
  values: (number | null | undefined)[]
}

interface DraftData {
  labels: string[]
  series: DraftSeries[]
}

/** Reads `data: [...]` — the row-per-category shape, in each of its dialects. */
function draftFromRows(rows: unknown[]): DraftData | ChartSpecError {
  const labels: string[] = []
  const columns = new Map<string, (number | null | undefined)[]>()
  // Columns start full of nulls: a measure that only some rows carry is
  // unknown for the others, which is a gap in the chart — never a zero, and
  // never the hard error a missing array slot would otherwise raise.
  const ensure = (name: string): (number | null | undefined)[] => {
    let column = columns.get(name)
    if (!column) {
      column = new Array<number | null | undefined>(rows.length).fill(null)
      columns.set(name, column)
    }
    return column
  }

  rows.forEach((row, index) => {
    if (Array.isArray(row)) {
      // ["kimi-k2", 1234]
      labels.push(sanitizeChartText(String(row[0] ?? index + 1), MAX_LABEL_CHARS))
      ensure('value')[index] = coerceValue(row[1])
      return
    }
    if (!isRecord(row)) {
      // A bare number list inside `data` is a single unlabeled series.
      labels.push(String(index + 1))
      ensure('value')[index] = coerceValue(row)
      return
    }
    const labelKey = LABEL_KEYS.find((key) => typeof row[key] === 'string')
    const rawLabel = labelKey ? String(row[labelKey]) : String(index + 1)
    labels.push(sanitizeChartText(rawLabel, MAX_LABEL_CHARS))

    const valueKey = VALUE_KEYS.find((key) => row[key] !== undefined)
    if (valueKey !== undefined) {
      ensure('value')[index] = coerceValue(row[valueKey])
      return
    }
    // No canonical value key: every remaining non-label field becomes its own
    // series, which is how a model writes {day, requests, tokens} rows. A row
    // with nothing measurable leaves every column at its null default, so it
    // renders as an unknown category instead of failing the whole figure.
    for (const [key, value] of Object.entries(row)) {
      if (key === labelKey || LABEL_KEYS.includes(key)) continue
      const coerced = coerceValue(value)
      if (coerced === undefined) continue
      ensure(sanitizeChartText(key, MAX_NAME_CHARS))[index] = coerced
    }
  })

  const series: DraftSeries[] = []
  for (const [name, values] of columns) {
    values.length = labels.length
    series.push({ name, values: Array.from(values) })
  }
  if (series.length === 0) {
    return { ok: false, error: 'The chart has no numeric values.', hint: 'Give each row a "value", for example {"label":"kimi-k2","value":1234}.' }
  }
  // A single unnamed column is the common case; the title already names it.
  if (series.length === 1 && series[0].name === 'value') series[0].name = ''
  return { labels, series }
}

/** Reads `series: [...]` — the column-per-series shape. */
function draftFromSeries(rawSeries: unknown[], rawLabels: string[] | null): DraftData | ChartSpecError {
  const series: DraftSeries[] = []
  let labels = rawLabels
  for (const [index, entry] of rawSeries.entries()) {
    if (Array.isArray(entry)) {
      series.push({ name: `Series ${index + 1}`, values: entry.map(coerceValue) })
      continue
    }
    if (!isRecord(entry)) {
      return { ok: false, error: 'Each series must be an object with a name and values.', hint: '{"name":"Requests","values":[12,9,4]}' }
    }
    const name = optionalText(firstKey(entry, ['name', 'label', 'id', 'key']), MAX_NAME_CHARS) ?? `Series ${index + 1}`
    const rawValues = firstKey(entry, SERIES_VALUE_KEYS)
    if (!Array.isArray(rawValues)) {
      return { ok: false, error: `Series "${name}" has no values array.`, hint: '{"name":"Requests","values":[12,9,4]}' }
    }
    // Point objects ({label, value}) carry their own labels; the first series
    // that provides them defines the shared category axis.
    const pointLabels: string[] = []
    const values = rawValues.map((point, pointIndex) => {
      if (isRecord(point)) {
        const labelKey = LABEL_KEYS.find((key) => typeof point[key] === 'string')
        pointLabels[pointIndex] = labelKey
          ? sanitizeChartText(String(point[labelKey]), MAX_LABEL_CHARS)
          : String(pointIndex + 1)
        const valueKey = VALUE_KEYS.find((key) => point[key] !== undefined)
        return coerceValue(valueKey === undefined ? undefined : point[valueKey])
      }
      if (Array.isArray(point)) {
        pointLabels[pointIndex] = sanitizeChartText(String(point[0] ?? pointIndex + 1), MAX_LABEL_CHARS)
        return coerceValue(point[1])
      }
      return coerceValue(point)
    })
    if (labels === null && pointLabels.length === values.length) labels = pointLabels
    series.push({ name, values })
  }
  if (series.length === 0) {
    return { ok: false, error: 'The chart has no series.', hint: '"series":[{"name":"Requests","values":[12,9,4]}]' }
  }
  const width = Math.max(...series.map((s) => s.values.length))
  return { labels: labels ?? Array.from({ length: width }, (_, i) => String(i + 1)), series }
}

function readLabels(root: Record<string, unknown>): string[] | null {
  const raw = firstKey(root, ['labels', 'categories', 'x', 'buckets', 'columns', 'index'])
  if (!Array.isArray(raw)) return null
  return raw.map((entry, index) =>
    sanitizeChartText(entry === null || entry === undefined ? String(index + 1) : String(entry), MAX_LABEL_CHARS),
  )
}

/* ------------------------------------------------------------------ *
 * Entry point
 * ------------------------------------------------------------------ */

/**
 * Parses one chart fence. `info` is the fence's info string, so a type given as
 * ```chart bar is honored when the object omits `type`; an explicit `type` in
 * the object always wins.
 */
export function parseChartSpec(source: string, info: string | null = null): ChartSpecResult {
  const root = parseLenientJson(source)
  if (root === undefined) {
    return {
      ok: false,
      error: 'The chart block is not valid JSON.',
      hint: 'Write one JSON object, for example {"type":"bar","unit":"tokens","data":[{"label":"kimi-k2","value":1234}]}.',
    }
  }
  if (!isRecord(root)) {
    return { ok: false, error: 'A chart block must contain a single JSON object.', hint: null }
  }

  const resolved = resolveKind(firstKey(root, ['type', 'chart', 'kind', 'chart_type']), fenceKindHint(info))
  if (resolved === null) {
    return {
      ok: false,
      error: 'The chart type is missing or not supported.',
      hint: `Supported types: ${SUPPORTED_CHART_KINDS.join(', ')}.`,
    }
  }
  const stacked = resolved.stacked || root.stacked === true

  const labels = readLabels(root)
  const rawSeries = firstKey(root, ['series', 'rows', 'groups'])
  const rawData = firstKey(root, ['data', 'points', 'items', 'values', 'y'])

  let draft: DraftData | ChartSpecError
  if (Array.isArray(rawSeries)) {
    draft = draftFromSeries(rawSeries, labels)
  } else if (Array.isArray(rawData)) {
    const rows = draftFromRows(rawData)
    if ('ok' in rows) {
      draft = rows
    } else if (labels !== null && labels.length === rows.labels.length) {
      // Explicit labels win over positional placeholders.
      draft = { labels, series: rows.series }
    } else {
      draft = rows
    }
  } else {
    return {
      ok: false,
      error: 'The chart has no data.',
      hint: 'Use "data":[{"label":"kimi-k2","value":1234}] or "labels" plus "series":[{"name":"Requests","values":[12,9]}].',
    }
  }
  if ('ok' in draft) return draft

  return finalize(root, resolved.kind, stacked, draft)
}

function finalize(
  root: Record<string, unknown>,
  kind: ChartKind,
  stacked: boolean,
  draft: DraftData,
): ChartSpecResult {
  const labels = draft.labels
  if (labels.length === 0) {
    return { ok: false, error: 'The chart has no categories to plot.', hint: null }
  }
  if (labels.length > MAX_POINTS) {
    return {
      ok: false,
      error: `The chart has ${labels.length} points; the limit is ${MAX_POINTS}.`,
      hint: 'Aggregate to a coarser bucket or chart the top rows and describe the rest in prose.',
    }
  }
  if (draft.series.length > MAX_SERIES) {
    return {
      ok: false,
      error: `The chart has ${draft.series.length} series; the limit is ${MAX_SERIES}.`,
      hint: 'Keep the largest series and fold the remainder into one "Other" series.',
    }
  }
  if (labels.length * draft.series.length > MAX_CELLS) {
    return { ok: false, error: 'The chart has too many values to render.', hint: 'Reduce the buckets or the series count.' }
  }

  const series: ChartSeries[] = []
  let hasUnknown = false
  for (const entry of draft.series) {
    if (entry.values.length !== labels.length) {
      return {
        ok: false,
        error: `Series "${entry.name || 'value'}" has ${entry.values.length} values but there are ${labels.length} categories.`,
        hint: 'Every series must have exactly one value per label; use null where the evidence is unknown.',
      }
    }
    const values: (number | null)[] = []
    for (const value of entry.values) {
      if (value === undefined) {
        return {
          ok: false,
          error: `Series "${entry.name || 'value'}" contains a value that is not a number.`,
          hint: 'Use plain numbers, or null when the metric is unknown. Never write an unknown value as 0.',
        }
      }
      if (value === null) hasUnknown = true
      values.push(value)
    }
    series.push({ name: entry.name, values })
  }

  const unit = resolveUnit(firstKey(root, ['unit', 'units', 'metric_unit', 'value_unit']))
  const spec: ChartSpec = {
    kind,
    stacked,
    title: optionalText(firstKey(root, ['title', 'caption', 'heading']), MAX_TITLE_CHARS),
    unit,
    labels,
    series,
    source: optionalText(firstKey(root, ['source', 'source_id', 'data_source']), MAX_META_CHARS),
    period: optionalText(firstKey(root, ['period', 'range', 'window', 'timeframe']), MAX_META_CHARS),
    note: optionalText(firstKey(root, ['note', 'notes', 'caveat', 'footnote', 'provenance']), MAX_NOTE_CHARS),
    hasUnknown,
  }

  return validateForKind(spec)
}

/** Per-form rules that a generic shape check cannot express. */
function validateForKind(spec: ChartSpec): ChartSpecResult {
  if (spec.kind === 'donut') {
    if (spec.series.length !== 1) {
      return {
        ok: false,
        error: 'A donut chart shows one series of shares.',
        hint: 'Use "data":[{"label":"kimi-k2","value":62}], or switch to a stacked-column chart to compare several series.',
      }
    }
    if (spec.labels.length > MAX_DONUT_SLICES) {
      return {
        ok: false,
        error: `A donut chart shows at most ${MAX_DONUT_SLICES} slices; this one has ${spec.labels.length}.`,
        hint: 'Chart the largest slices plus one "Other", or use a bar chart for a long ranking.',
      }
    }
    if (spec.series[0].values.some((value) => value !== null && value < 0)) {
      return { ok: false, error: 'A donut chart cannot show negative shares.', hint: 'Use a bar chart for values that can be negative.' }
    }
    // A share-of-total form has nowhere to put an unknown slice: it would either
    // vanish or silently read as zero, and both misstate the evidence.
    if (spec.series[0].values.some((value) => value === null)) {
      return {
        ok: false,
        error: 'A donut chart cannot show an unknown share.',
        hint: 'Use a bar chart so the unknown row stays visible, and say in prose which value is unavailable.',
      }
    }
  }
  if (spec.kind === 'heatmap' && spec.series.some((s) => s.values.some((value) => value !== null && value < 0))) {
    return { ok: false, error: 'A heatmap cannot show negative values.', hint: 'Use a bar or column chart instead.' }
  }
  if (spec.stacked && spec.series.some((s) => s.values.some((value) => value !== null && value < 0))) {
    return { ok: false, error: 'A stacked chart cannot mix negative values.', hint: 'Drop "stacked" so the series are compared side by side.' }
  }
  return { ok: true, spec }
}

/* ------------------------------------------------------------------ *
 * Value formatting
 * ------------------------------------------------------------------ */

function formatSeconds(value: number): string {
  const abs = Math.abs(value)
  if (abs < 60) return `${Number(value.toFixed(abs < 10 ? 2 : 1))} s`
  const minutes = Math.floor(abs / 60)
  const seconds = Math.round(abs % 60)
  return `${value < 0 ? '-' : ''}${minutes}m ${String(seconds).padStart(2, '0')}s`
}

function formatPlain(value: number): string {
  return Number.isInteger(value) ? formatInteger(value) : String(Number(value.toFixed(2)))
}

/** Exact, human-readable value for tooltips, tables, and direct labels. */
export function formatChartValue(value: number | null, unit: ChartUnit): string {
  if (value === null) return 'unknown'
  switch (unit) {
    case 'usd':
      return formatCurrency(value)
    case 'tokens':
    case 'count':
      return Number.isInteger(value) ? formatInteger(value) : formatPlain(value)
    case 'percent': {
      const abs = Math.abs(value)
      return `${Number(value.toFixed(abs < 1 ? 2 : abs < 100 ? 1 : 0))}%`
    }
    case 'ms':
      return Math.abs(value) >= 1000 ? formatSeconds(value / 1000) : `${Number(value.toFixed(0))} ms`
    case 'seconds':
      return formatSeconds(value)
    case 'none':
      return formatPlain(value)
  }
}

/** Compact value for axis ticks, where horizontal space is scarce. */
export function formatChartTick(value: number, unit: ChartUnit): string {
  switch (unit) {
    case 'usd':
      return formatCompactCurrency(value)
    case 'tokens':
    case 'count':
      return formatCompactInteger(Math.round(value))
    case 'percent':
      return `${Number(value.toFixed(0))}%`
    case 'ms':
      return Math.abs(value) >= 1000 ? `${Number((value / 1000).toFixed(1))}s` : `${Number(value.toFixed(0))}ms`
    case 'seconds':
      return Math.abs(value) >= 60 ? `${Number((value / 60).toFixed(1))}m` : `${Number(value.toFixed(0))}s`
    case 'none':
      return formatCompactInteger(Math.round(value * 100) / 100)
  }
}

/** The provenance line under the title, e.g. "kimi-code · 7d". */
export function chartProvenance(spec: ChartSpec): string | null {
  const parts = [spec.source, spec.period].filter((part): part is string => part !== null)
  return parts.length === 0 ? null : parts.join(' · ')
}

/** Per-label totals of the stacked series, ignoring unknown values. */
export function stackTotals(spec: ChartSpec): (number | null)[] {
  return spec.labels.map((_, index) => {
    let total: number | null = null
    for (const series of spec.series) {
      const value = series.values[index]
      if (value === null) continue
      total = (total ?? 0) + value
    }
    return total
  })
}

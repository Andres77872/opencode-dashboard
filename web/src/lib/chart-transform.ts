import type { DayStats, TokenStats } from '../types/api'
import { getTokenTotal } from './token-breakdown'

export interface TokenBarDatum {
  date: string
  input: number
  'cache-read': number
  output: number
  reasoning: number
  'cache-write': number
  total: number
}

/** Normalised (0–1) datum for percentage-stacked token bars.
 *  Raw values are preserved in `_raw` so tooltips can show absolute counts. */
export interface TokenPercentageDatum {
  date: string
  input: number
  'cache-read': number
  output: number
  reasoning: number
  'cache-write': number
  total: number
  _raw: {
    input: number
    'cache-read': number
    output: number
    reasoning: number
    'cache-write': number
  }
}

export interface CostBarDatum {
  date: string
  cost: number
}

export interface RequestsBarDatum {
  date: string
  requests: number
}

export interface TokenSlice {
  name: string
  key: string
  value: number
  fill: string
}

export function transformDaysToTokenBars(days: DayStats[]): TokenBarDatum[] {
  return days.map((day) => ({
    date: day.date,
    input: day.tokens.input,
    'cache-read': day.tokens.cache.read,
    output: day.tokens.output,
    reasoning: day.tokens.reasoning,
    'cache-write': day.tokens.cache.write,
    total: getTokenTotal(day.tokens),
  }))
}

export function transformDaysToCostBars(days: DayStats[]): CostBarDatum[] {
  return days.map((day) => ({
    date: day.date,
    cost: day.cost,
  }))
}

export function transformDaysToRequestBars(days: DayStats[]): RequestsBarDatum[] {
  return days.map((day) => ({
    date: day.date,
    requests: day.messages,
  }))
}

/** Transform days into normalised (0–1) token categories for percentage-stacked bars.
 *  Raw absolute values are kept in `_raw` for tooltip display. */
export function transformDaysToTokenPercentageBars(days: DayStats[]): TokenPercentageDatum[] {
  return days.map((day) => {
    const input = day.tokens.input
    const cacheRead = day.tokens.cache.read
    const output = day.tokens.output
    const reasoning = day.tokens.reasoning
    const cacheWrite = day.tokens.cache.write
    const total = input + cacheRead + output + reasoning + cacheWrite

    if (total === 0) {
      return {
        date: day.date,
        input: 0,
        'cache-read': 0,
        output: 0,
        reasoning: 0,
        'cache-write': 0,
        total: 0,
        _raw: { input: 0, 'cache-read': 0, output: 0, reasoning: 0, 'cache-write': 0 },
      }
    }

    return {
      date: day.date,
      input: input / total,
      'cache-read': cacheRead / total,
      output: output / total,
      reasoning: reasoning / total,
      'cache-write': cacheWrite / total,
      total,
      _raw: { input, 'cache-read': cacheRead, output, reasoning, 'cache-write': cacheWrite },
    }
  })
}

const TOKEN_COLORS: Record<string, string> = {
  input: 'var(--color-chart-1)',
  'cache-read': 'var(--color-chart-2)',
  output: 'var(--color-chart-3)',
  reasoning: 'var(--color-chart-4)',
  'cache-write': 'var(--color-chart-5)',
}

const TOKEN_LABELS: Record<string, string> = {
  input: 'Input',
  'cache-read': 'Cache read',
  output: 'Output',
  reasoning: 'Reasoning',
  'cache-write': 'Cache write',
}

export function transformTokensToSlices(tokens: TokenStats): TokenSlice[] {
  const entries: Array<{ key: string; value: number }> = [
    { key: 'input', value: tokens.input },
    { key: 'cache-read', value: tokens.cache.read },
    { key: 'output', value: tokens.output },
    { key: 'reasoning', value: tokens.reasoning },
    { key: 'cache-write', value: tokens.cache.write },
  ]

  return entries
    .filter((entry) => entry.value > 0)
    .map((entry) => ({
      name: TOKEN_LABELS[entry.key] ?? entry.key,
      key: entry.key,
      value: entry.value,
      fill: TOKEN_COLORS[entry.key] ?? 'var(--color-chart-1)',
    }))
}
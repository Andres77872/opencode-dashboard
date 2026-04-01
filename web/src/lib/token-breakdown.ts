import type { TokenStats } from '../types/api'

export type TokenBreakdownKey = 'input' | 'cache-read' | 'output' | 'reasoning' | 'cache-write'

export interface TokenBreakdownItem {
  key: TokenBreakdownKey
  label: string
  value: number
  color: string
}

const TOKEN_BREAKDOWN_CONFIG: Array<{
  key: TokenBreakdownKey
  label: string
  color: string
  getValue: (tokens: TokenStats) => number
}> = [
  {
    key: 'input',
    label: 'Input',
    color: 'var(--color-chart-1)',
    getValue: (tokens) => tokens.input,
  },
  {
    key: 'cache-read',
    label: 'Cache read',
    color: 'var(--color-chart-2)',
    getValue: (tokens) => tokens.cache.read,
  },
  {
    key: 'output',
    label: 'Output',
    color: 'var(--color-chart-3)',
    getValue: (tokens) => tokens.output,
  },
  {
    key: 'reasoning',
    label: 'Reasoning',
    color: 'var(--color-chart-4)',
    getValue: (tokens) => tokens.reasoning,
  },
  {
    key: 'cache-write',
    label: 'Cache write',
    color: 'var(--color-chart-5)',
    getValue: (tokens) => tokens.cache.write,
  },
]

export function getTokenTotal(tokens: TokenStats) {
  return tokens.input + tokens.output + tokens.reasoning + tokens.cache.read + tokens.cache.write
}

export function getTokenBreakdownItems(tokens: TokenStats): TokenBreakdownItem[] {
  return TOKEN_BREAKDOWN_CONFIG.map((item) => ({
    key: item.key,
    label: item.label,
    value: item.getValue(tokens),
    color: item.color,
  }))
}

import type { ChartConfig } from '../components/ui/chart'

export const tokenStackedChartConfig = {
  input: { label: 'Input', color: 'var(--color-chart-1)' },
  'cache-read': { label: 'Cache read', color: 'var(--color-chart-2)' },
  output: { label: 'Output', color: 'var(--color-chart-3)' },
  reasoning: { label: 'Reasoning', color: 'var(--color-chart-4)' },
  'cache-write': { label: 'Cache write', color: 'var(--color-chart-5)' },
} satisfies ChartConfig

export const costChartConfig = {
  cost: { label: 'Cost', color: 'var(--color-chart-2)' },
} satisfies ChartConfig

export const requestsChartConfig = {
  requests: { label: 'Requests', color: 'var(--color-chart-1)' },
} satisfies ChartConfig

export const tokenBreakdownChartConfig = {
  input: { label: 'Input', color: 'var(--color-chart-1)' },
  'cache-read': { label: 'Cache read', color: 'var(--color-chart-2)' },
  output: { label: 'Output', color: 'var(--color-chart-3)' },
  reasoning: { label: 'Reasoning', color: 'var(--color-chart-4)' },
  'cache-write': { label: 'Cache write', color: 'var(--color-chart-5)' },
} satisfies ChartConfig
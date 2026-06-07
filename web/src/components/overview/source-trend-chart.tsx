import { useMemo, useState } from 'react'
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'

import type { SourceOverview } from '../../types/api'
import { buildSourceTrendData, type TrendMetric } from '../../lib/overview-all'
import { formatCompactCurrency, formatCompactInteger, formatShortDate } from '../../lib/format'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent, type ChartConfig } from '../ui/chart'
import { SegmentedControl } from '../daily/segmented-control'

const SOURCE_COLORS = [
  'var(--color-chart-1)',
  'var(--color-chart-2)',
  'var(--color-chart-3)',
  'var(--color-chart-4)',
  'var(--color-chart-5)',
]

const METRIC_OPTIONS = [
  { label: 'Messages', value: 'messages' as const },
  { label: 'Cost', value: 'cost' as const },
  { label: 'Tokens', value: 'tokens' as const },
]

interface SourceTrendChartProps {
  sources: SourceOverview[]
}

/**
 * Combined activity trend over the selected range, stacked by source. Each stack
 * segment is a single source's own value, so per-source costs are never merged
 * into one headline number.
 */
export function SourceTrendChart({ sources }: SourceTrendChartProps) {
  const [metric, setMetric] = useState<TrendMetric>('messages')

  const data = useMemo(() => buildSourceTrendData(sources, metric), [sources, metric])

  const config = useMemo<ChartConfig>(() => {
    const cfg: ChartConfig = {}
    sources.forEach((src, i) => {
      cfg[src.source_id] = { label: src.label ?? src.source_id, color: SOURCE_COLORS[i % SOURCE_COLORS.length] }
    })
    return cfg
  }, [sources])

  const tickFormatter = (value: number) =>
    metric === 'cost' ? formatCompactCurrency(value) : formatCompactInteger(value)

  if (data.length === 0) {
    return null
  }

  return (
    <Card className="border-border/70 bg-linear-to-b from-card to-panel">
      <CardHeader className="pb-0">
        <CardTitle className="text-xl">Activity over time</CardTitle>
        <p className="mt-1.5 max-w-2xl text-sm text-muted-foreground">
          Combined daily activity across all sources, stacked by source.
        </p>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        <div className="flex flex-wrap items-center gap-3 border-b border-border/40 pb-4">
          <SegmentedControl
            ariaLabel="Trend metric"
            className="max-w-full"
            onChange={setMetric}
            options={METRIC_OPTIONS}
            value={metric}
          />
        </div>

        <div className="rounded-2xl border border-border/70 bg-background/40 p-4">
          <ChartContainer config={config} className="min-h-[300px] w-full">
            <BarChart accessibilityLayer data={data}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis
                dataKey="date"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                tickFormatter={(value: string) => formatShortDate(value)}
                interval="preserveStartEnd"
                minTickGap={32}
              />
              <YAxis tickLine={false} axisLine={false} width={48} tickCount={6} tickFormatter={tickFormatter} />
              <ChartTooltip
                cursor={{ fill: 'oklch(0.74 0.16 64 / 0.15)' }}
                allowEscapeViewBox={{ x: true, y: true }}
                content={
                  <ChartTooltipContent
                    indicator="dot"
                    labelFormatter={(value) => formatShortDate(String(value))}
                    formatter={(value, name) => {
                      const label = config[name as string]?.label ?? name
                      const formatted = metric === 'cost' ? formatCompactCurrency(Number(value)) : formatCompactInteger(Number(value))
                      return (
                        <div className="flex w-full items-center justify-between gap-4">
                          <span className="text-muted-foreground">{label}</span>
                          <span className="font-mono font-medium text-foreground tabular-nums whitespace-nowrap">{formatted}</span>
                        </div>
                      )
                    }}
                  />
                }
              />
              <ChartLegend content={<ChartLegendContent />} />
              {sources.map((src, i) => (
                <Bar
                  key={src.source_id}
                  dataKey={src.source_id}
                  stackId="src"
                  fill={`var(--color-${src.source_id})`}
                  radius={i === sources.length - 1 ? [4, 4, 0, 0] : [0, 0, 0, 0]}
                />
              ))}
            </BarChart>
          </ChartContainer>
        </div>
      </CardContent>
    </Card>
  )
}

import { useMemo } from 'react'
import { BarChart, Bar, XAxis, YAxis, CartesianGrid } from 'recharts'

import type { DayStats, Granularity, TokenStats } from '../../types/api'
import { formatCompactCurrency, formatCompactInteger, formatShortDate, formatTokenCount } from '../../lib/format'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { ChartContainer, ChartTooltip, ChartTooltipContent, ChartLegend, ChartLegendContent } from '../ui/chart'
import { dailyMetricOptions, formatDailyMetricValue, getDailyMetricMeta, getDailyMetricValue, type DailyMetric } from './daily-metrics'
import { SegmentedControl } from './segmented-control'
import { tokenStackedChartConfig, costChartConfig, requestsChartConfig } from '../../lib/chart-config'
import { transformDaysToTokenBars, transformDaysToCostBars, transformDaysToRequestBars } from '../../lib/chart-transform'

interface DailyChartProps {
  days: DayStats[]
  metric: DailyMetric
  granularity?: Granularity
  onMetricChange: (value: DailyMetric) => void
}

const EMPTY_TOKENS: TokenStats = {
  input: 0,
  output: 0,
  reasoning: 0,
  cache: {
    read: 0,
    write: 0,
  },
}

const EMPTY_DAY: DayStats = {
  date: '',
  sessions: 0,
  messages: 0,
  cost: 0,
  tokens: EMPTY_TOKENS,
}

function getLatestDeltaLabel(days: DayStats[], metric: DailyMetric, isHourly?: boolean) {
  if (days.length < 2) {
    return 'No comparison yet'
  }

  const latestDay = days[days.length - 1]
  const previousDay = days[days.length - 2]
  const delta = getDailyMetricValue(latestDay, metric) - getDailyMetricValue(previousDay, metric)

  const previousLabel = isHourly ? formatShortDate(previousDay.date) : formatShortDate(previousDay.date)

  if (delta === 0) {
    return `Flat vs ${previousLabel}`
  }

  return `${delta > 0 ? 'Up' : 'Down'} ${formatDailyMetricValue(metric, Math.abs(delta), true)} vs ${previousLabel}`
}

function tickFormatterForMetric(metric: DailyMetric) {
  switch (metric) {
    case 'cost':
      return (value: number) => formatCompactCurrency(value)
    case 'requests':
      return (value: number) => formatCompactInteger(value)
    case 'tokens':
      return (value: number) => formatCompactInteger(value)
  }
}

export function DailyChart({ days, metric, granularity, onMetricChange }: DailyChartProps) {
  const meta = getDailyMetricMeta(metric)
  const values = days.map((day) => getDailyMetricValue(day, metric))
  const maxValue = Math.max(...values, 0)
  const totalValue = values.reduce((sum, value) => sum + value, 0)
  const averageValue = days.length === 0 ? 0 : totalValue / days.length
  const latestDay = days[days.length - 1] ?? EMPTY_DAY
  const peakDay = days.find((day) => getDailyMetricValue(day, metric) === maxValue) ?? latestDay
  const isHourly = granularity === 'hour'

  const peakLabel = isHourly ? 'Peak hour' : 'Peak day'
  const averageLabel = isHourly ? 'Average / hour' : 'Average / day'
  const latestLabel = isHourly ? 'Latest hour' : 'Latest day'

  // eslint-disable-next-line @typescript-eslint/no-explicit-any -- Recharts data is untyped; each metric produces a different shape
  const chartData = useMemo((): any[] => {
    switch (metric) {
      case 'tokens':
        return transformDaysToTokenBars(days)
      case 'cost':
        return transformDaysToCostBars(days)
      case 'requests':
        return transformDaysToRequestBars(days)
    }
  }, [days, metric])

  const chartConfig = useMemo(() => {
    switch (metric) {
      case 'tokens':
        return tokenStackedChartConfig
      case 'cost':
        return costChartConfig
      case 'requests':
        return requestsChartConfig
    }
  }, [metric])

  const yAxisFormatter = tickFormatterForMetric(metric)

  return (
    <Card className="border-border/70 bg-linear-to-b from-card to-panel">
      <CardHeader className="gap-4">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="space-y-1.5">
            <CardDescription>Trend explorer</CardDescription>
            <CardTitle>{meta.chartTitle}</CardTitle>
            <p className="max-w-2xl text-sm text-muted-foreground">{meta.chartDescription}</p>
          </div>

          <div className="space-y-2">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Chart metric</div>
            <SegmentedControl
              ariaLabel="Daily chart metric"
              className="max-w-full"
              onChange={onMetricChange}
              options={dailyMetricOptions}
              value={metric}
            />
          </div>
        </div>
      </CardHeader>

      <CardContent className="space-y-5">
        <div className="grid gap-3 sm:grid-cols-3">
          <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{peakLabel}</div>
            <div className="mt-2 font-mono text-lg text-foreground">{formatDailyMetricValue(metric, getDailyMetricValue(peakDay, metric), true)}</div>
            <div className="text-sm text-muted-foreground">{peakDay.date ? formatShortDate(peakDay.date) : 'No data'}</div>
          </div>

          <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{averageLabel}</div>
            <div className="mt-2 font-mono text-lg text-foreground">{formatDailyMetricValue(metric, averageValue, true)}</div>
            <div className="text-sm text-muted-foreground">Inactive hours stay in the window for honest pacing</div>
          </div>

          <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{latestLabel}</div>
            <div className="mt-2 font-mono text-lg text-foreground">{formatDailyMetricValue(metric, getDailyMetricValue(latestDay, metric), true)}</div>
            <div className="text-sm text-muted-foreground">{latestDay.date ? getLatestDeltaLabel(days, metric, isHourly) : 'No data yet'}</div>
          </div>
        </div>

        <div className="rounded-2xl border border-border/70 bg-background/40 p-4">
          <ChartContainer config={chartConfig} className="min-h-[300px] w-full">
            <BarChart accessibilityLayer data={chartData}>
              <CartesianGrid vertical={false} strokeDasharray="3 3" />
              <XAxis
                dataKey="date"
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                tickFormatter={(value: string) => formatShortDate(value)}
              />
              <YAxis
                tickLine={false}
                axisLine={false}
                width={60}
                tickFormatter={yAxisFormatter}
              />

              {metric === 'tokens' ? (
                <>
                  <ChartTooltip
                    cursor={{ fill: 'oklch(0.74 0.16 64 / 0.15)' }}
                    allowEscapeViewBox={{ x: true, y: true }}
                    content={
                      <ChartTooltipContent
                        indicator="dot"
                        labelFormatter={(value) => formatShortDate(String(value))}
                        formatter={(value, name) => {
                          const config = tokenStackedChartConfig[name as keyof typeof tokenStackedChartConfig]
                          return (
                            <div className="flex w-full items-center justify-between gap-4">
                              <span className="text-muted-foreground">{config?.label ?? name}</span>
                              <span className="font-mono font-medium text-foreground tabular-nums">{formatTokenCount(Number(value))}</span>
                            </div>
                          )
                        }}
                      />
                    }
                  />
                  <ChartLegend content={<ChartLegendContent />} />
                  <Bar dataKey="input" stackId="tokens" fill="var(--color-input)" radius={[0, 0, 0, 0]} />
                  <Bar dataKey="cache-read" stackId="tokens" fill="var(--color-cache-read)" radius={[0, 0, 0, 0]} />
                  <Bar dataKey="output" stackId="tokens" fill="var(--color-output)" radius={[0, 0, 0, 0]} />
                  <Bar dataKey="reasoning" stackId="tokens" fill="var(--color-reasoning)" radius={[0, 0, 0, 0]} />
                  <Bar dataKey="cache-write" stackId="tokens" fill="var(--color-cache-write)" radius={[4, 4, 0, 0]} />
                </>
              ) : metric === 'cost' ? (
                <>
                  <ChartTooltip
                    cursor={{ fill: 'oklch(0.74 0.16 64 / 0.15)' }}
                    allowEscapeViewBox={{ x: true, y: true }}
                    content={
                      <ChartTooltipContent
                        hideIndicator
                        labelFormatter={(value) => formatShortDate(String(value))}
                        formatter={(value) => (
                          <div className="flex w-full items-center justify-between gap-4">
                            <span className="text-muted-foreground">Cost</span>
                            <span className="font-mono font-medium text-foreground tabular-nums">{formatCompactCurrency(Number(value))}</span>
                          </div>
                        )}
                      />
                    }
                  />
                  <Bar dataKey="cost" fill="var(--color-cost)" radius={[4, 4, 0, 0]} />
                </>
              ) : (
                <>
                  <ChartTooltip
                    cursor={{ fill: 'oklch(0.74 0.16 64 / 0.15)' }}
                    allowEscapeViewBox={{ x: true, y: true }}
                    content={
                      <ChartTooltipContent
                        hideIndicator
                        labelFormatter={(value) => formatShortDate(String(value))}
                        formatter={(value) => (
                          <div className="flex w-full items-center justify-between gap-4">
                            <span className="text-muted-foreground">Requests</span>
                            <span className="font-mono font-medium text-foreground tabular-nums">{formatCompactInteger(Number(value))}</span>
                          </div>
                        )}
                      />
                    }
                  />
                  <Bar dataKey="requests" fill="var(--color-requests)" radius={[4, 4, 0, 0]} />
                </>
              )}
            </BarChart>
          </ChartContainer>
        </div>
      </CardContent>
    </Card>
  )
}

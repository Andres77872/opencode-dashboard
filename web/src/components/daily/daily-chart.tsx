import type { DayStats, TokenStats } from '../../types/api'
import { formatCompactInteger, formatPercentage, formatShortDate, formatShortWeekday, formatTokenCount } from '../../lib/format'
import { getTokenBreakdownItems, getTokenTotal } from '../../lib/token-breakdown'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { cn } from '../../lib/utils'
import { dailyMetricOptions, formatDailyMetricValue, getDailyMetricMeta, getDailyMetricValue, type DailyMetric } from './daily-metrics'
import { SegmentedControl } from './segmented-control'

interface DailyChartProps {
  days: DayStats[]
  metric: DailyMetric
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

function shouldShowLabel(index: number, total: number) {
  if (total <= 7) {
    return true
  }

  if (index === 0 || index === total - 1) {
    return true
  }

  return index % 5 === 0
}

function hasActivity(day: DayStats) {
  return day.cost > 0 || day.messages > 0 || day.sessions > 0 || getTokenTotal(day.tokens) > 0
}

function getLatestDeltaLabel(days: DayStats[], metric: DailyMetric) {
  if (days.length < 2) {
    return 'No comparison yet'
  }

  const latestDay = days[days.length - 1]
  const previousDay = days[days.length - 2]
  const delta = getDailyMetricValue(latestDay, metric) - getDailyMetricValue(previousDay, metric)

  if (delta === 0) {
    return `Flat vs ${formatShortDate(previousDay.date)}`
  }

  return `${delta > 0 ? 'Up' : 'Down'} ${formatDailyMetricValue(metric, Math.abs(delta), true)} vs ${formatShortDate(previousDay.date)}`
}

function getTooltipMetricRows(day: DayStats, metric: DailyMetric) {
  const rows = [
    { label: 'Cost', value: formatDailyMetricValue('cost', day.cost) },
    { label: 'Requests', value: formatDailyMetricValue('requests', day.messages) },
    { label: 'Sessions', value: formatCompactInteger(day.sessions) },
    { label: 'Tokens', value: formatDailyMetricValue('tokens', getTokenTotal(day.tokens)) },
  ]

  const currentMetricLabel = getDailyMetricMeta(metric).label

  return [
    rows.find((row) => row.label === currentMetricLabel),
    ...rows.filter((row) => row.label !== currentMetricLabel),
  ].filter((row): row is { label: string; value: string } => Boolean(row))
}

function getStackSegments(day: DayStats) {
  const total = getTokenTotal(day.tokens)

  if (total === 0) {
    return []
  }

  return getTokenBreakdownItems(day.tokens)
    .filter((item) => item.value > 0)
    .map((item) => ({
      ...item,
      percentage: (item.value / total) * 100,
    }))
}

function getWindowTokens(days: DayStats[]) {
  return days.reduce<TokenStats>(
    (accumulator, day) => ({
      input: accumulator.input + day.tokens.input,
      output: accumulator.output + day.tokens.output,
      reasoning: accumulator.reasoning + day.tokens.reasoning,
      cache: {
        read: accumulator.cache.read + day.tokens.cache.read,
        write: accumulator.cache.write + day.tokens.cache.write,
      },
    }),
    EMPTY_TOKENS,
  )
}

export function DailyChart({ days, metric, onMetricChange }: DailyChartProps) {
  const meta = getDailyMetricMeta(metric)
  const values = days.map((day) => getDailyMetricValue(day, metric))
  const maxValue = Math.max(...values, 0)
  const totalValue = values.reduce((sum, value) => sum + value, 0)
  const averageValue = days.length === 0 ? 0 : totalValue / days.length
  const latestDay = days[days.length - 1] ?? EMPTY_DAY
  const peakDay = days.find((day) => getDailyMetricValue(day, metric) === maxValue) ?? latestDay
  const windowTokenLegend = getTokenBreakdownItems(getWindowTokens(days)).filter((item) => item.value > 0)
  const chartMinWidth = days.length > 7 ? `${days.length * 44}px` : undefined

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
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Peak day</div>
            <div className="mt-2 font-mono text-lg text-foreground">{formatDailyMetricValue(metric, getDailyMetricValue(peakDay, metric), true)}</div>
            <div className="text-sm text-muted-foreground">{peakDay.date ? formatShortDate(peakDay.date) : 'No data'}</div>
          </div>

          <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Average / day</div>
            <div className="mt-2 font-mono text-lg text-foreground">{formatDailyMetricValue(metric, averageValue, true)}</div>
            <div className="text-sm text-muted-foreground">Inactive days stay in the window for honest pacing</div>
          </div>

          <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
            <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Latest day</div>
            <div className="mt-2 font-mono text-lg text-foreground">{formatDailyMetricValue(metric, getDailyMetricValue(latestDay, metric), true)}</div>
            <div className="text-sm text-muted-foreground">{latestDay.date ? getLatestDeltaLabel(days, metric) : 'No data yet'}</div>
          </div>
        </div>

        <div className="rounded-2xl border border-border/70 bg-background/40 p-4">
          <div className="mb-4 flex items-center justify-between gap-3 text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
            <span>{meta.label} scale</span>
            <span className="font-mono text-foreground">{formatDailyMetricValue(metric, maxValue, true)} max</span>
          </div>

          <TooltipProvider>
            <div className="-mx-1 overflow-x-auto px-1 pb-2">
              <div className="relative min-w-full" style={chartMinWidth ? { minWidth: chartMinWidth } : undefined}>
                <div className="pointer-events-none absolute inset-x-0 top-0 flex h-64 flex-col justify-between pb-7">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <div key={index} className="border-t border-dashed border-border/55" />
                  ))}
                </div>

                <div className="flex h-64 items-end gap-2">
                  {days.map((day, index) => {
                    const total = getDailyMetricValue(day, metric)
                    const height = maxValue > 0 ? Math.max((total / maxValue) * 100, total > 0 ? 8 : 2) : 2
                    const active = hasActivity(day)
                    const stackSegments = getStackSegments(day)
                    const tooltipRows = getTooltipMetricRows(day, metric)

                    return (
                      <div key={day.date} className="flex h-full min-w-0 flex-1 flex-col justify-end">
                        <div className="relative flex h-full items-end justify-center">
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <button
                                type="button"
                                aria-label={`${formatShortDate(day.date)} ${meta.label.toLowerCase()} ${formatDailyMetricValue(metric, total)}, ${day.sessions} sessions, ${day.messages} requests, ${getTokenTotal(day.tokens)} tokens`}
                                className={cn(
                                  'relative flex w-full appearance-none items-end justify-center overflow-hidden rounded-t-xl border border-b-0 px-1 outline-none transition-[filter,background-color] focus-visible:ring-2 focus-visible:ring-accent/70',
                                  metric === 'tokens'
                                    ? active
                                      ? 'border-border/70 bg-background/25 hover:brightness-110 focus-visible:brightness-110'
                                      : 'border-border/70 bg-linear-to-t from-muted/80 to-muted/25'
                                    : active
                                      ? 'border-accent/35 bg-linear-to-t from-accent/90 via-accent/60 to-accent/20 hover:brightness-110 focus-visible:brightness-110'
                                      : 'border-border/70 bg-linear-to-t from-muted/80 to-muted/25',
                                )}
                                style={{ height: `${height}%` }}
                              >
                                {metric === 'tokens' && stackSegments.length > 0 ? (
                                  <div className="absolute inset-0 flex flex-col justify-end">
                                    {stackSegments.map((segment) => (
                                      <div
                                        key={segment.key}
                                        className="w-full"
                                        style={{
                                          backgroundColor: segment.color,
                                          height: `${segment.percentage}%`,
                                        }}
                                      />
                                    ))}
                                  </div>
                                ) : (
                                  <>
                                    <div className="absolute inset-x-[18%] top-1 h-3 rounded-full bg-white/18 blur-sm" />
                                    <div className="absolute inset-x-0 bottom-0 top-[55%] bg-linear-to-t from-black/10 to-transparent" />
                                  </>
                                )}
                              </button>
                            </TooltipTrigger>

                            <TooltipContent
                              side="top"
                              align="center"
                              sideOffset={12}
                              collisionPadding={16}
                              className={metric === 'tokens' ? 'w-56' : 'w-48'}
                            >
                              <div className="text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground">{formatShortDate(day.date)}</div>

                              <div className="mt-2 space-y-1.5 text-sm">
                                {tooltipRows.map((row) => (
                                  <div key={row.label} className="flex items-center justify-between gap-3">
                                    <span className={cn('text-muted-foreground', row.label === meta.label && 'text-foreground/80')}>{row.label}</span>
                                    <span className="font-mono text-foreground">{row.value}</span>
                                  </div>
                                ))}
                              </div>

                              {metric === 'tokens' && stackSegments.length > 0 ? (
                                <div className="mt-3 border-t border-border/60 pt-3">
                                  <div className="mb-2 text-[11px] uppercase tracking-[0.14em] text-muted-foreground">Token mix</div>
                                  <div className="space-y-1.5 text-sm">
                                    {stackSegments.map((segment) => (
                                      <div key={segment.key} className="flex items-center justify-between gap-3">
                                        <span className="flex items-center gap-2 text-muted-foreground">
                                          <span
                                            aria-hidden="true"
                                            className="size-2 rounded-full border border-white/12"
                                            style={{ backgroundColor: segment.color }}
                                          />
                                          {segment.label}
                                        </span>
                                        <span className="font-mono text-foreground">
                                          {formatTokenCount(segment.value)} · {formatPercentage(segment.percentage)}
                                        </span>
                                      </div>
                                    ))}
                                  </div>
                                </div>
                              ) : null}
                            </TooltipContent>
                          </Tooltip>
                        </div>

                        <div className="mt-3 text-center text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                          {shouldShowLabel(index, days.length) ? formatShortWeekday(day.date) : '·'}
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            </div>
          </TooltipProvider>

          {metric === 'tokens' && windowTokenLegend.length > 0 ? (
            <div className="mt-4 flex flex-wrap gap-x-4 gap-y-2 text-xs text-muted-foreground">
              {windowTokenLegend.map((item) => (
                <span key={item.key} className="inline-flex items-center gap-2">
                  <span
                    aria-hidden="true"
                    className="size-2.5 rounded-full border border-white/12"
                    style={{ backgroundColor: item.color }}
                  />
                  {item.label}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}

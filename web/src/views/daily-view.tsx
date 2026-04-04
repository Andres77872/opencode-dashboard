import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { DailyChart } from '../components/daily/daily-chart'
import { getDailyMetricMeta, getDailyMetricValue, type DailyMetric } from '../components/daily/daily-metrics'
import { DailySkeleton } from '../components/daily/daily-skeleton'
import { PeriodToggle } from '../components/daily/period-toggle'
import { MetricCard } from '../components/overview/metric-card'
import { TokenBreakdownCard } from '../components/overview/token-breakdown-card'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { getDaily } from '../lib/api'
import { formatCompactInteger, formatCurrency, formatInteger, formatShortDate, formatTokenCount, safeDivide } from '../lib/format'
import { getTokenBreakdownItems, getTokenTotal } from '../lib/token-breakdown'
import { isDailyPeriod, type DailyPeriod, type DailyStats, type DayStats } from '../types/api'

const dailyMetricCopy: Record<DailyMetric, { detailTitle: string; detailDescription: string; metricLabel: string }> = {
  cost: {
    detailTitle: 'Window detail',
    detailDescription: 'Track which days are actually driving spend in the selected range.',
    metricLabel: 'Cost',
  },
  requests: {
    detailTitle: 'Request cadence',
    detailDescription: 'Requests are represented by message volume because that is the daily API signal available now.',
    metricLabel: 'Requests',
  },
  tokens: {
    detailTitle: 'Token distribution',
    detailDescription: 'Token mode highlights how input, cache, output, reasoning, and writes move day by day.',
    metricLabel: 'Tokens',
  },
}

function hasActivity(day: DayStats) {
  return day.sessions > 0 || day.messages > 0 || day.cost > 0 || getTokenTotal(day.tokens) > 0
}

function compareDayActivity(current: DayStats, candidate: DayStats) {
  if (candidate.cost !== current.cost) {
    return candidate.cost > current.cost
  }

  if (candidate.messages !== current.messages) {
    return candidate.messages > current.messages
  }

  if (candidate.sessions !== current.sessions) {
    return candidate.sessions > current.sessions
  }

  return getTokenTotal(candidate.tokens) > getTokenTotal(current.tokens)
}

function getWindowTokens(data: DailyStats) {
  return data.days.reduce(
    (accumulator, day) => ({
      input: accumulator.input + day.tokens.input,
      output: accumulator.output + day.tokens.output,
      reasoning: accumulator.reasoning + day.tokens.reasoning,
      cache: {
        read: accumulator.cache.read + day.tokens.cache.read,
        write: accumulator.cache.write + day.tokens.cache.write,
      },
    }),
    {
      input: 0,
      output: 0,
      reasoning: 0,
      cache: { read: 0, write: 0 },
    },
  )
}

function getPeriodWindowHint(period: DailyPeriod, dayCount: number, granularity?: string) {
  if (granularity === 'hour') {
    return `Current UTC day with ${dayCount} hourly buckets`
  }
  
  switch (period) {
    case '1d':
      return 'Current UTC day only'
    case '1y':
      return `${formatCompactInteger(dayCount)} calendar days in the trailing year`
    case 'all':
      return `All historic spans ${formatCompactInteger(dayCount)} calendar days since first recorded activity`
    default:
      return `${formatCompactInteger(dayCount)}-day inclusive window`
  }
}

function getEmptyWindowCopy(period: DailyPeriod) {
  if (period === 'all') {
    return 'All historic stretches from the first recorded activity day through today when data exists, otherwise the view falls back to today.'
  }

  return 'The backend returns a zero-filled calendar window for the selected period until OpenCode records real sessions and assistant messages.'
}

export function DailyView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const [dataByPeriod, setDataByPeriod] = useState<Partial<Record<DailyPeriod, DailyStats>>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [metric, setMetric] = useState<DailyMetric>('cost')
  const dataByPeriodRef = useRef<Partial<Record<DailyPeriod, DailyStats>>>({})

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = isDailyPeriod(rawPeriod) ? rawPeriod : '7d'
  const dataForPeriod = dataByPeriod[period] ?? null

  useEffect(() => {
    const controller = new AbortController()

    async function loadDaily() {
      setRefreshing(true)
      setError(null)
      setLoading(!dataByPeriodRef.current[period])

      try {
        const next = await getDaily(period, controller.signal)
        const nextDataByPeriod = {
          ...dataByPeriodRef.current,
          [period]: next,
        }

        dataByPeriodRef.current = nextDataByPeriod
        setDataByPeriod(nextDataByPeriod)
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setError(caught instanceof Error ? caught.message : 'Failed to load daily stats')
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void loadDaily()

    return () => controller.abort()
  }, [period, refreshNonce, setLastUpdatedAt, setRefreshing])

  const summary = useMemo(() => {
    if (!dataForPeriod) {
      return null
    }

    const isHourly = dataForPeriod.granularity === 'hour'

    const totals = dataForPeriod.days.reduce(
      (accumulator, day) => {
        accumulator.sessions += day.sessions
        accumulator.messages += day.messages
        accumulator.cost += day.cost
        accumulator.tokens += getTokenTotal(day.tokens)

        if (hasActivity(day)) {
          accumulator.activeDays += 1
        }

        if (!accumulator.peakDay || compareDayActivity(accumulator.peakDay, day)) {
          accumulator.peakDay = day
        }

        return accumulator
      },
      {
        sessions: 0,
        messages: 0,
        cost: 0,
        tokens: 0,
        activeDays: 0,
        peakDay: null as DayStats | null,
      },
    )

    return {
      ...totals,
      isHourly,
      averageCostPerDay: safeDivide(totals.cost, dataForPeriod.days.length),
      averageMessagesPerSession: safeDivide(totals.messages, totals.sessions),
      averageTokensPerDay: safeDivide(totals.tokens, dataForPeriod.days.length),
      recentDays: [...dataForPeriod.days].reverse(),
      empty: totals.activeDays === 0,
      windowTokens: getWindowTokens(dataForPeriod),
    }
  }, [dataForPeriod])

  const metricMeta = getDailyMetricMeta(metric)
  const metricDetailCopy = dailyMetricCopy[metric]

  const handleRetry = () => {
    requestRefresh()
  }

  const handlePeriodChange = (nextPeriod: DailyPeriod) => {
    if (nextPeriod === period) {
      return
    }

    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('period', nextPeriod)
      return next
    })
  }

  if (loading && !dataForPeriod) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Daily</h2>
             <p className="max-w-3xl text-sm text-muted-foreground">
               Trend view backed by <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/daily</code>
               , with shareable 1d-to-all windows and switchable cost, request, and token lenses.
             </p>
          </div>
          <PeriodToggle value={period} onChange={handlePeriodChange} disabled />
        </div>
        <DailySkeleton />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Daily</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Switch between spend, request volume, and token flow without leaving the same daily window. The URL period toggle stays shareable and maps directly to the Go API.
          </p>
        </div>

        <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="text-sm text-muted-foreground">
            Endpoint:{' '}
            <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/daily?period={period}</code>
          </div>
          <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Daily trends failed to load</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>
            Retry
          </Button>
        </Alert>
      ) : null}

      {summary ? (
        <>
          <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-4">
            <MetricCard
              label="Spend in window"
              value={formatCurrency(summary.cost)}
              hint={getPeriodWindowHint(period, dataForPeriod?.days.length ?? 0, dataForPeriod?.granularity)}
            />
            <MetricCard
              label="Sessions"
              value={formatInteger(summary.sessions)}
              hint={`${formatCompactInteger(summary.activeDays)} active days in the selected range`}
            />
            <MetricCard
              label="Requests"
              value={formatInteger(summary.messages)}
              hint={`${summary.averageMessagesPerSession.toFixed(1)} messages per session in the current API model`}
            />
              <MetricCard
                label="Token load"
                value={formatTokenCount(summary.tokens)}
                hint={`${formatTokenCount(Math.round(summary.averageTokensPerDay))} average tokens per calendar day`}
              />
          </div>

          {summary.empty ? (
            <Card>
              <CardHeader>
                <CardDescription>Empty state</CardDescription>
                <CardTitle>No daily activity yet</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <p>
                  {getEmptyWindowCopy(period)}
                </p>
                <p>
                  Once data exists, this screen will light up with switchable daily charts, token mix context, and a newest-first ledger automatically.
                </p>
              </CardContent>
            </Card>
          ) : (
            <div className="grid items-start gap-4 2xl:grid-cols-[minmax(0,1.55fr)_minmax(20rem,1fr)]">
              <div className="min-w-0 space-y-4">
                <DailyChart days={dataForPeriod?.days ?? []} metric={metric} granularity={dataForPeriod?.granularity} onMetricChange={setMetric} />

                {metric === 'tokens' ? (
                  <TokenBreakdownCard
                    description="Window-level token mix"
                    hideZeroItems
                    title="Selected range"
                    tokens={summary.windowTokens}
                  />
                ) : null}
              </div>

              <Card className="min-w-0 2xl:self-start">
                <CardHeader className="pb-3">
                  <CardDescription>{metricDetailCopy.detailTitle}</CardDescription>
                  <CardTitle>{summary.isHourly ? 'Newest hours first' : 'Newest days first'}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  {/* Compact summary strip */}
                  <div className="flex items-center gap-2 rounded-lg border border-border/60 bg-panel/50 px-3 py-2 text-sm text-muted-foreground">
                    <span className="font-mono text-foreground">{summary.activeDays}/{dataForPeriod?.days.length ?? 0}</span>
                    <span>active {summary.isHourly ? 'hours' : 'days'}</span>
                    <span className="text-border">·</span>
                    <span>{metricMeta.label} lens</span>
                  </div>

                  {/* Compact activity ledger */}
                  <div className="max-h-[32rem] space-y-1 overflow-y-auto pr-1">
                    {summary.recentDays.map((day) => {
                      const active = hasActivity(day)
                      const metricValue = getDailyMetricValue(day, metric)
                      const tokenBreakdown = getTokenBreakdownItems(day.tokens).filter((item) => item.value > 0)

                      if (!active) {
                        return (
                          <div
                            key={day.date}
                            className="flex items-center gap-3 rounded-md px-3 py-1.5 text-xs text-muted-foreground/60"
                          >
                            <span className="min-w-[4.5rem] font-medium">{formatShortDate(day.date)}</span>
                            <span className="italic">No activity</span>
                          </div>
                        )
                      }

                      return (
                        <div
                          key={day.date}
                          className="rounded-md border-l-2 border-l-accent/60 border border-border/50 bg-panel/50 px-3 py-2 transition-colors hover:bg-panel/80"
                        >
                          <div className="flex items-center justify-between gap-2">
                            <div className="flex items-center gap-3 min-w-0">
                              <span className="text-sm font-medium text-foreground whitespace-nowrap">{formatShortDate(day.date)}</span>
                              <span className="text-xs text-muted-foreground truncate">
                                {formatCompactInteger(day.sessions)} sess
                                <span className="mx-1 text-border">·</span>
                                {formatCompactInteger(day.messages)} req
                                <span className="mx-1 text-border">·</span>
                                {formatTokenCount(getTokenTotal(day.tokens))} tok
                              </span>
                            </div>
                            <span className="font-mono text-sm text-foreground whitespace-nowrap">
                              {metric === 'cost'
                                ? formatCurrency(day.cost)
                                : metric === 'requests'
                                  ? formatInteger(day.messages)
                                  : formatTokenCount(metricValue)}
                            </span>
                          </div>

                          {metric === 'tokens' && tokenBreakdown.length > 0 ? (
                            <div className="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[11px] text-muted-foreground">
                              {tokenBreakdown.map((item) => (
                                <span key={item.key} className="inline-flex items-center gap-1.5">
                                  <span
                                    aria-hidden="true"
                                    className="size-1.5 rounded-full"
                                    style={{ backgroundColor: item.color }}
                                  />
                                  <span>{item.label}</span>
                                  <span className="font-mono text-foreground/80">{formatTokenCount(item.value)}</span>
                                </span>
                              ))}
                            </div>
                          ) : null}
                        </div>
                      )
                    })}
                  </div>
                </CardContent>
              </Card>
            </div>
          )}
        </>
      ) : null}
    </section>
  )
}

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
import type { DailyPeriod, DailyStats, DayStats } from '../types/api'

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

export function DailyView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const [dataByPeriod, setDataByPeriod] = useState<Partial<Record<DailyPeriod, DailyStats>>>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [metric, setMetric] = useState<DailyMetric>('cost')
  const dataByPeriodRef = useRef<Partial<Record<DailyPeriod, DailyStats>>>({})

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = rawPeriod === '30d' ? '30d' : '7d'
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
              , with shareable 7d/30d windows and switchable cost, request, and token lenses.
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
              hint={`${period === '7d' ? 'Seven' : 'Thirty'}-day inclusive window`}
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
                  The backend still returns a full {period} window, but every day is zero-filled until OpenCode records real sessions and assistant messages.
                </p>
                <p>
                  Once data exists, this screen will light up with switchable daily charts, token mix context, and a newest-first ledger automatically.
                </p>
              </CardContent>
            </Card>
          ) : (
            <div className="grid gap-4 xl:grid-cols-[1.55fr_1fr]">
              <div className="space-y-4">
                <DailyChart days={dataForPeriod?.days ?? []} metric={metric} onMetricChange={setMetric} />

                {metric === 'tokens' ? (
                  <TokenBreakdownCard
                    description="Window-level token mix"
                    hideZeroItems
                    title="Selected range"
                    tokens={summary.windowTokens}
                  />
                ) : null}
              </div>

              <Card>
                <CardHeader>
                  <CardDescription>{metricDetailCopy.detailTitle}</CardDescription>
                  <CardTitle>Newest days first</CardTitle>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                    <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
                      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Window posture</div>
                      <div className="mt-2 font-mono text-lg text-foreground">{summary.activeDays}/{dataForPeriod?.days.length ?? 0}</div>
                      <div className="text-sm text-muted-foreground">days with non-zero activity</div>
                    </div>
                    <div className="rounded-xl border border-border/70 bg-panel/75 px-3 py-3">
                      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Current lens</div>
                      <div className="mt-2 font-mono text-lg text-foreground">{metricMeta.label}</div>
                      <div className="text-sm text-muted-foreground">{metricDetailCopy.detailDescription}</div>
                    </div>
                  </div>

                  <div className="max-h-[32rem] space-y-2 overflow-y-auto pr-1">
                    {summary.recentDays.map((day) => {
                      const active = hasActivity(day)
                      const metricValue = getDailyMetricValue(day, metric)
                      const tokenBreakdown = getTokenBreakdownItems(day.tokens).filter((item) => item.value > 0)

                      return (
                        <div
                          key={day.date}
                          className="rounded-xl border border-border/70 bg-panel/65 px-3 py-3 transition-colors hover:bg-panel/85"
                        >
                          <div className="flex items-start justify-between gap-3">
                            <div>
                              <div className="font-medium text-foreground">{formatShortDate(day.date)}</div>
                              <div className="text-xs uppercase tracking-[0.14em] text-muted-foreground">
                                {active ? 'Activity recorded' : 'Zero-filled day'}
                              </div>
                            </div>
                            <div className="text-right">
                              <div className="font-mono text-sm text-foreground">
                                {metric === 'cost'
                                  ? formatCurrency(day.cost)
                                  : metric === 'requests'
                                    ? formatInteger(day.messages)
                                    : formatTokenCount(metricValue)}
                              </div>
                              <div className="text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                                {metricDetailCopy.metricLabel}
                              </div>
                            </div>
                          </div>

                          <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-muted-foreground">
                            <div className="rounded-lg bg-background/40 px-2.5 py-2">
                              <div className="uppercase tracking-[0.14em]">Sessions</div>
                              <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(day.sessions)}</div>
                            </div>
                            <div className="rounded-lg bg-background/40 px-2.5 py-2">
                              <div className="uppercase tracking-[0.14em]">Requests</div>
                              <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(day.messages)}</div>
                            </div>
                            <div className="rounded-lg bg-background/40 px-2.5 py-2">
                              <div className="uppercase tracking-[0.14em]">Tokens</div>
                              <div className="mt-1 font-mono text-sm text-foreground">{formatTokenCount(getTokenTotal(day.tokens))}</div>
                            </div>
                          </div>

                          {metric === 'tokens' && tokenBreakdown.length > 0 ? (
                            <div className="mt-3 flex flex-wrap gap-x-3 gap-y-2 text-[11px] uppercase tracking-[0.14em] text-muted-foreground">
                              {tokenBreakdown.map((item) => (
                                <div key={item.key} className="inline-flex items-center gap-2">
                                  <span
                                    aria-hidden="true"
                                    className="size-2 rounded-full border border-white/12"
                                    style={{ backgroundColor: item.color }}
                                  />
                                  <span>
                                    {item.label} <span className="font-mono text-foreground">{formatTokenCount(item.value)}</span>
                                  </span>
                                </div>
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

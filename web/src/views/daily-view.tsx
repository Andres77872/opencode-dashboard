import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { DailyChart } from '../components/daily/daily-chart'
import { MessageDetailSheet } from '../components/daily/message-detail-sheet'
import { getDailyMetricMeta, getDailyMetricValue, type DailyMetric } from '../components/daily/daily-metrics'
import { PeriodToggle } from '../components/daily/period-toggle'
import { RequestsHistoryTable, REQUESTS_SORT_DEFAULTS } from '../components/daily/requests-history-table'
import type { RequestsSortKey } from '../components/daily/requests-history-table'
import { MetricCard } from '../components/overview/metric-card'
import { TokenBreakdownList } from '../components/overview/token-breakdown-card'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { getDaily, getMessages } from '../lib/api'
import { formatCompactInteger, formatCurrency, formatInteger, formatShortDate, formatTokenCount, safeDivide } from '../lib/format'
import { getNextSortState } from '../lib/table-sort'
import type { SortState } from '../lib/table-sort'
import { getTokenTotal } from '../lib/token-breakdown'
import { isDailyPeriod, type DailyPeriod, type DailyStats, type DayStats, type MessageList } from '../types/api'

const REQUESTS_PAGE_SIZE = 12

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
  const [messages, setMessages] = useState<MessageList | null>(null)
  const [messagesLoading, setMessagesLoading] = useState(true)
  const [messagesError, setMessagesError] = useState<string | null>(null)
  const [messagesPage, setMessagesPage] = useState(1)
  const [messagesSort, setMessagesSort] = useState<SortState<RequestsSortKey> | null>(null)
  const [selectedMessageId, setSelectedMessageId] = useState<string | null>(null)
  const [metric, setMetric] = useState<DailyMetric>('cost')
  const dataByPeriodRef = useRef<Partial<Record<DailyPeriod, DailyStats>>>({})
  const messageTriggerRef = useRef<HTMLElement | null>(null)

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

  useEffect(() => {
    const controller = new AbortController()

    async function loadMessages() {
      setMessagesError(null)
      setMessagesLoading(true)

      try {
        const sortParam = messagesSort ? `${messagesSort.key}:${messagesSort.direction}` : undefined
        const next = await getMessages(period, messagesPage, REQUESTS_PAGE_SIZE, sortParam, controller.signal)
        setMessages(next)
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setMessagesError(caught instanceof Error ? caught.message : 'Failed to load requests history')
      } finally {
        if (!controller.signal.aborted) {
          setMessagesLoading(false)
        }
      }
    }

    void loadMessages()

    return () => controller.abort()
  }, [messagesPage, messagesSort, period, refreshNonce])

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
      peakDay: totals.peakDay,
      windowTokens: getWindowTokens(dataForPeriod),
    }
  }, [dataForPeriod])

  const metricMeta = getDailyMetricMeta(metric)
  const metricDetailCopy = dailyMetricCopy[metric]

  const handleRetry = () => {
    requestRefresh()
  }

  const handleSortChange = (key: RequestsSortKey) => {
    setMessagesSort(getNextSortState(messagesSort, key, REQUESTS_SORT_DEFAULTS[key]))
    setMessagesPage(1)
  }

  const handleOpenMessage = (messageId: string, trigger: HTMLElement) => {
    messageTriggerRef.current = trigger
    setSelectedMessageId(messageId)
  }

  const handlePeriodChange = (nextPeriod: DailyPeriod) => {
    if (nextPeriod === period) {
      return
    }

    setMessagesPage(1)
    setMessagesSort(null)
    setSelectedMessageId(null)

    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('period', nextPeriod)
      return next
    })
  }

  return (
    <>
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
          </>
        ) : null}

        <RequestsHistoryTable
          data={messages}
          error={messagesError}
          loading={messagesLoading}
          page={messagesPage}
          period={period}
          sortState={messagesSort}
          onOpenMessage={handleOpenMessage}
          onPageChange={setMessagesPage}
          onSortChange={handleSortChange}
          onRetry={handleRetry}
        />

        {summary ? (
          summary.empty ? (
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
              </div>

              <Card className="min-w-0 border-border/70 bg-panel/55 2xl:self-start">
                <CardHeader className="pb-3">
                  <CardDescription>{metricDetailCopy.detailTitle}</CardDescription>
                  <CardTitle>{summary.isHourly ? 'Newest hours first' : 'Newest days first'}</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <div className="flex items-center gap-2 rounded-lg border border-border/60 bg-panel/50 px-3 py-2 text-sm text-muted-foreground">
                    <span className="font-mono text-foreground">{summary.activeDays}/{dataForPeriod?.days.length ?? 0}</span>
                    <span>active {summary.isHourly ? 'hours' : 'days'}</span>
                    <span className="text-border">·</span>
                    <span>{metricMeta.label} lens</span>
                  </div>

                  {metric === 'tokens' ? (
                    <div className="rounded-xl border border-border/60 bg-background/35 px-3 py-3">
                      <div className="flex flex-wrap items-end justify-between gap-3">
                        <div>
                          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Window token mix</div>
                          <div className="mt-2 font-mono text-lg text-foreground">{formatTokenCount(summary.tokens)}</div>
                        </div>
                        <div className="text-right text-xs text-muted-foreground">
                          <div className="uppercase tracking-[0.14em]">Peak {summary.isHourly ? 'hour' : 'day'}</div>
                          <div className="mt-1 font-mono text-foreground">
                            {summary.peakDay ? formatShortDate(summary.peakDay.date) : 'No data'}
                          </div>
                        </div>
                      </div>

                      <TokenBreakdownList
                        className="mt-3 border-t border-border/50 pt-3"
                        hideZeroItems
                        tokens={summary.windowTokens}
                        variant="compact"
                      />
                    </div>
                  ) : null}

                  <div className="max-h-[32rem] divide-y divide-border/40 overflow-y-auto">
                    {summary.recentDays.map((day) => {
                      const active = hasActivity(day)
                      const metricValue = getDailyMetricValue(day, metric)

                      if (!active) {
                        return (
                          <div
                            key={day.date}
                            className="flex items-center gap-3 px-2 py-1.5 text-xs text-muted-foreground/50"
                          >
                            <span className="min-w-[4rem] font-medium">{formatShortDate(day.date)}</span>
                            <span className="italic">No activity</span>
                          </div>
                        )
                      }

                      return (
                        <div key={day.date} className="px-2 py-2">
                          <div className="flex items-center justify-between gap-2">
                            <div className="flex items-center gap-2 min-w-0">
                              <span className="text-sm font-semibold text-foreground whitespace-nowrap">{formatShortDate(day.date)}</span>
                              <span className="text-[11px] text-muted-foreground truncate">
                                {formatCompactInteger(day.sessions)} sess
                                <span className="mx-0.5 text-border/60">·</span>
                                {formatCompactInteger(day.messages)} req
                              </span>
                            </div>
                            <span className="font-mono text-sm font-medium text-foreground whitespace-nowrap">
                              {metric === 'cost'
                                ? formatCurrency(day.cost)
                                : metric === 'requests'
                                  ? formatInteger(day.messages)
                                  : formatTokenCount(metricValue)}
                            </span>
                          </div>

                          {metric === 'tokens' && getTokenTotal(day.tokens) > 0 ? (
                            <TokenBreakdownList
                              className="mt-1.5"
                              hideZeroItems
                              tokens={day.tokens}
                              variant="compact"
                            />
                          ) : null}
                        </div>
                      )
                    })}
                  </div>
                </CardContent>
              </Card>
            </div>
          )
        ) : null}
      </section>

      <MessageDetailSheet
        messageId={selectedMessageId}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedMessageId(null)
          }
        }}
        triggerRef={messageTriggerRef}
      />
    </>
  )
}

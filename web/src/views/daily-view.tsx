import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { DailyChart } from '../components/daily/daily-chart'
import { MessageDetailSheet } from '../components/daily/message-detail-sheet'
import { getDailyMetricMeta, getDailyMetricValue, type DailyMetric } from '../components/daily/daily-metrics'
import { PeriodToggle } from '../components/daily/period-toggle'
import type { PeriodMode } from '../components/daily/period-toggle'
import { RequestsHistoryTable, REQUESTS_SORT_DEFAULTS } from '../components/daily/requests-history-table'
import type { RequestsSortKey } from '../components/daily/requests-history-table'
import { MetricCard } from '../components/overview/metric-card'
import { TokenBreakdownList } from '../components/overview/token-breakdown-card'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../components/ui/tooltip'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { getDaily, getMessages } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { formatCompactInteger, formatCurrencyWithProvenance, formatInteger, formatShortDate, formatTokenCount, safeDivide } from '../lib/format'
import { getNextSortState } from '../lib/table-sort'
import type { SortState } from '../lib/table-sort'
import { getTokenTotal } from '../lib/token-breakdown'
import { usePeriodState, serializeCustomPeriod, applyPeriodToUrl } from '../lib/use-period-state'
import type { CustomPeriod, DailyPeriod, DailyStats, DayStats, MessageList } from '../types/api'

const REQUESTS_PAGE_SIZE = 12

const dailyMetricCopy: Record<DailyMetric, { detailTitle: string; detailDescription: string; metricLabel: string }> = {
  cost: {
    detailTitle: 'Day-by-day spend',
    detailDescription: 'Which days drive spend in the selected range.',
    metricLabel: 'Cost',
  },
  requests: {
    detailTitle: 'Daily cadence',
    detailDescription: 'Message volume per calendar day.',
    metricLabel: 'Messages',
  },
  tokens: {
    detailTitle: 'Token mix per day',
    detailDescription: 'How input, cache, output, reasoning, and writes distribute day by day.',
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

function getPeriodWindowHint(period: string, dayCount: number, granularity?: string) {
  // Custom range (serialized cache key starts with "from_")
  if (period.startsWith('from_')) {
    if (granularity === 'hour') {
      return `Custom range · ${dayCount} hourly buckets`
    }
    return `Custom range · ${formatCompactInteger(dayCount)} days`
  }

  // Hour presets (rolling window)
  if (['1h', '6h', '12h', '24h', '72h'].includes(period)) {
    if (granularity === 'hour') {
      return `Last ${period} · ${dayCount} hourly buckets`
    }
    return `Last ${period}`
  }

  // Day presets (server-timezone-aligned)
  if (granularity === 'hour') {
    return `Current day · ${dayCount} hourly buckets`
  }

  switch (period) {
    case '1d':
      return 'Current day (server timezone)'
    case '1y':
      return `${formatCompactInteger(dayCount)}-day trailing year`
    case 'all':
      return `All time · ${formatCompactInteger(dayCount)} days`
    default:
      return `${formatCompactInteger(dayCount)}-day window`
  }
}

function getEmptyWindowCopy(period: string, sourceLabel: string, sourceId: string) {
  if (sourceId === 'claude_code') {
    return 'No persisted Claude Code transcript activity was found in this selected window.'
  }

  if (period === 'all') {
    return `All recorded ${sourceLabel} activity since the first data point. Falls back to today when no data exists.`
  }

  return `Zero-filled window until ${sourceLabel} records sessions and messages in this period.`
}

export function DailyView() {
  const {
    requestRefresh,
    refreshNonce,
    selectedSourceId,
    selectedSourceInfo,
    sourceAvailable,
    sourceMetadataLoading,
    sourceStateError,
  } = useDashboardContext()
  const [, setSearchParams] = useSearchParams()
  const [messages, setMessages] = useState<MessageList | null>(null)
  const [messagesLoading, setMessagesLoading] = useState(true)
  const [messagesError, setMessagesError] = useState<string | null>(null)
  const [messagesPage, setMessagesPage] = useState(1)
  const [messagesSort, setMessagesSort] = useState<SortState<RequestsSortKey> | null>(null)
  const [selectedMessageId, setSelectedMessageId] = useState<string | null>(null)
  const [metric, setMetric] = useState<DailyMetric>('cost')
  const messageTriggerRef = useRef<HTMLElement | null>(null)

  const periodState = usePeriodState()
  const cacheKey = periodState.mode === 'custom' && periodState.customRange
    ? serializeCustomPeriod(periodState.customRange.from, periodState.customRange.to)
    : periodState.preset
  const period: string = cacheKey
  const { data: dataForPeriod, loading, error } = usePeriodResource(getDaily, cacheKey)
  const sourceLabel = selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  useEffect(() => {
    setMessagesPage(1)
    setMessagesSort(null)
    setSelectedMessageId(null)
    setMessages(null)
  }, [selectedSourceId])

  // Messages pagination stays inline — different state shape from daily stats
  useEffect(() => {
    if (sourceMetadataLoading) {
      setMessages(null)
      setMessagesError(null)
      setMessagesLoading(true)
      return
    }

    if (!sourceAvailable) {
      setMessages(null)
      setMessagesError(sourceStateError?.message ?? 'Selected source is unavailable')
      setMessagesLoading(false)
      return
    }

    const controller = new AbortController()

    async function loadMessages() {
      setMessagesError(null)
      setMessagesLoading(true)

      try {
        const sortParam = messagesSort ? `${messagesSort.key}:${messagesSort.direction}` : undefined
        const next = await getMessages(period, messagesPage, REQUESTS_PAGE_SIZE, sortParam, controller.signal, selectedSourceId)
        setMessages(next)
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setMessagesError(caught instanceof Error ? caught.message : 'Failed to load messages history')
      } finally {
        if (!controller.signal.aborted) {
          setMessagesLoading(false)
        }
      }
    }

    void loadMessages()

    return () => controller.abort()
  }, [messagesPage, messagesSort, period, refreshNonce, selectedSourceId, sourceAvailable, sourceMetadataLoading, sourceStateError?.message])

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

  const handlePresetChange = (nextPeriod: DailyPeriod) => {
    setMessagesPage(1)
    setMessagesSort(null)
    setSelectedMessageId(null)
    setSearchParams((previous) => {
      return applyPeriodToUrl(previous, { mode: 'preset', preset: nextPeriod })
    })
  }

  const handleCustomRangeChange = (range: CustomPeriod) => {
    setMessagesPage(1)
    setMessagesSort(null)
    setSelectedMessageId(null)
    setSearchParams((previous) => {
      return applyPeriodToUrl(previous, { mode: 'custom', customRange: range })
    })
  }

  const handleModeChange = (mode: PeriodMode) => {
    setMessagesPage(1)
    setMessagesSort(null)
    setSelectedMessageId(null)
    if (mode === 'preset') {
      setSearchParams((previous) => {
        return applyPeriodToUrl(previous, { mode: 'preset', preset: periodState.preset })
      })
    } else {
      setSearchParams((previous) => {
        return applyPeriodToUrl(previous, {
          mode: 'custom',
          customRange: periodState.customRange ?? { from: '' },
        })
      })
    }
  }

  if (loading && !dataForPeriod) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Daily</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Daily spend, message volume, and token-mix trends across the selected period. The URL stays shareable.
            </p>
          </div>
        </div>
        <DataPageSkeleton sections={['kpi-grid', 'chart', 'table']} tableRows={6} />
      </section>
    )
  }

  return (
    <>
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Daily</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Daily spend, message volume, and token-mix trends across the selected period. The URL stays shareable.
            </p>
          </div>

          <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
            <div className="text-sm text-muted-foreground">
              Endpoint:{' '}
              <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
                /api/v1/daily?period={period}{selectedSourceId !== 'opencode' ? `&source=${selectedSourceId}` : ''}
              </code>
            </div>
            <PeriodToggle
              mode={periodState.mode}
              preset={periodState.preset}
              customRange={periodState.customRange}
              onPresetChange={handlePresetChange}
              onCustomRangeChange={handleCustomRangeChange}
              onModeChange={handleModeChange}
              disabled={loading}
            />
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

        <TooltipProvider>
          <div className="grid gap-4 md:grid-cols-2 2xl:grid-cols-4">
              <MetricCard
                label="Total spend"
                value={summary ? formatCurrencyWithProvenance(summary.cost, dataForPeriod?.cost_status, dataForPeriod?.cost_provenance) : ''}
                hint={summary ? getPeriodWindowHint(period, dataForPeriod?.days.length ?? 0, dataForPeriod?.granularity) : 'Loading...'}
                loading={loading && !summary}
            />
            <MetricCard
              label="Sessions"
              value={summary ? formatInteger(summary.sessions) : ''}
              hint={summary ? `${formatCompactInteger(summary.activeDays)} active ${summary.isHourly ? 'hours' : 'days'} in window` : 'Loading...'}
              loading={loading && !summary}
            />
            <MetricCard
              label="Messages"
              value={summary ? formatInteger(summary.messages) : ''}
              hint={summary ? `${summary.averageMessagesPerSession.toFixed(1)} avg messages per session` : 'Loading...'}
              loading={loading && !summary}
            />
            <MetricCard
              label="Total tokens"
              value={summary ? formatTokenCount(summary.tokens) : ''}
              tooltipValue={summary ? `${formatInteger(summary.tokens)} tokens` : undefined}
              hint={summary ? `${formatTokenCount(Math.round(summary.averageTokensPerDay))} avg per calendar day` : 'Loading...'}
              loading={loading && !summary}
            />
          </div>
        </TooltipProvider>

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
                  {getEmptyWindowCopy(period, sourceLabel, selectedSourceId)}
                </p>
                <p>
                  Once data exists, charts, token breakdowns, and the message ledger will appear automatically.
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
                  <CardTitle className="text-base">{metricMeta.label} breakdown</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  <div className="flex items-center gap-2 rounded-lg border border-border/60 bg-panel/50 px-3 py-2 text-sm text-muted-foreground">
                    <span className="font-mono text-foreground">{summary.activeDays}/{dataForPeriod?.days.length ?? 0}</span>
                    <span>active {summary.isHourly ? 'hrs' : 'days'}</span>
                    <span className="text-border">·</span>
                    <span>{metricMeta.label} lens</span>
                  </div>

                  {metric === 'tokens' ? (
                    <div className="rounded-xl border border-border/60 bg-background/35 px-3 py-3">
                      <div className="flex flex-wrap items-end justify-between gap-3">
                        <div>
                          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Window token mix</div>
                          <TooltipProvider>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <div className="mt-2 cursor-default font-mono text-lg text-foreground transition-opacity hover:opacity-80">
                                  {formatTokenCount(summary.tokens)}
                                </div>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="font-mono">
                                <p>{formatInteger(summary.tokens)} tokens</p>
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
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
                                {formatCompactInteger(day.messages)} msg
                              </span>
                            </div>
                            <span className="font-mono text-sm font-medium text-foreground whitespace-nowrap">
                              {metric === 'cost'
                                ? formatCurrencyWithProvenance(day.cost, day.cost_status, day.cost_provenance)
                                : metric === 'requests'
                                  ? formatInteger(day.messages)
                                  : (
                                    <TooltipProvider>
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <span className="cursor-default transition-opacity hover:opacity-80">
                                            {formatTokenCount(metricValue)}
                                          </span>
                                        </TooltipTrigger>
                                        <TooltipContent side="top" className="font-mono">
                                          <p>{formatInteger(metricValue)} tokens</p>
                                        </TooltipContent>
                                      </Tooltip>
                                    </TooltipProvider>
                                  )}
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

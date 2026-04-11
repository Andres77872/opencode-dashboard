import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PeriodToggle } from '../components/daily/period-toggle'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { TokenBreakdownList } from '../components/overview/token-breakdown-card'
import { SessionMessageRow } from '../components/sessions/session-message-row'
import { SessionsSkeleton } from '../components/sessions/sessions-skeleton'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Progress } from '../components/ui/progress'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '../components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../components/ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../components/ui/tooltip'
import { getSessionDetail, getSessions } from '../lib/api'
import {
  formatCompactCurrency,
  formatCompactInteger,
  formatCurrency,
  formatDateTime,
  formatInteger,
  formatTokenCount,
  safeDivide,
} from '../lib/format'
import type { SessionDetail, SessionEntry, SessionList, SessionMessage, TokenStats } from '../types/api'
import { isDailyPeriod, type DailyPeriod } from '../types/api'

const PAGE_SIZE = 12

function getSessionLabel(session: Pick<SessionEntry, 'title'> | Pick<SessionDetail, 'title'>) {
  return session.title || 'Untitled session'
}

function getSessionProjectLabel(
  session: Pick<SessionEntry, 'project_name' | 'project_id'> | Pick<SessionDetail, 'project_name' | 'project_id'>,
) {
  return session.project_name || session.project_id || 'No linked project'
}

function getTokenTotal(tokens?: TokenStats) {
  if (!tokens) {
    return 0
  }

  return tokens.input + tokens.output + tokens.reasoning + tokens.cache.read + tokens.cache.write
}

function formatSessionWindow(createdAt: string, updatedAt: string) {
  const created = new Date(createdAt)
  const updated = new Date(updatedAt)
  const deltaMinutes = Math.max(0, Math.round((updated.getTime() - created.getTime()) / 60000))

  if (deltaMinutes < 1) {
    return 'Under 1 minute'
  }

  if (deltaMinutes < 60) {
    return `${deltaMinutes}m span`
  }

  const deltaHours = safeDivide(deltaMinutes, 60)
  if (deltaHours < 24) {
    return `${deltaHours.toFixed(deltaHours >= 10 ? 0 : 1)}h span`
  }

  return `${safeDivide(deltaHours, 24).toFixed(1)}d span`
}

function PaginationButton({
  disabled,
  onClick,
  children,
}: {
  disabled: boolean
  onClick: () => void
  children: string
}) {
  return (
    <Button variant="ghost" disabled={disabled} onClick={onClick} className="min-w-24">
      {children}
    </Button>
  )
}

function DetailMetric({ label, value, hint, tooltipValue }: { label: string; value: string; hint: string; tooltipValue?: string }) {
  const valueElement = tooltipValue ? (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="mt-2 cursor-default font-mono text-lg text-foreground transition-opacity hover:opacity-80 sm:text-xl">
            {value}
          </div>
        </TooltipTrigger>
        <TooltipContent side="top" className="font-mono">
          <p>{tooltipValue}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  ) : (
    <div className="mt-2 font-mono text-lg text-foreground sm:text-xl">{value}</div>
  )

  return (
    <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-4">
      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
      {valueElement}
      <div className="mt-2 text-sm leading-6 text-muted-foreground">{hint}</div>
    </div>
  )
}

function DetailFact({ label, value, subtle = false }: { label: string; value: string; subtle?: boolean }) {
  return (
    <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
      <div className={`mt-2 break-all font-mono text-sm ${subtle ? 'text-muted-foreground' : 'text-foreground'}`}>{value}</div>
    </div>
  )
}

export function SessionsView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const [data, setData] = useState<SessionList | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)
  const [detailRequestNonce, setDetailRequestNonce] = useState(0)
  const hasLoadedOnceRef = useRef(false)

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = isDailyPeriod(rawPeriod) ? rawPeriod : '7d'

  // Derive page from URL (single source of truth).
  const pageFromUrl = parseInt(searchParams.get('page') ?? '1', 10)
  const page = isNaN(pageFromUrl) || pageFromUrl < 1 ? 1 : pageFromUrl

  // Normalize missing/invalid period to ?period=7d and ensure page has a value on mount.
  useEffect(() => {
    const needsNormalization = !isDailyPeriod(rawPeriod) || !searchParams.has('page')
    if (needsNormalization) {
      setSearchParams((previous) => {
        const next = new URLSearchParams(previous)
        if (!isDailyPeriod(rawPeriod)) {
          next.set('period', '7d')
        }
        if (!next.has('page')) {
          next.set('page', '1')
        }
        return next
      })
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // Helper to update page in URL.
  const setPage = (updater: number | ((prev: number) => number)) => {
    const nextPage = typeof updater === 'function' ? updater(page) : updater
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('page', String(nextPage))
      return next
    })
  }

  useEffect(() => {
    const controller = new AbortController()

    async function loadSessions() {
      setRefreshing(true)
      setError(null)

      if (!hasLoadedOnceRef.current) {
        setLoading(true)
      }

      try {
        const next = await getSessions(page, PAGE_SIZE, period, controller.signal)
        setData(next)
        hasLoadedOnceRef.current = true
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setError(caught instanceof Error ? caught.message : 'Failed to load sessions')
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void loadSessions()

    return () => controller.abort()
  }, [page, period, refreshNonce, setLastUpdatedAt, setRefreshing])

  useEffect(() => {
    if (!selectedSessionId) {
      setDetail(null)
      setDetailError(null)
      setDetailLoading(false)
      return
    }

    const sessionId = selectedSessionId

    const controller = new AbortController()

    async function loadDetail() {
      setDetail(null)
      setDetailError(null)
      setDetailLoading(true)

      try {
        const next = await getSessionDetail(sessionId, controller.signal)
        setDetail(next)
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setDetailError(caught instanceof Error ? caught.message : 'Failed to load session detail')
      } finally {
        if (!controller.signal.aborted) {
          setDetailLoading(false)
        }
      }
    }

    void loadDetail()

    return () => controller.abort()
  }, [detailRequestNonce, selectedSessionId])

  const triggerButtonRef = useRef<HTMLButtonElement | null>(null)

  const summary = useMemo(() => {
    if (!data) {
      return null
    }

    const totalPages = Math.max(1, Math.ceil(data.total / data.page_size))
    const firstVisible = data.total === 0 ? 0 : (data.page - 1) * data.page_size + 1
    const lastVisible = data.total === 0 ? 0 : firstVisible + data.sessions.length - 1
    const visibleCost = data.sessions.reduce((accumulator, session) => accumulator + session.cost, 0)
    const visibleMessages = data.sessions.reduce((accumulator, session) => accumulator + session.message_count, 0)
    const visibleProjects = new Set(data.sessions.map((session) => getSessionProjectLabel(session))).size
    const hottestSession = [...data.sessions].sort((left, right) => right.cost - left.cost)[0] ?? null

    return {
      totalPages,
      firstVisible,
      lastVisible,
      visibleCost,
      visibleMessages,
      visibleProjects,
      hottestSession,
      empty: data.sessions.length === 0,
    }
  }, [data])

  const detailMessageMix = useMemo(() => {
    if (!detail) {
      return { assistant: 0, user: 0, other: 0 }
    }

    return detail.messages.reduce(
      (accumulator, message) => {
        if (message.role === 'assistant') {
          accumulator.assistant += 1
        } else if (message.role === 'user') {
          accumulator.user += 1
        } else {
          accumulator.other += 1
        }

        return accumulator
      },
      { assistant: 0, user: 0, other: 0 },
    )
  }, [detail])

  const detailMessageStats = useMemo(() => {
    if (!detail) {
      return {
        hottestMessageId: null as string | null,
        hottestMessageCost: 0,
        heaviestTokenMessageId: null as string | null,
        heaviestTokenTotal: 0,
      }
    }

    let hottestMessage: SessionMessage | null = null
    let heaviestTokenMessage: SessionMessage | null = null

    for (const message of detail.messages) {
      if (!hottestMessage || (message.cost ?? 0) > (hottestMessage.cost ?? 0)) {
        hottestMessage = message
      }

      if (!heaviestTokenMessage || getTokenTotal(message.tokens) > getTokenTotal(heaviestTokenMessage.tokens)) {
        heaviestTokenMessage = message
      }
    }

    return {
      hottestMessageId: hottestMessage?.id ?? null,
      hottestMessageCost: hottestMessage?.cost ?? 0,
      heaviestTokenMessageId: heaviestTokenMessage?.id ?? null,
      heaviestTokenTotal: heaviestTokenMessage ? getTokenTotal(heaviestTokenMessage.tokens) : 0,
    }
  }, [detail])

  const handleRetry = () => {
    requestRefresh()
  }

  const handlePeriodChange = (nextPeriod: DailyPeriod) => {
    if (nextPeriod === period) {
      return
    }

    // Reset page to 1 and update period in a single URL write, preserving all other params (search, filters, etc.).
    setSearchParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('period', nextPeriod)
      next.set('page', '1')
      return next
    })
  }

  const handleDetailRetry = () => {
    if (selectedSessionId) {
      setDetailRequestNonce((current) => current + 1)
    }
  }

  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Sessions</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Dense session browsing from <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions</code>
              {' '}with detail hydration from <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions/:id</code>.
            </p>
          </div>
          <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
            <div className="text-sm text-muted-foreground">
              Endpoints:{' '}
              <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions</code>
              {' '}+{' '}
              <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions/:id</code>
            </div>
            <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
          </div>
        </div>
        <SessionsSkeleton />
      </section>
    )
  }

  return (
    <>
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Sessions</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Modern dark session triage with dense scan lines, honest pagination, and a real metadata drawer instead of fake transcript theater.
            </p>
          </div>

          <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
            <div className="text-sm text-muted-foreground">
              Endpoints:{' '}
              <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions?period={period}</code>
              {' '}+{' '}
              <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions/:id</code>
            </div>
            <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
          </div>
        </div>

        {error ? (
          <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="font-medium text-foreground">Sessions failed to load</div>
              <div className="text-sm opacity-90">{error}</div>
            </div>
            <Button variant="ghost" onClick={handleRetry}>
              Retry
            </Button>
          </Alert>
        ) : null}

        {summary ? (
          <>
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
              <MetricCard
                label="Total sessions"
                value={formatInteger(data?.total ?? 0)}
                hint={summary.empty ? 'No sessions recorded in the current database' : `${formatInteger(summary.totalPages)} pages at ${formatInteger(data?.page_size ?? PAGE_SIZE)} rows each`}
              />
              <MetricCard
                label="Visible window"
                value={summary.empty ? '0' : `${summary.firstVisible}-${summary.lastVisible}`}
                hint={summary.empty ? 'Nothing to paginate yet' : `Page ${formatInteger(data?.page ?? 1)} of ${formatInteger(summary.totalPages)}`}
              />
              <MetricCard
                label="Visible cost"
                value={formatCurrency(summary.visibleCost)}
                hint={`${formatCompactInteger(summary.visibleMessages)} messages across the current page`}
              />
              <MetricCard
                label="Projects on page"
                value={formatInteger(summary.visibleProjects)}
                hint={summary.hottestSession ? `${getSessionLabel(summary.hottestSession)} is the highest visible spend session` : 'Awaiting session activity'}
              />
            </div>

            {summary.empty ? (
              <Card>
                <CardHeader>
                  <CardDescription>Empty state</CardDescription>
                  <CardTitle>No sessions recorded yet</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3 text-sm text-muted-foreground">
                  <p>
                    This route stays empty until the database contains session rows. Once activity exists, you get paginated browsing, per-session spend, message counts, and metadata detail.
                  </p>
                  <p>
                    The detail drawer intentionally stays metadata-only because the backend does not expose transcript text in the current session detail payload.
                  </p>
                </CardContent>
              </Card>
            ) : (
              <Card>
                <CardHeader className="gap-3 lg:flex-row lg:items-end lg:justify-between">
                  <div className="space-y-1.5">
                    <CardDescription>Primary artifact</CardDescription>
                    <CardTitle>Session index</CardTitle>
                  </div>

                  <div className="flex flex-wrap items-center gap-2">
                    <Badge tone="success">Dense table</Badge>
                    <Badge>Page {data?.page ?? 1}</Badge>
                    {summary.hottestSession ? <Badge tone="accent">Top visible spend · {getSessionLabel(summary.hottestSession)}</Badge> : null}
                  </div>
                </CardHeader>

                <CardContent className="space-y-4">
                  <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_18rem]">
                    <div className="space-y-3">
                      <div className="hidden md:block">
                        <Table className="min-w-[42rem] overflow-hidden rounded-2xl border border-border/70 bg-card/40">
                          <TableHeader className="bg-panel/75">
                            <TableRow className="border-b border-border/70 hover:bg-transparent">
                              <TableHead className="min-w-[15rem] px-4 py-3 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Session</TableHead>
                              <TableHead className="w-[11rem] px-4 py-3 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Activity</TableHead>
                              <TableHead className="w-[9rem] px-4 py-3 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Summary</TableHead>
                              <TableHead className="w-[5rem] px-4 py-3 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Open</TableHead>
                            </TableRow>
                          </TableHeader>
                          <TableBody className="divide-y divide-border/60">
                           {data?.sessions.map((session) => {
                             const share = safeDivide(session.cost, summary.visibleCost) * 100

                             return (
                                <TableRow key={session.id} className="bg-card/40 hover:bg-white/4">
                                  <TableCell className="min-w-[15rem] px-4 py-3">
                                    <div className="min-w-0 space-y-2">
                                      <div className="truncate font-medium text-foreground">{getSessionLabel(session)}</div>
                                      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                                        <Badge className="max-w-full px-2 py-0.5 text-[10px] tracking-[0.16em]">{getSessionProjectLabel(session)}</Badge>
                                        <span className="font-mono">id {session.id.slice(0, 10)}</span>
                                      </div>
                                      <div className="flex items-center gap-3">
                                        <Progress className="h-1.5" value={Math.max(share, session.cost > 0 ? 3 : 0)} />
                                        <span className="font-mono text-[11px] text-muted-foreground">{Math.round(share || 0)}%</span>
                                      </div>
                                    </div>
                                  </TableCell>
                                  <TableCell className="w-[11rem] px-4 py-3">
                                    <div className="space-y-2 text-xs text-muted-foreground">
                                      <div>
                                        <div className="uppercase tracking-[0.14em]">Created</div>
                                        <div className="mt-1 font-mono text-sm text-foreground">{formatDateTime(session.time_created)}</div>
                                      </div>
                                      <div>
                                        <div className="uppercase tracking-[0.14em]">Updated</div>
                                        <div className="mt-1 font-mono text-sm text-foreground">{formatDateTime(session.time_updated)}</div>
                                      </div>
                                    </div>
                                  </TableCell>
                                  <TableCell className="w-[9rem] px-4 py-3">
                                    <div className="space-y-2 text-xs text-muted-foreground">
                                      <div>
                                        <div className="uppercase tracking-[0.14em]">Messages</div>
                                        <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(session.message_count)}</div>
                                      </div>
                                      <div>
                                        <div className="uppercase tracking-[0.14em]">Cost</div>
                                        <div className="mt-1 font-mono text-sm text-foreground">{formatCompactCurrency(session.cost)}</div>
                                      </div>
                                    </div>
                                  </TableCell>
                                  <TableCell className="w-[5rem] px-4 py-3">
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                     onClick={(e) => {
                                       triggerButtonRef.current = e.currentTarget
                                       setSelectedSessionId(session.id)
                                      }}
                                      aria-label={`View details for ${getSessionLabel(session)}`}
                                      className="w-full justify-center text-accent"
                                    >
                                      Open
                                    </Button>
                                  </TableCell>
                                </TableRow>
                              )
                            })}
                          </TableBody>
                        </Table>
                      </div>

                      <div className="space-y-3 md:hidden">
                        {data?.sessions.map((session) => (
                          <button
                              key={session.id}
                              type="button"
                              onClick={(e) => {
                                triggerButtonRef.current = e.currentTarget
                                setSelectedSessionId(session.id)
                             }}
                              className="w-full rounded-2xl border border-border/70 bg-panel/65 p-4 text-left transition-colors hover:bg-panel/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/70"
                              aria-label={`View details for ${getSessionLabel(session)}`}
                            >
                              <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0">
                                  <div className="truncate font-medium text-foreground">{getSessionLabel(session)}</div>
                                  <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                                    <span className="uppercase tracking-[0.14em]">{getSessionProjectLabel(session)}</span>
                                    <span aria-hidden="true">•</span>
                                    <span className="font-mono">id {session.id.slice(0, 10)}</span>
                                  </div>
                                </div>
                                <div className="font-mono text-sm text-foreground">{formatCompactCurrency(session.cost)}</div>
                              </div>

                              <div className="mt-3 rounded-xl border border-border/60 bg-background/35 px-3 py-2.5 text-xs text-muted-foreground">
                                <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
                                  <span>
                                    <span className="uppercase tracking-[0.14em]">Messages</span>{' '}
                                    <span className="font-mono text-foreground">{formatCompactInteger(session.message_count)}</span>
                                  </span>
                                  <span>
                                    <span className="uppercase tracking-[0.14em]">Created</span>{' '}
                                    <span className="font-mono text-foreground">{formatDateTime(session.time_created)}</span>
                                  </span>
                                  <span>
                                    <span className="uppercase tracking-[0.14em]">Updated</span>{' '}
                                    <span className="font-mono text-foreground">{formatDateTime(session.time_updated)}</span>
                                  </span>
                                </div>
                              </div>

                              <div className="mt-3 text-sm font-medium text-accent">Open detail</div>
                           </button>
                        ))}
                      </div>

                      <div className="flex flex-col gap-3 rounded-2xl border border-border/70 bg-panel/50 px-4 py-4 sm:flex-row sm:items-center sm:justify-between">
                        <div className="space-y-1 text-sm text-muted-foreground">
                          <div className="font-medium text-foreground">
                            Showing {summary.firstVisible}-{summary.lastVisible} of {formatInteger(data?.total ?? 0)} sessions
                          </div>
                          <div>
                            Backend pagination is authoritative: <span className="font-mono text-foreground">page={data?.page ?? 1}</span>,{' '}
                            <span className="font-mono text-foreground">page_size={data?.page_size ?? PAGE_SIZE}</span>,{' '}
                            <span className="font-mono text-foreground">total={data?.total ?? 0}</span>
                          </div>
                        </div>

                        <div className="flex flex-wrap gap-2">
                          <PaginationButton disabled={(data?.page ?? 1) <= 1} onClick={() => setPage((current) => Math.max(1, current - 1))}>
                            Previous
                          </PaginationButton>
                          <PaginationButton
                            disabled={(data?.page ?? 1) >= summary.totalPages}
                            onClick={() => setPage((current) => current + 1)}
                          >
                            Next
                          </PaginationButton>
                        </div>
                      </div>
                    </div>

                    <Card className="border-border/70 bg-panel/55 2xl:sticky 2xl:top-24">
                      <CardHeader>
                        <CardDescription>Session cues</CardDescription>
                        <CardTitle>Read the page faster</CardTitle>
                      </CardHeader>
                      <CardContent className="space-y-3 text-sm text-muted-foreground">
                        <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Top visible spend</div>
                          <div className="mt-2 font-mono text-base text-foreground">
                            {summary.hottestSession ? getSessionLabel(summary.hottestSession) : 'No data'}
                          </div>
                          <div className="mt-1 text-sm text-muted-foreground">
                            {summary.hottestSession
                              ? `${formatCurrency(summary.hottestSession.cost)} · ${formatCompactInteger(summary.hottestSession.message_count)} messages`
                              : 'Awaiting activity'}
                          </div>
                        </div>

                        <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Detail drawer behavior</div>
                          <div className="mt-2 text-sm leading-6 text-muted-foreground">
                            Open any row to pull live session metadata. The drawer fetches separately, so list browsing stays responsive even if detail hydration fails.
                          </div>
                        </div>

                        <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Backend caveat</div>
                          <div className="mt-2 text-sm leading-6 text-muted-foreground">
                            Session detail exposes role, time, model, provider, agent, cost, and token metadata. It does <span className="font-semibold text-foreground">not</span> expose message transcript text yet, so this slice deliberately avoids fake conversation rendering.
                          </div>
                        </div>

                        <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Scan priority</div>
                          <div className="mt-2 text-sm leading-6 text-muted-foreground">
                            Preserve title, activity, message count, and cost first. Everything else belongs in the inspector once a session earns attention.
                          </div>
                        </div>
                      </CardContent>
                    </Card>
                  </div>
                </CardContent>
              </Card>
            )}
          </>
        ) : error ? (
          <Card>
            <CardHeader>
              <CardDescription>Unavailable</CardDescription>
              <CardTitle>Session list could not be loaded</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <p>The shell stays up, but this slice has no data yet because the sessions request failed.</p>
              <Button variant="ghost" onClick={handleRetry}>
                Retry sessions request
              </Button>
            </CardContent>
          </Card>
        ) : null}
      </section>

      <Sheet
        open={selectedSessionId !== null}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedSessionId(null)
          }
        }}
      >
        <SheetContent
          side="right"
          className="flex h-full w-full max-w-[calc(100vw-0.75rem)] flex-col overflow-hidden border-l border-border/70 bg-background shadow-[0_24px_100px_-32px_rgba(0,0,0,0.95)] sm:max-w-[42rem] xl:max-w-[min(100vw-2rem,72rem)] 2xl:max-w-[78rem]"
          onCloseAutoFocus={(e) => {
            // Prevent default focus return and manually focus trigger
            e.preventDefault()
            if (triggerButtonRef.current) {
              triggerButtonRef.current.focus()
            }
          }}
        >
          <SheetHeader className="sticky top-0 z-10 border-b border-border/70 bg-background/95 px-4 py-4 pr-14 backdrop-blur-xl sm:px-6 sm:pr-16">
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone="accent">Telemetry inspector</Badge>
                {detail ? <Badge>{getSessionProjectLabel(detail)}</Badge> : null}
                {detail ? <span className="font-mono text-xs text-muted-foreground">id {detail.id.slice(0, 12)}</span> : null}
              </div>

              <div className="space-y-2">
                <div>
                  <SheetTitle className="sr-only">Session Detail</SheetTitle>
                  <h3 className="text-lg font-semibold tracking-tight text-foreground sm:text-xl">
                    {detail ? getSessionLabel(detail) : 'Loading session detail'}
                  </h3>
                  <SheetDescription className="sr-only">Session metadata drawer for a single recorded session.</SheetDescription>
                </div>

                <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                  {detail ? (
                    <>
                      <span className="font-mono text-foreground">{formatDateTime(detail.time_created)}</span>
                      <span aria-hidden="true">•</span>
                      <span>{formatSessionWindow(detail.time_created, detail.time_updated)}</span>
                      <span aria-hidden="true">•</span>
                      <span>{formatInteger(detail.message_count)} recorded messages</span>
                    </>
                  ) : (
                    <span>Fetching live session telemetry…</span>
                  )}
                </div>

                <div className="rounded-xl border border-border/70 bg-panel/40 px-3 py-2 text-sm text-muted-foreground">
                  Telemetry inspector only — transcript text is not available from this endpoint.
                </div>
              </div>
            </div>
          </SheetHeader>

          <div className="min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-4 py-5 sm:px-6">
            {detailLoading ? (
              <div className="space-y-4">
                <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                  {Array.from({ length: 4 }).map((_, index) => (
                    <div key={index} className="rounded-2xl border border-border/70 bg-panel/45 px-4 py-4">
                      <div className="h-3 w-24 rounded bg-white/8" />
                      <div className="mt-3 h-8 w-28 rounded bg-white/8" />
                      <div className="mt-3 h-4 w-40 rounded bg-white/8" />
                    </div>
                  ))}
                </div>
                {Array.from({ length: 5 }).map((_, index) => (
                  <div key={index} className="h-28 rounded-2xl border border-border/70 bg-panel/45" />
                ))}
              </div>
            ) : detailError ? (
              <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div className="font-medium text-foreground">Session detail failed to load</div>
                  <div className="text-sm opacity-90">{detailError}</div>
                </div>
                <Button variant="ghost" onClick={handleDetailRetry}>
                  Retry detail
                </Button>
              </Alert>
            ) : detail ? (
              <div className="space-y-6">
                <div className="grid gap-3 md:grid-cols-2 2xl:grid-cols-4">
                  <DetailMetric
                    label="Recorded rows"
                    value={formatInteger(detail.messages.length)}
                    hint={`${formatInteger(detail.message_count)} total messages reported · ${formatInteger(detailMessageMix.user)} user · ${formatInteger(detailMessageMix.assistant)} assistant`}
                  />
                  <DetailMetric
                    label="Session spend"
                    value={formatCurrency(detail.total_cost)}
                    hint={detail.messages.length > 0 ? `Peak row ${formatCurrency(detailMessageStats.hottestMessageCost)} · ${formatCurrency(safeDivide(detail.total_cost, detail.messages.length))} average per recorded row` : 'No message rows'}
                  />
                  <DetailMetric
                    label="Token load"
                    value={formatTokenCount(getTokenTotal(detail.total_tokens))}
                    tooltipValue={`${formatInteger(getTokenTotal(detail.total_tokens))} tokens`}
                    hint={detailMessageStats.heaviestTokenTotal > 0 ? `Heaviest row ${formatTokenCount(detailMessageStats.heaviestTokenTotal)} · ${formatTokenCount(detail.total_tokens.input)} input · ${formatTokenCount(detail.total_tokens.output)} output` : 'No token activity recorded'}
                  />
                  <DetailMetric
                    label="Capture window"
                    value={formatSessionWindow(detail.time_created, detail.time_updated)}
                    hint={`Created ${formatDateTime(detail.time_created)} · updated ${formatDateTime(detail.time_updated)}`}
                  />
                </div>

                <div className="grid min-w-0 gap-4 2xl:grid-cols-[minmax(0,1.55fr)_minmax(18rem,22rem)]">
                  <div className="min-w-0 space-y-4">
                    <Card className="border-border/70 bg-panel/45">
                      <CardHeader className="gap-3 md:flex-row md:items-end md:justify-between">
                        <CardDescription>Primary review surface</CardDescription>
                        <div className="space-y-1.5">
                          <CardTitle>Message timeline</CardTitle>
                          <p className="text-sm text-muted-foreground">Scan role, time, spend, and total tokens first. Model, provider, agent, and ids stay present but quieter.</p>
                        </div>
                      </CardHeader>
                      <CardContent className="space-y-4">
                        <div className="flex flex-wrap items-center gap-2 rounded-xl border border-border/60 bg-background/35 px-3 py-2 text-sm text-muted-foreground">
                          <span className="font-mono text-foreground">{formatInteger(detailMessageMix.user)}</span>
                          <span>user</span>
                          <span className="text-border">·</span>
                          <span className="font-mono text-foreground">{formatInteger(detailMessageMix.assistant)}</span>
                          <span>assistant</span>
                          {detailMessageMix.other > 0 ? (
                            <>
                              <span className="text-border">·</span>
                              <span className="font-mono text-foreground">{formatInteger(detailMessageMix.other)}</span>
                              <span>other</span>
                            </>
                          ) : null}
                          {detailMessageStats.hottestMessageCost > 0 ? (
                            <>
                              <span className="text-border">·</span>
                              <span>peak row {formatCurrency(detailMessageStats.hottestMessageCost)}</span>
                            </>
                          ) : null}
                        </div>

                        {detail.messages.length === 0 ? (
                          <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-5 text-sm text-muted-foreground">
                            This session exists, but the detail endpoint returned no message rows.
                          </div>
                        ) : (
                          <div className="divide-y divide-border/40 overflow-hidden rounded-xl border border-border/60">
                            {detail.messages.map((message, index) => (
                              <SessionMessageRow
                                key={message.id}
                                message={message}
                                previousMessage={index > 0 ? detail.messages[index - 1] : undefined}
                                isHighestCost={detailMessageStats.hottestMessageId === message.id}
                                isHighestTokens={detailMessageStats.heaviestTokenMessageId === message.id}
                              />
                            ))}
                          </div>
                        )}
                      </CardContent>
                    </Card>
                  </div>

                  <div className="order-last min-w-0 space-y-4 2xl:order-none 2xl:pt-1">
                    <Card className="border-border/60 bg-background/25 shadow-none">
                      <CardHeader>
                        <CardDescription>Secondary context</CardDescription>
                        <CardTitle>Session facts</CardTitle>
                      </CardHeader>
                      <CardContent className="space-y-3 text-sm text-muted-foreground">
                        <div className="flex items-center gap-2 rounded-xl border border-border/60 bg-background/35 px-3 py-2 text-xs leading-5 text-muted-foreground">
                          <TooltipProvider>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="cursor-default font-mono text-foreground transition-opacity hover:opacity-80">
                                  {formatTokenCount(getTokenTotal(detail.total_tokens))}
                                </span>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="font-mono">
                                <p>{formatInteger(getTokenTotal(detail.total_tokens))} tokens</p>
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                          <span>window token load</span>
                          <span className="text-border">·</span>
                          <span>{formatCurrency(detail.total_cost)} spend</span>
                        </div>

                        <div className="rounded-xl border border-border/60 bg-background/35 px-3 py-3">
                          <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Token mix</div>
                          <TokenBreakdownList
                            className="mt-3 border-t border-border/50 pt-3"
                            hideZeroItems
                            tokens={detail.total_tokens}
                            variant="compact"
                          />
                        </div>

                        <div className="grid gap-3 sm:grid-cols-2 2xl:grid-cols-1">
                          <DetailFact label="Project" value={getSessionProjectLabel(detail)} />
                          <DetailFact label="Directory" value={detail.directory || 'No directory recorded'} subtle={!detail.directory} />
                          <DetailFact label="Last update" value={formatDateTime(detail.time_updated)} />
                          <DetailFact
                            label="Peak row"
                            value={detailMessageStats.hottestMessageCost > 0 ? formatCurrency(detailMessageStats.hottestMessageCost) : 'No spend signal'}
                            subtle={detailMessageStats.hottestMessageCost <= 0}
                          />
                        </div>
                      </CardContent>
                    </Card>
                  </div>
                </div>
              </div>
            ) : null}
          </div>
        </SheetContent>
      </Sheet>
    </>
  )
}

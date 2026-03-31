import { useEffect, useMemo, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { SessionsSkeleton } from '../components/sessions/sessions-skeleton'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
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
import { cn } from '../lib/utils'
import type { SessionDetail, SessionEntry, SessionList, SessionMessage, TokenStats } from '../types/api'

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

function getRoleTone(role: string) {
  switch (role) {
    case 'assistant':
      return 'accent' as const
    case 'user':
      return 'success' as const
    case 'system':
      return 'warning' as const
    default:
      return 'default' as const
  }
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

function DetailMetric({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-4">
      <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">{label}</div>
      <div className="mt-2 font-mono text-lg text-foreground">{value}</div>
      <div className="mt-1 text-sm text-muted-foreground">{hint}</div>
    </div>
  )
}

function SessionMessageRow({ message }: { message: SessionMessage }) {
  const tokenTotal = getTokenTotal(message.tokens)

  return (
    <div className="rounded-2xl border border-border/70 bg-background/40 px-4 py-4">
      <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={getRoleTone(message.role)}>{message.role || 'unknown'}</Badge>
            <Badge>{formatDateTime(message.time_created)}</Badge>
            {message.agent ? <Badge tone="warning">agent · {message.agent}</Badge> : null}
          </div>

          <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
            <span className="rounded-full border border-border/70 bg-panel/55 px-2.5 py-1 font-mono">id {message.id.slice(0, 12)}</span>
            {message.model_id ? (
              <span className="rounded-full border border-border/70 bg-panel/55 px-2.5 py-1 font-mono">
                model {message.model_id}
              </span>
            ) : null}
            {message.provider_id ? (
              <span className="rounded-full border border-border/70 bg-panel/55 px-2.5 py-1 font-mono">
                provider {message.provider_id}
              </span>
            ) : null}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground sm:grid-cols-4 xl:min-w-[22rem]">
          <div className="rounded-xl bg-panel/55 px-3 py-2">
            <div className="uppercase tracking-[0.14em]">Cost</div>
            <div className="mt-1 font-mono text-sm text-foreground">{formatCurrency(message.cost ?? 0)}</div>
          </div>
          <div className="rounded-xl bg-panel/55 px-3 py-2">
            <div className="uppercase tracking-[0.14em]">Tokens</div>
            <div className="mt-1 font-mono text-sm text-foreground">{formatTokenCount(tokenTotal)}</div>
          </div>
          <div className="rounded-xl bg-panel/55 px-3 py-2">
            <div className="uppercase tracking-[0.14em]">Input</div>
            <div className="mt-1 font-mono text-sm text-foreground">{formatTokenCount(message.tokens?.input ?? 0)}</div>
          </div>
          <div className="rounded-xl bg-panel/55 px-3 py-2">
            <div className="uppercase tracking-[0.14em]">Output</div>
            <div className="mt-1 font-mono text-sm text-foreground">{formatTokenCount(message.tokens?.output ?? 0)}</div>
          </div>
        </div>
      </div>
    </div>
  )
}

export function SessionsView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [data, setData] = useState<SessionList | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)
  const [detailRequestNonce, setDetailRequestNonce] = useState(0)
  const hasLoadedOnceRef = useRef(false)

  useEffect(() => {
    const controller = new AbortController()

    async function loadSessions() {
      setRefreshing(true)
      setError(null)

      if (!hasLoadedOnceRef.current) {
        setLoading(true)
      }

      try {
        const next = await getSessions(page, PAGE_SIZE, controller.signal)
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
  }, [page, refreshNonce, setLastUpdatedAt, setRefreshing])

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

  useEffect(() => {
    if (!selectedSessionId) {
      return
    }

    const previousOverflow = document.body.style.overflow

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        setSelectedSessionId(null)
      }
    }

    document.body.style.overflow = 'hidden'
    window.addEventListener('keydown', handleKeyDown)

    return () => {
      document.body.style.overflow = previousOverflow
      window.removeEventListener('keydown', handleKeyDown)
    }
  }, [selectedSessionId])

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

  const handleRetry = () => {
    requestRefresh()
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

          <div className="text-sm text-muted-foreground">
            Endpoints:{' '}
            <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions</code>
            {' '}+{' '}
            <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions/:id</code>
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
                  <div className="grid gap-4 xl:grid-cols-[1.45fr_20rem]">
                    <div className="space-y-3">
                      <div className="hidden overflow-hidden rounded-2xl border border-border/70 lg:block">
                        <div className="grid grid-cols-[minmax(18rem,1.7fr)_8rem_8rem_6rem_7rem_5.5rem] gap-3 border-b border-border/70 bg-panel/75 px-4 py-3">
                          <div className="px-1 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Session</div>
                          <div className="px-1 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Created</div>
                          <div className="px-1 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Updated</div>
                          <div className="px-1 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Msgs</div>
                          <div className="px-1 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Cost</div>
                          <div className="px-1 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Detail</div>
                        </div>

                        <div className="divide-y divide-border/60">
                          {data?.sessions.map((session) => {
                            const share = safeDivide(session.cost, summary.visibleCost) * 100

                            return (
                              <button
                                key={session.id}
                                type="button"
                                onClick={() => setSelectedSessionId(session.id)}
                                className="grid w-full grid-cols-[minmax(18rem,1.7fr)_8rem_8rem_6rem_7rem_5.5rem] gap-3 bg-card/40 px-4 py-3 text-left transition-colors hover:bg-white/4 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/70"
                              >
                                <div className="min-w-0 space-y-2">
                                  <div className="truncate font-medium text-foreground">{getSessionLabel(session)}</div>
                                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                                    <Badge className="px-2 py-0.5 text-[10px] tracking-[0.16em]">{getSessionProjectLabel(session)}</Badge>
                                    <span className="font-mono">id {session.id.slice(0, 10)}</span>
                                  </div>
                                  <div className="flex items-center gap-3">
                                    <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-background/80">
                                      <div
                                        className="h-full rounded-full bg-linear-to-r from-accent/60 to-accent"
                                        style={{ width: `${Math.max(share, session.cost > 0 ? 3 : 0)}%` }}
                                      />
                                    </div>
                                    <span className="font-mono text-[11px] text-muted-foreground">{Math.round(share || 0)}%</span>
                                  </div>
                                </div>
                                <div className="font-mono text-sm text-foreground">{formatDateTime(session.time_created)}</div>
                                <div className="font-mono text-sm text-foreground">{formatDateTime(session.time_updated)}</div>
                                <div className="font-mono text-sm text-foreground">{formatCompactInteger(session.message_count)}</div>
                                <div className="font-mono text-sm text-foreground">{formatCompactCurrency(session.cost)}</div>
                                <div className="pt-1 text-sm font-medium text-accent">Open</div>
                              </button>
                            )
                          })}
                        </div>
                      </div>

                      <div className="space-y-3 lg:hidden">
                        {data?.sessions.map((session) => (
                          <button
                            key={session.id}
                            type="button"
                            onClick={() => setSelectedSessionId(session.id)}
                            className="w-full rounded-2xl border border-border/70 bg-panel/65 p-4 text-left transition-colors hover:bg-panel/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/70"
                          >
                            <div className="flex items-start justify-between gap-3">
                              <div className="min-w-0">
                                <div className="truncate font-medium text-foreground">{getSessionLabel(session)}</div>
                                <div className="mt-1 flex flex-wrap items-center gap-2 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                                  <span>{getSessionProjectLabel(session)}</span>
                                  <span aria-hidden="true">•</span>
                                  <span>{formatCompactInteger(session.message_count)} msgs</span>
                                </div>
                              </div>
                              <div className="font-mono text-sm text-foreground">{formatCompactCurrency(session.cost)}</div>
                            </div>

                            <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-muted-foreground">
                              <div className="rounded-lg bg-background/40 px-2.5 py-2">
                                <div className="uppercase tracking-[0.14em]">Created</div>
                                <div className="mt-1 font-mono text-sm text-foreground">{formatDateTime(session.time_created)}</div>
                              </div>
                              <div className="rounded-lg bg-background/40 px-2.5 py-2">
                                <div className="uppercase tracking-[0.14em]">Updated</div>
                                <div className="mt-1 font-mono text-sm text-foreground">{formatDateTime(session.time_updated)}</div>
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

                    <Card className="border-border/70 bg-panel/55">
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
                            Preserve title, timestamps, message count, and cost first. Everything else belongs in the drawer once you decide a session deserves attention.
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

      {selectedSessionId ? (
        <div className="fixed inset-0 z-40 bg-black/72 backdrop-blur-sm" onClick={() => setSelectedSessionId(null)}>
          <div className="flex min-h-full justify-end">
            <div
              role="dialog"
              aria-modal="true"
              aria-labelledby="session-detail-title"
              aria-describedby="session-detail-description"
              className="flex h-full w-full max-w-4xl flex-col border-l border-border/70 bg-background shadow-[0_24px_100px_-32px_rgba(0,0,0,0.95)]"
              onClick={(event) => event.stopPropagation()}
            >
              <div className="sticky top-0 z-10 border-b border-border/70 bg-background/95 px-5 py-4 backdrop-blur-xl sm:px-6">
                <div className="flex items-start justify-between gap-4">
                  <div className="space-y-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge tone="accent">Session detail</Badge>
                      {detail ? <Badge>{getSessionProjectLabel(detail)}</Badge> : null}
                    </div>
                    <div>
                      <h3 id="session-detail-title" className="text-lg font-semibold tracking-tight text-foreground sm:text-xl">
                        {detail ? getSessionLabel(detail) : 'Loading session detail'}
                      </h3>
                      <p id="session-detail-description" className="text-sm text-muted-foreground">
                        Live metadata from the detail endpoint. Transcript bodies are not available in the current API contract.
                      </p>
                    </div>
                  </div>

                  <Button variant="ghost" onClick={() => setSelectedSessionId(null)}>
                    Close
                  </Button>
                </div>
              </div>

              <div className="flex-1 overflow-y-auto px-5 py-5 sm:px-6">
                {detailLoading ? (
                  <div className="space-y-4">
                    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
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
                    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
                      <DetailMetric
                        label="Messages"
                        value={formatInteger(detail.message_count)}
                        hint={`${formatInteger(detailMessageMix.user)} user · ${formatInteger(detailMessageMix.assistant)} assistant`}
                      />
                      <DetailMetric
                        label="Assistant cost"
                        value={formatCurrency(detail.total_cost)}
                        hint={detail.messages.length > 0 ? `${formatCurrency(safeDivide(detail.total_cost, detail.messages.length))} per recorded message` : 'No message rows'}
                      />
                      <DetailMetric
                        label="Token load"
                        value={formatTokenCount(getTokenTotal(detail.total_tokens))}
                        hint={`${formatTokenCount(detail.total_tokens.input)} input · ${formatTokenCount(detail.total_tokens.output)} output`}
                      />
                      <DetailMetric
                        label="Session span"
                        value={formatSessionWindow(detail.time_created, detail.time_updated)}
                        hint={`Created ${formatDateTime(detail.time_created)}`}
                      />
                    </div>

                    <div className="grid gap-4 xl:grid-cols-[1.25fr_22rem]">
                      <div className="space-y-4">
                        <Card className="border-border/70 bg-panel/45">
                          <CardHeader>
                            <CardDescription>Timeline metadata</CardDescription>
                            <CardTitle>Message rows</CardTitle>
                          </CardHeader>
                          <CardContent className="space-y-3">
                            {detail.messages.length === 0 ? (
                              <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-5 text-sm text-muted-foreground">
                                This session exists, but the detail endpoint returned no message rows.
                              </div>
                            ) : (
                              detail.messages.map((message) => <SessionMessageRow key={message.id} message={message} />)
                            )}
                          </CardContent>
                        </Card>
                      </div>

                      <div className="space-y-4">
                        <Card className="border-border/70 bg-panel/45">
                          <CardHeader>
                            <CardDescription>Session facts</CardDescription>
                            <CardTitle>What the API actually gives you</CardTitle>
                          </CardHeader>
                          <CardContent className="space-y-3 text-sm text-muted-foreground">
                            <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                              <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Project</div>
                              <div className="mt-2 font-mono text-base text-foreground">{getSessionProjectLabel(detail)}</div>
                            </div>

                            <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                              <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Directory</div>
                              <div className={cn('mt-2 break-all font-mono text-sm text-foreground', !detail.directory && 'text-muted-foreground')}>
                                {detail.directory || 'No directory recorded'}
                              </div>
                            </div>

                            <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                              <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Last update</div>
                              <div className="mt-2 font-mono text-sm text-foreground">{formatDateTime(detail.time_updated)}</div>
                            </div>

                            <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                              <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Deliberate limitation</div>
                              <div className="mt-2 leading-6 text-muted-foreground">
                                There is no message text/content field in the current endpoint. You are looking at session telemetry, not a transcript viewer. If you want transcript rendering, the backend contract has to grow up first.
                              </div>
                            </div>
                          </CardContent>
                        </Card>
                      </div>
                    </div>
                  </div>
                ) : null}
              </div>
            </div>
          </div>
        </div>
      ) : null}
    </>
  )
}

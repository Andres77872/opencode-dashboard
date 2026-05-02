import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PeriodToggle } from '../components/daily/period-toggle'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { SessionsKpiGrid, type SessionsSummary } from '../components/sessions/sessions-kpi-grid'
import { SessionsTable } from '../components/sessions/sessions-table'
import { SessionDetailCard } from '../components/sessions/session-detail-card'
import { SessionCuesSidebar } from '../components/sessions/session-cues-sidebar'
import { SessionPagination } from '../components/sessions/session-pagination'
import { Input } from '../components/ui/input'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '../components/ui/sheet'
import { getSessionDetail, getSessionsWithFilter } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { formatDateTime, formatInteger } from '../lib/format'
import type { SessionDetail, SessionEntry, SessionList } from '../types/api'
import { isDailyPeriod, type DailyPeriod } from '../types/api'

const PAGE_SIZE = 12
const SEARCH_DEBOUNCE_MS = 300

function getSessionProjectLabel(session: Pick<SessionEntry, 'project_name' | 'project_id'>) {
  return session.project_name || session.project_id || 'No linked project'
}

function formatSessionWindow(createdAt: string, updatedAt: string) {
  const created = new Date(createdAt)
  const updated = new Date(updatedAt)
  const deltaMinutes = Math.max(0, Math.round((updated.getTime() - created.getTime()) / 60000))

  if (deltaMinutes < 1) return 'Under 1 minute'
  if (deltaMinutes < 60) return `${deltaMinutes}m span`

  const deltaHours = deltaMinutes / 60
  if (deltaHours < 24) return `${deltaHours.toFixed(deltaHours >= 10 ? 0 : 1)}h span`

  return `${(deltaHours / 24).toFixed(1)}d span`
}

export function SessionsView() {
  const { requestRefresh } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null)
  const [detail, setDetail] = useState<SessionDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string | null>(null)
  const [detailRequestNonce, setDetailRequestNonce] = useState(0)
  const triggerButtonRef = useRef<HTMLButtonElement | null>(null)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = isDailyPeriod(rawPeriod) ? rawPeriod : '7d'
  const rawFilter = searchParams.get('filter') ?? ''
  const rawProjectId = searchParams.get('project_id')
  const projectId = rawProjectId ? parseInt(rawProjectId, 10) : undefined

  const pageFromUrl = parseInt(searchParams.get('page') ?? '1', 10)
  const page = isNaN(pageFromUrl) || pageFromUrl < 1 ? 1 : pageFromUrl

  // Normalize URL params on mount
  useEffect(() => {
    const needs = !isDailyPeriod(rawPeriod) || !searchParams.has('page')
    if (needs) {
      setSearchParams((prev) => {
        const n = new URLSearchParams(prev)
        if (!isDailyPeriod(rawPeriod)) n.set('period', '7d')
        if (!n.has('page')) n.set('page', '1')
        return n
      })
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // URL page setter
  const setPage = (updater: number | ((prev: number) => number)) => {
    const np = typeof updater === 'function' ? updater(page) : updater
    setSearchParams((prev) => {
      const n = new URLSearchParams(prev)
      n.set('page', String(np))
      return n
    })
  }

  // ── Sessions list via usePeriodResource ──
  // Sessions has additional query dimensions (page, filter, projectId) beyond period.
  // We use cachePeriods: false and a stable fetcher that reads latest values via ref.
  // When page/filter/projectId changes, we call requestRefresh which increments
  // the dashboard refreshNonce, triggering the hook's effect to re-fetch.
  const sessionQueryRef = useRef({ page, filter: rawFilter, projectId })
  sessionQueryRef.current = { page, filter: rawFilter, projectId }

  const fetcher = useCallback((p: DailyPeriod, signal?: AbortSignal) => {
    const q = sessionQueryRef.current
    return getSessionsWithFilter(q.page, PAGE_SIZE, p, q.filter || undefined, q.projectId, signal)
  }, [])

  const { data, loading, error } = usePeriodResource<SessionList>(fetcher, period, { cachePeriods: false })

  // Trigger re-fetch when page/filter/projectId changes (the hook only watches period/refreshNonce)
  const sessionVersion = `${page}:${rawFilter}:${projectId ?? ''}`
  const lastVersionRef = useRef(sessionVersion)
  useEffect(() => {
    if (sessionVersion !== lastVersionRef.current) {
      lastVersionRef.current = sessionVersion
      requestRefresh()
    }
  }, [sessionVersion, requestRefresh])

  // Fetch session detail
  useEffect(() => {
    if (!selectedSessionId) {
      setDetail(null); setDetailError(null); setDetailLoading(false)
      return
    }
    const sid = selectedSessionId
    const ctrl = new AbortController()
    async function load() {
      setDetail(null); setDetailError(null); setDetailLoading(true)
      try {
        setDetail(await getSessionDetail(sid, ctrl.signal))
      } catch (caught) {
        if (ctrl.signal.aborted) return
        setDetailError(caught instanceof Error ? caught.message : 'Failed to load session detail')
      } finally {
        if (!ctrl.signal.aborted) setDetailLoading(false)
      }
    }
    void load()
    return () => ctrl.abort()
  }, [detailRequestNonce, selectedSessionId])

  // Summary
  const summary = useMemo((): SessionsSummary | null => {
    if (!data) return null
    const tp = Math.max(1, Math.ceil(data.total / data.page_size))
    const fv = data.total === 0 ? 0 : (data.page - 1) * data.page_size + 1
    const lv = data.total === 0 ? 0 : fv + data.sessions.length - 1
    const vc = data.sessions.reduce((a, s) => a + s.cost, 0)
    const vm = data.sessions.reduce((a, s) => a + s.message_count, 0)
    const vp = new Set(data.sessions.map((s) => getSessionProjectLabel(s))).size
    const h = [...data.sessions].sort((a, b) => b.cost - a.cost)[0] ?? null
    return {
      totalPages: tp, firstVisible: fv, lastVisible: lv,
      visibleCost: vc, visibleMessages: vm, visibleProjects: vp,
      hottestSession: h ? { label: h.title || 'Untitled session', cost: h.cost, message_count: h.message_count } : null,
      total: data.total, pageSize: data.page_size, page: data.page,
      empty: data.sessions.length === 0,
    }
  }, [data])

  // Handlers
  const handleRetry = () => requestRefresh()
  const handlePeriodChange = (np: DailyPeriod) => {
    if (np === period) return
    setSearchParams((prev) => { const n = new URLSearchParams(prev); n.set('period', np); n.set('page', '1'); return n })
  }
  const handleDetailRetry = () => { if (selectedSessionId) setDetailRequestNonce((c) => c + 1) }
  const handleSelectSession = (s: SessionEntry) => setSelectedSessionId(s.id)
  const handleTriggerClick = (s: SessionEntry, e: React.MouseEvent) => {
    e.stopPropagation()
    triggerButtonRef.current = e.currentTarget as HTMLButtonElement
    setSelectedSessionId(s.id)
  }

  // Search/filter with 300ms debounce
  const [searchText, setSearchText] = useState(rawFilter)
  const handleSearchChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const v = e.target.value
    setSearchText(v)
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    debounceTimerRef.current = setTimeout(() => {
      setSearchParams((prev) => {
        const n = new URLSearchParams(prev)
        n.set('page', '1')
        if (v) n.set('filter', v)
        else n.delete('filter')
        return n
      })
    }, SEARCH_DEBOUNCE_MS)
  }
  useEffect(() => () => { if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current) }, [])

  // ── Loading ──
  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Sessions</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Dense session browsing from <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions</code>
              {' '}+ <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions/:id</code>.
            </p>
          </div>
          <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
        </div>
        <DataPageSkeleton sections={['kpi-grid', 'table']} tableRows={8} />
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
              <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions?period={period}</code>
              {' '}+ <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/sessions/:id</code>
            </div>
            <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
          </div>
        </div>

        {error && (
          <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="font-medium text-foreground">Sessions failed to load</div>
              <div className="text-sm opacity-90">{error}</div>
            </div>
            <Button variant="ghost" onClick={handleRetry}>Retry</Button>
          </Alert>
        )}

        {summary && (
          <>
            {/* Search input */}
            <div className="flex flex-wrap items-center gap-3">
              <Input placeholder="Search sessions..." value={searchText} onChange={handleSearchChange} className="max-w-xs" />
              {rawFilter && <span className="text-xs text-muted-foreground">Filtering: <code className="rounded bg-muted px-1 py-0.5 font-mono">{rawFilter}</code></span>}
            </div>

            <SessionsKpiGrid summary={summary} />

            <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_18rem]">
              <div className="min-w-0 space-y-3">
                {summary.empty ? (
                  <Card>
                    <CardHeader>
                      <CardDescription>Empty state</CardDescription>
                      <CardTitle>No sessions recorded yet</CardTitle>
                    </CardHeader>
                    <CardContent className="text-sm text-muted-foreground">
                      <p>This route stays empty until the database contains session rows.</p>
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
                        {summary.hottestSession && <Badge tone="accent">Top spend · {summary.hottestSession.label}</Badge>}
                      </div>
                    </CardHeader>
                    <CardContent className="space-y-4">
                      <SessionsTable
                        sessions={data?.sessions ?? []}
                        summary={summary}
                        sortState={null}
                        onSortChange={() => {}}
                        onRowClick={handleSelectSession}
                        onTriggerClick={handleTriggerClick}
                      />
                      <SessionPagination
                        page={data?.page ?? 1}
                        total={data?.total ?? 0}
                        pageSize={data?.page_size ?? PAGE_SIZE}
                        totalPages={summary.totalPages}
                        firstVisible={summary.firstVisible}
                        lastVisible={summary.lastVisible}
                        onPageChange={setPage}
                      />
                    </CardContent>
                  </Card>
                )}
              </div>
              <SessionCuesSidebar summary={summary} />
            </div>
          </>
        )}

        {!summary && error && (
          <Card>
            <CardHeader>
              <CardDescription>Unavailable</CardDescription>
              <CardTitle>Session list could not be loaded</CardTitle>
            </CardHeader>
            <CardContent>
              <Button variant="ghost" onClick={handleRetry}>Retry sessions request</Button>
            </CardContent>
          </Card>
        )}
      </section>

      {/* Detail drawer */}
      <Sheet
        open={selectedSessionId !== null}
        onOpenChange={(o) => { if (!o) setSelectedSessionId(null) }}
      >
        <SheetContent
          side="right"
          className="flex h-full w-full max-w-[calc(100vw-0.75rem)] flex-col overflow-hidden border-l border-border/70 bg-background shadow-[0_24px_100px_-32px_rgba(0,0,0,0.95)] sm:max-w-[42rem] xl:max-w-[min(100vw-2rem,72rem)] 2xl:max-w-[78rem]"
          onCloseAutoFocus={(e) => { e.preventDefault(); triggerButtonRef.current?.focus() }}
        >
          <SheetHeader className="sticky top-0 z-10 border-b border-border/70 bg-background/95 px-4 py-4 pr-14 backdrop-blur-xl sm:px-6 sm:pr-16">
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone="accent">Telemetry inspector</Badge>
                {detail && <Badge>{getSessionProjectLabel(detail)}</Badge>}
                {detail && <span className="font-mono text-xs text-muted-foreground">id {detail.id.slice(0, 12)}</span>}
              </div>
              <div className="space-y-2">
                <SheetTitle className="sr-only">Session Detail</SheetTitle>
                <h3 className="text-lg font-semibold tracking-tight text-foreground sm:text-xl">
                  {detail ? (detail.title || 'Untitled session') : 'Loading session detail'}
                </h3>
                <SheetDescription className="sr-only">Session metadata drawer.</SheetDescription>
                <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                  {detail ? (
                    <>
                      <span className="font-mono text-foreground">{formatDateTime(detail.time_created)}</span>
                      <span aria-hidden="true">•</span>
                      <span>{formatSessionWindow(detail.time_created, detail.time_updated)}</span>
                      <span aria-hidden="true">•</span>
                      <span>{formatInteger(detail.message_count)} recorded messages</span>
                    </>
                  ) : <span>Fetching live session telemetry…</span>}
                </div>
                <div className="rounded-xl border border-border/70 bg-panel/40 px-3 py-2 text-sm text-muted-foreground">
                  Telemetry inspector only — transcript text is not available from this endpoint.
                </div>
              </div>
            </div>
          </SheetHeader>

          <div className="min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-4 py-5 sm:px-6">
            <SessionDetailCard detail={detail} loading={detailLoading} error={detailError} onRetry={handleDetailRetry} />
          </div>
        </SheetContent>
      </Sheet>
    </>
  )
}

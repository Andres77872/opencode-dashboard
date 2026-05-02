import { useEffect, useState } from 'react'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { Alert } from '../ui/alert'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '../ui/sheet'
import { Skeleton } from '../ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../ui/table'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { getProjectDetail } from '../../lib/api'
import {
  formatCompactCurrency,
  formatCompactInteger,
  formatCurrency,
  formatDateTime,
  formatInteger,
  formatTokenCount,
  safeDivide,
} from '../../lib/format'
import { getTokenTotal } from '../../lib/token-breakdown'
import type { DailyPeriod, ProjectDetail, SessionEntry } from '../../types/api'

// ── Props ──────────────────────────────────────────────────────

interface ProjectDrilldownDrawerProps {
  projectId: number | null
  period: DailyPeriod
  onClose: () => void
}

// ── Helpers ────────────────────────────────────────────────────

function getSessionLabel(session: SessionEntry) {
  return session.title || 'Untitled session'
}

function getSessionProjectLabel(session: SessionEntry) {
  return session.project_name || session.project_id || 'No linked project'
}

// ── Component ──────────────────────────────────────────────────

export function ProjectDrilldownDrawer({ projectId, period, onClose }: ProjectDrilldownDrawerProps) {
  const [detail, setDetail] = useState<ProjectDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sessionPage, setSessionPage] = useState(1)

  // Reset page when project changes
  useEffect(() => {
    setSessionPage(1)
    setDetail(null)
    setError(null)
  }, [projectId])

  // Fetch project detail
  useEffect(() => {
    if (projectId === null) return

    const ctrl = new AbortController()

    async function load() {
      setLoading(true)
      setError(null)

      try {
        const next = await getProjectDetail(projectId!, period, sessionPage, 10, ctrl.signal)
        setDetail(next)
      } catch (caught) {
        if (ctrl.signal.aborted) return
        setError(caught instanceof Error ? caught.message : 'Failed to load project detail')
      } finally {
        if (!ctrl.signal.aborted) setLoading(false)
      }
    }

    void load()
    return () => ctrl.abort()
  }, [projectId, period, sessionPage])

  const open = projectId !== null
  const totalPages = detail ? Math.max(1, Math.ceil(detail.total_sessions / 10)) : 0

  return (
    <Sheet open={open} onOpenChange={(o) => { if (!o) onClose() }}>
      <SheetContent
        side="right"
        className="flex h-full w-full max-w-[calc(100vw-0.75rem)] flex-col overflow-hidden border-l border-border/70 bg-background shadow-[0_24px_100px_-32px_rgba(0,0,0,0.95)] sm:max-w-[42rem] xl:max-w-[min(100vw-2rem,56rem)]"
      >
        <SheetHeader className="sticky top-0 z-10 border-b border-border/70 bg-background/95 px-4 py-4 pr-14 backdrop-blur-xl sm:px-6 sm:pr-16">
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone="accent">Project drilldown</Badge>
              {detail?.project_name && <Badge>{detail.project_name}</Badge>}
              {detail?.project_id && <span className="font-mono text-xs text-muted-foreground">id {detail.project_id.slice(0, 12)}</span>}
            </div>

            <SheetTitle className="sr-only">Project Detail</SheetTitle>
            <h3 className="text-lg font-semibold tracking-tight text-foreground sm:text-xl">
              {detail?.project_name || 'Loading project detail'}
            </h3>
            <SheetDescription className="sr-only">Project drilldown with aggregate stats and recent sessions.</SheetDescription>

            <div className="text-sm text-muted-foreground">
              Period: <span className="font-mono text-foreground">{period}</span>
            </div>
          </div>
        </SheetHeader>

        <div className="min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-4 py-5 sm:px-6">
          {loading && !detail ? (
            <div className="space-y-4">
              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                {Array.from({ length: 4 }).map((_, i) => (
                  <div key={i} className="rounded-2xl border border-border/70 bg-panel/45 px-4 py-4">
                    <Skeleton className="h-3 w-24" />
                    <Skeleton className="mt-3 h-8 w-28" />
                    <Skeleton className="mt-3 h-4 w-40" />
                  </div>
                ))}
              </div>
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-16 rounded-2xl" />
              ))}
            </div>
          ) : error ? (
            <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <div className="font-medium text-foreground">Project detail failed to load</div>
                <div className="text-sm opacity-90">{error}</div>
              </div>
              <Button variant="ghost" onClick={() => setSessionPage((p) => p)}>
                Retry
              </Button>
            </Alert>
          ) : detail ? (
            <div className="space-y-6">
              {/* Aggregate KPI cards */}
              <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-4">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Sessions</div>
                  <div className="mt-2 font-mono text-lg text-foreground sm:text-xl">{formatInteger(detail.sessions)}</div>
                  <div className="mt-2 text-sm leading-6 text-muted-foreground">Total in selected period</div>
                </div>
                <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-4">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Messages</div>
                  <div className="mt-2 font-mono text-lg text-foreground sm:text-xl">{formatInteger(detail.messages)}</div>
                  <div className="mt-2 text-sm leading-6 text-muted-foreground">Assistant messages in window</div>
                </div>
                <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-4">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Cost</div>
                  <div className="mt-2 font-mono text-lg text-foreground sm:text-xl">{formatCompactCurrency(detail.cost)}</div>
                  <div className="mt-2 text-sm leading-6 text-muted-foreground">{formatCurrency(safeDivide(detail.cost, Math.max(1, detail.sessions)))} avg per session</div>
                </div>
                <div className="rounded-2xl border border-border/70 bg-background/45 px-4 py-4">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Token load</div>
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="mt-2 cursor-default font-mono text-lg text-foreground transition-opacity hover:opacity-80 sm:text-xl">
                          {formatTokenCount(getTokenTotal(detail.tokens))}
                        </div>
                      </TooltipTrigger>
                      <TooltipContent side="top" className="font-mono">
                        <p>{formatInteger(getTokenTotal(detail.tokens))} tokens</p>
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                  <div className="mt-2 text-sm leading-6 text-muted-foreground">{formatInteger(detail.tokens.input)} input · {formatInteger(detail.tokens.output)} output</div>
                </div>
              </div>

              {/* Recent sessions */}
              <Card className="border-border/60 bg-background/25 shadow-none">
                <CardHeader>
                  <CardDescription>Session list</CardDescription>
                  <CardTitle>Recent sessions</CardTitle>
                </CardHeader>
                <CardContent className="space-y-3">
                  {detail.recent_sessions && detail.recent_sessions.length > 0 ? (
                    <>
                      <Table className="overflow-hidden rounded-xl border border-border/60">
                        <TableHeader className="bg-panel/75">
                          <TableRow className="border-b border-border/70 hover:bg-transparent">
                            <TableHead className="min-w-[12rem] px-3 py-2 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Session</TableHead>
                            <TableHead className="w-[8rem] px-3 py-2 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Activity</TableHead>
                            <TableHead className="w-[5rem] px-3 py-2 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Messages</TableHead>
                            <TableHead className="w-[6rem] px-3 py-2 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Cost</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody className="divide-y divide-border/40">
                          {detail.recent_sessions.map((session) => (
                            <TableRow key={session.id} className="bg-panel/30 hover:bg-panel/50">
                              <TableCell className="min-w-[12rem] px-3 py-2">
                                <div className="truncate font-medium text-foreground">{getSessionLabel(session)}</div>
                                <div className="text-xs text-muted-foreground">{getSessionProjectLabel(session)}</div>
                              </TableCell>
                              <TableCell className="px-3 py-2 text-xs text-muted-foreground">
                                <div>{formatDateTime(session.time_created)}</div>
                              </TableCell>
                              <TableCell className="px-3 py-2 font-mono text-sm text-foreground">{formatCompactInteger(session.message_count)}</TableCell>
                              <TableCell className="px-3 py-2 font-mono text-sm text-foreground">{formatCompactCurrency(session.cost)}</TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>

                      {/* Pagination for recent sessions */}
                      {totalPages > 1 && (
                        <div className="flex items-center justify-between gap-3 rounded-xl border border-border/60 bg-panel/40 px-3 py-2.5 text-sm">
                          <span className="text-muted-foreground">
                            Page {sessionPage} of {totalPages} · {detail.total_sessions} total sessions
                          </span>
                          <div className="flex items-center gap-2">
                            <Button
                              variant="ghost"
                              size="sm"
                              disabled={sessionPage <= 1}
                              onClick={() => setSessionPage((p) => Math.max(1, p - 1))}
                            >
                              Previous
                            </Button>
                            <Button
                              variant="ghost"
                              size="sm"
                              disabled={sessionPage >= totalPages}
                              onClick={() => setSessionPage((p) => p + 1)}
                            >
                              Next
                            </Button>
                          </div>
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="rounded-xl border border-border/60 bg-background/35 px-3 py-4 text-sm text-muted-foreground">
                      No session activity for this project in the selected period.
                    </div>
                  )}
                </CardContent>
              </Card>
            </div>
          ) : null}
        </div>
      </SheetContent>
    </Sheet>
  )
}

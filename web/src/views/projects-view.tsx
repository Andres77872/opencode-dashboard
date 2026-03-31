import { useEffect, useMemo, useRef, useState } from 'react'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { ProjectsSkeleton } from '../components/projects/projects-skeleton'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { getProjects } from '../lib/api'
import {
  formatCompactCurrency,
  formatCompactInteger,
  formatCurrency,
  formatInteger,
  formatPercentage,
  formatTokenCount,
  safeDivide,
} from '../lib/format'
import { cn } from '../lib/utils'
import type { ProjectEntry, ProjectStats } from '../types/api'

type SortKey = 'cost' | 'messages' | 'sessions' | 'project' | 'tokens'

interface EnrichedProjectRow extends ProjectEntry {
  totalTokens: number
  costShare: number
  avgCostPerSession: number
}

function getProjectLabel(project: ProjectEntry) {
  return project.project_name || 'Unnamed project'
}

function getProjectIdentifier(project: ProjectEntry) {
  if (!project.project_id) {
    return 'unknown-project'
  }

  return project.project_id.length > 12 ? project.project_id.slice(0, 12) : project.project_id
}

function getTotalTokens(project: ProjectEntry) {
  return project.tokens.input + project.tokens.output + project.tokens.reasoning + project.tokens.cache.read + project.tokens.cache.write
}

function compareRows(sortKey: SortKey, left: EnrichedProjectRow, right: EnrichedProjectRow) {
  switch (sortKey) {
    case 'project':
      return getProjectLabel(left).localeCompare(getProjectLabel(right))
    case 'sessions':
      return right.sessions - left.sessions
    case 'messages':
      return right.messages - left.messages
    case 'tokens':
      return right.totalTokens - left.totalTokens
    case 'cost':
    default:
      return right.cost - left.cost
  }
}

function SortButton({
  active,
  label,
  onClick,
}: {
  active: boolean
  label: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-2 rounded-md px-1 py-1 text-left text-[11px] font-medium uppercase tracking-[0.16em] transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/70',
        active ? 'text-foreground' : 'text-muted-foreground hover:text-foreground',
      )}
    >
      <span>{label}</span>
      <span aria-hidden="true" className={cn('text-[10px]', active ? 'text-accent' : 'text-muted-foreground/70')}>
        ↓
      </span>
    </button>
  )
}

export function ProjectsView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [data, setData] = useState<ProjectStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [sortKey, setSortKey] = useState<SortKey>('cost')
  const hasLoadedOnceRef = useRef(false)

  useEffect(() => {
    const controller = new AbortController()

    async function loadProjects() {
      setRefreshing(true)
      setError(null)

      if (!hasLoadedOnceRef.current) {
        setLoading(true)
      }

      try {
        const next = await getProjects(controller.signal)
        setData(next)
        hasLoadedOnceRef.current = true
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted) {
          return
        }

        setError(caught instanceof Error ? caught.message : 'Failed to load project stats')
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void loadProjects()

    return () => controller.abort()
  }, [refreshNonce, setLastUpdatedAt, setRefreshing])

  const summary = useMemo(() => {
    if (!data) {
      return null
    }

    const totalCost = data.projects.reduce((accumulator, project) => accumulator + project.cost, 0)
    const totalSessions = data.projects.reduce((accumulator, project) => accumulator + project.sessions, 0)
    const totalMessages = data.projects.reduce((accumulator, project) => accumulator + project.messages, 0)

    const rows = data.projects.map<EnrichedProjectRow>((project) => ({
      ...project,
      totalTokens: getTotalTokens(project),
      costShare: safeDivide(project.cost, totalCost) * 100,
      avgCostPerSession: safeDivide(project.cost, project.sessions),
    }))

    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(sortKey, left, right)
      if (primary !== 0) {
        return primary
      }

      if (right.cost !== left.cost) {
        return right.cost - left.cost
      }

      return getProjectLabel(left).localeCompare(getProjectLabel(right))
    })

    const costLeader = [...rows].sort((left, right) => right.cost - left.cost)[0] ?? null
    const activityLeader = [...rows].sort((left, right) => right.messages - left.messages)[0] ?? null
    const efficiencyLeader = [...rows]
      .filter((row) => row.sessions > 0)
      .sort((left, right) => left.avgCostPerSession - right.avgCostPerSession)[0] ?? null

    return {
      rows: sortedRows,
      totalCost,
      totalSessions,
      totalMessages,
      totalTokens: rows.reduce((accumulator, row) => accumulator + row.totalTokens, 0),
      empty: rows.length === 0,
      costLeader,
      activityLeader,
      efficiencyLeader,
    }
  }, [data, sortKey])

  const handleRetry = () => {
    requestRefresh()
  }

  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Projects</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Project attention ranking from <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/projects</code>, tuned for dense scanning and honest spend visibility.
            </p>
          </div>
        </div>
        <ProjectsSkeleton />
      </section>
    )
  }

  return (
    <section className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Projects</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            A table-first project view for spotting which repos are soaking up cost, sessions, and message volume without pretending the backend exposes more than it does.
          </p>
        </div>

        <div className="text-sm text-muted-foreground">
          Endpoint: <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/projects</code>
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Projects failed to load</div>
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
              label="Tracked projects"
              value={formatInteger(summary.rows.length)}
              hint={summary.rows.length === 1 ? 'One project is visible in the current database' : 'Distinct projects aggregated from the OpenCode project table'}
            />
            <MetricCard
              label="Total project cost"
              value={formatCurrency(summary.totalCost)}
              hint={`${formatCompactInteger(summary.totalMessages)} assistant messages attributed across projects`}
            />
            <MetricCard
              label="Sessions touched"
              value={formatInteger(summary.totalSessions)}
              hint={summary.activityLeader ? `${getProjectLabel(summary.activityLeader)} leads message volume` : 'Awaiting project activity'}
            />
            <MetricCard
              label="Token load"
              value={formatTokenCount(summary.totalTokens)}
              hint={summary.costLeader ? `${getProjectLabel(summary.costLeader)} owns ${formatPercentage(summary.costLeader.costShare)} of spend` : 'No spend recorded yet'}
            />
          </div>

          {summary.empty ? (
            <Card>
              <CardHeader>
                <CardDescription>Empty state</CardDescription>
                <CardTitle>No project activity recorded yet</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm text-muted-foreground">
                <p>
                  The backend can return known projects before they accumulate assistant-message usage, so this route stays empty until sessions and assistant cost/token data exist.
                </p>
                <p>
                  Once activity lands, this slice ranks projects by cost, shows message and token load, and makes spend concentration obvious with share bars.
                </p>
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardHeader className="gap-3 lg:flex-row lg:items-end lg:justify-between">
                <div className="space-y-1.5">
                  <CardDescription>Primary artifact</CardDescription>
                  <CardTitle>Project usage ranking</CardTitle>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <Badge tone="success">Dense table</Badge>
                  {summary.costLeader ? <Badge tone="accent">Top spend · {getProjectLabel(summary.costLeader)}</Badge> : null}
                  {summary.activityLeader ? <Badge>Top traffic · {getProjectLabel(summary.activityLeader)}</Badge> : null}
                </div>
              </CardHeader>

              <CardContent className="space-y-4">
                <div className="grid gap-4 xl:grid-cols-[1.35fr_20rem]">
                  <div className="hidden overflow-hidden rounded-2xl border border-border/70 lg:block">
                    <div className="grid grid-cols-[minmax(16rem,1.7fr)_8rem_6rem_7rem_7rem_9rem] gap-3 border-b border-border/70 bg-panel/75 px-4 py-3">
                      <SortButton active={sortKey === 'project'} label="Project" onClick={() => setSortKey('project')} />
                      <SortButton active={sortKey === 'sessions'} label="Sessions" onClick={() => setSortKey('sessions')} />
                      <SortButton active={sortKey === 'messages'} label="Messages" onClick={() => setSortKey('messages')} />
                      <SortButton active={sortKey === 'tokens'} label="Tokens" onClick={() => setSortKey('tokens')} />
                      <SortButton active={sortKey === 'cost'} label="Cost" onClick={() => setSortKey('cost')} />
                      <div className="px-1 py-1 text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">Share</div>
                    </div>

                    <div className="divide-y divide-border/60">
                      {summary.rows.map((row) => (
                        <div
                          key={row.project_id}
                          className="grid grid-cols-[minmax(16rem,1.7fr)_8rem_6rem_7rem_7rem_9rem] gap-3 bg-card/40 px-4 py-3 transition-colors hover:bg-white/4"
                        >
                          <div className="min-w-0 space-y-2">
                            <div className="truncate font-medium text-foreground">{getProjectLabel(row)}</div>
                            <div className="flex items-center gap-2 text-xs text-muted-foreground">
                              <Badge className="px-2 py-0.5 text-[10px] tracking-[0.16em]">id {getProjectIdentifier(row)}</Badge>
                              <span>{formatCurrency(row.avgCostPerSession)} / session</span>
                            </div>
                          </div>
                          <div className="font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</div>
                          <div className="font-mono text-sm text-foreground">{formatCompactInteger(row.messages)}</div>
                          <div className="font-mono text-sm text-foreground">{formatTokenCount(row.totalTokens)}</div>
                          <div className="font-mono text-sm text-foreground">{formatCompactCurrency(row.cost)}</div>
                          <div className="space-y-2">
                            <div className="h-2 overflow-hidden rounded-full bg-background/80">
                              <div
                                className="h-full rounded-full bg-linear-to-r from-accent/60 to-accent"
                                style={{ width: `${Math.max(row.costShare, row.cost > 0 ? 4 : 0)}%` }}
                              />
                            </div>
                            <div className="font-mono text-xs text-muted-foreground">{formatPercentage(row.costShare)}</div>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>

                  <Card className="border-border/70 bg-panel/55">
                    <CardHeader>
                      <CardDescription>Project cues</CardDescription>
                      <CardTitle>Read the table faster</CardTitle>
                    </CardHeader>
                    <CardContent className="space-y-3 text-sm text-muted-foreground">
                      <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                        <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Top spend</div>
                        <div className="mt-2 font-mono text-base text-foreground">
                          {summary.costLeader ? getProjectLabel(summary.costLeader) : 'No data'}
                        </div>
                        <div className="mt-1 text-sm text-muted-foreground">
                          {summary.costLeader
                            ? `${formatCurrency(summary.costLeader.cost)} across ${formatCompactInteger(summary.costLeader.sessions)} sessions`
                            : 'Awaiting activity'}
                        </div>
                      </div>

                      <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                        <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Most active project</div>
                        <div className="mt-2 font-mono text-base text-foreground">
                          {summary.activityLeader ? getProjectLabel(summary.activityLeader) : 'No data'}
                        </div>
                        <div className="mt-1 text-sm text-muted-foreground">
                          {summary.activityLeader
                            ? `${formatCompactInteger(summary.activityLeader.messages)} messages · ${formatTokenCount(summary.activityLeader.totalTokens)} tokens`
                            : 'No usage recorded yet'}
                        </div>
                      </div>

                      <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                        <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Backend caveat</div>
                        <div className="mt-2 text-sm leading-6 text-muted-foreground">
                          The current projects endpoint exposes <span className="font-semibold text-foreground">name, id, sessions, messages, tokens, and cost</span>. It does not expose worktree labels or latest activity timestamps yet, so this slice stays honest instead of inventing fake columns.
                        </div>
                      </div>

                      <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                        <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Lowest spend / session</div>
                        <div className="mt-2 font-mono text-base text-foreground">
                          {summary.efficiencyLeader ? getProjectLabel(summary.efficiencyLeader) : 'Insufficient data'}
                        </div>
                        <div className="mt-1 text-sm text-muted-foreground">
                          {summary.efficiencyLeader
                            ? `${formatCurrency(summary.efficiencyLeader.avgCostPerSession)} per session in the current aggregate`
                            : 'Need at least one project with sessions to compute this'}
                        </div>
                      </div>
                    </CardContent>
                  </Card>
                </div>

                <div className="space-y-3 lg:hidden">
                  {summary.rows.map((row) => (
                    <div key={row.project_id} className="rounded-2xl border border-border/70 bg-panel/65 p-4">
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="truncate font-medium text-foreground">{getProjectLabel(row)}</div>
                          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                            <span>id {getProjectIdentifier(row)}</span>
                            <span aria-hidden="true">•</span>
                            <span>{formatPercentage(row.costShare)} share</span>
                          </div>
                        </div>
                        <div className="font-mono text-sm text-foreground">{formatCompactCurrency(row.cost)}</div>
                      </div>

                      <div className="mt-3 h-2 overflow-hidden rounded-full bg-background/80">
                        <div
                          className="h-full rounded-full bg-linear-to-r from-accent/60 to-accent"
                          style={{ width: `${Math.max(row.costShare, row.cost > 0 ? 4 : 0)}%` }}
                        />
                      </div>

                      <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-muted-foreground">
                        <div className="rounded-lg bg-background/40 px-2.5 py-2">
                          <div className="uppercase tracking-[0.14em]">Sessions</div>
                          <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</div>
                        </div>
                        <div className="rounded-lg bg-background/40 px-2.5 py-2">
                          <div className="uppercase tracking-[0.14em]">Messages</div>
                          <div className="mt-1 font-mono text-sm text-foreground">{formatCompactInteger(row.messages)}</div>
                        </div>
                        <div className="rounded-lg bg-background/40 px-2.5 py-2">
                          <div className="uppercase tracking-[0.14em]">Tokens</div>
                          <div className="mt-1 font-mono text-sm text-foreground">{formatTokenCount(row.totalTokens)}</div>
                        </div>
                        <div className="rounded-lg bg-background/40 px-2.5 py-2">
                          <div className="uppercase tracking-[0.14em]">$/session</div>
                          <div className="mt-1 font-mono text-sm text-foreground">{formatCurrency(row.avgCostPerSession)}</div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}
        </>
      ) : null}
    </section>
  )
}

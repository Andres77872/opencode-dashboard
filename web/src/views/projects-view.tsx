import { useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PeriodToggle } from '../components/daily/period-toggle'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { MetricCard } from '../components/overview/metric-card'
import { DataPageSkeleton } from '../components/common/data-page-skeleton'
import { DataTable, type DataTableColumn } from '../components/common/data-table'
import { ProjectDrilldownDrawer } from '../components/projects/project-drilldown-drawer'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../components/ui/card'
import { Progress } from '../components/ui/progress'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../components/ui/tooltip'
import { getProjects } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import { getTokenTotal } from '../lib/token-breakdown'
import {
  formatCompactCurrency,
  formatCompactInteger,
  formatCurrency,
  formatInteger,
  formatPercentage,
  formatTokenCount,
  safeDivide,
} from '../lib/format'
import { type SortDirection, type SortState } from '../lib/table-sort'
import type { ProjectEntry } from '../types/api'
import { isDailyPeriod, type DailyPeriod } from '../types/api'

type SortKey = 'cost' | 'messages' | 'sessions' | 'project' | 'tokens'

const DEFAULT_SORT_DIRECTIONS: Record<SortKey, SortDirection> = {
  cost: 'desc',
  messages: 'desc',
  project: 'asc',
  sessions: 'desc',
  tokens: 'desc',
}

const DEFAULT_TABLE_SORT: SortState<SortKey> = {
  key: 'cost',
  direction: 'desc',
}

interface EnrichedProjectRow extends ProjectEntry {
  totalTokens: number
  costShare: number
  avgCostPerSession: number
}

function getProjectLabel(project: ProjectEntry) {
  return project.project_name || 'Unnamed project'
}

function getProjectIdentifier(project: ProjectEntry) {
  if (!project.project_id) return 'unknown-project'
  return project.project_id.length > 12 ? project.project_id.slice(0, 12) : project.project_id
}

function compareRows(sortKey: SortKey, left: EnrichedProjectRow, right: EnrichedProjectRow) {
  switch (sortKey) {
    case 'project': return getProjectLabel(left).localeCompare(getProjectLabel(right))
    case 'sessions': return right.sessions - left.sessions
    case 'messages': return right.messages - left.messages
    case 'tokens': return right.totalTokens - left.totalTokens
    case 'cost': default: return right.cost - left.cost
  }
}

export function ProjectsView() {
  const { requestRefresh } = useDashboardContext()
  const [searchParams, setSearchParams] = useSearchParams()
  const [sortState, setSortState] = useState<SortState<string> | null>(null)
  const [selectedProjectId, setSelectedProjectId] = useState<number | null>(null)

  const rawPeriod = searchParams.get('period')
  const period: DailyPeriod = isDailyPeriod(rawPeriod) ? rawPeriod : '7d'
  const { data, loading, error } = usePeriodResource(getProjects, period)

  const handleSortChange = (key: string) => {
    setSortState((current) => {
      if (!current || current.key !== key) {
        return { key: key as SortKey, direction: DEFAULT_SORT_DIRECTIONS[key as SortKey] }
      }
      const dir = current.direction === 'asc' ? 'desc' : 'asc'
      return { key: key as SortKey, direction: dir }
    })
  }

  const handlePeriodChange = (nextPeriod: DailyPeriod) => {
    if (nextPeriod === period) return
    setSortState(null)
    setSearchParams((prev) => { const n = new URLSearchParams(prev); n.set('period', nextPeriod); return n })
  }

  const summary = useMemo(() => {
    if (!data) return null

    const totalCost = data.projects.reduce((a, p) => a + p.cost, 0)
    const totalSessions = data.projects.reduce((a, p) => a + p.sessions, 0)
    const totalMessages = data.projects.reduce((a, p) => a + p.messages, 0)

    const rows = data.projects.map<EnrichedProjectRow>((project) => ({
      ...project,
      totalTokens: getTokenTotal(project.tokens),
      costShare: safeDivide(project.cost, totalCost) * 100,
      avgCostPerSession: safeDivide(project.cost, project.sessions),
    }))

    const effectiveSort = (sortState ?? DEFAULT_TABLE_SORT) as SortState<SortKey>

    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(effectiveSort.key, left, right)
      const m = effectiveSort.direction === DEFAULT_SORT_DIRECTIONS[effectiveSort.key] ? 1 : -1
      const d = primary * m
      if (d !== 0) return d
      if (right.cost !== left.cost) return right.cost - left.cost
      return getProjectLabel(left).localeCompare(getProjectLabel(right))
    })

    const costLeader = [...rows].sort((a, b) => b.cost - a.cost)[0] ?? null
    const activityLeader = [...rows].sort((a, b) => b.messages - a.messages)[0] ?? null
    const efficiencyLeader = [...rows].filter((r) => r.sessions > 0).sort((a, b) => a.avgCostPerSession - b.avgCostPerSession)[0] ?? null

    return {
      rows: sortedRows, totalCost, totalSessions, totalMessages,
      totalTokens: rows.reduce((a, r) => a + r.totalTokens, 0),
      empty: rows.length === 0, costLeader, activityLeader, efficiencyLeader,
    }
  }, [data, sortState])

  const handleRetry = () => requestRefresh()

  // DataTable columns
  const columns: DataTableColumn<EnrichedProjectRow>[] = [
    {
      key: 'project',
      label: 'Project',
      width: 'min-w-[16rem]',
      sortable: true,
      render: (row) => (
        <div className="space-y-2">
          <div className="truncate font-medium text-foreground">{getProjectLabel(row)}</div>
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Badge className="px-2 py-0.5 text-[10px] tracking-[0.16em]">id {getProjectIdentifier(row)}</Badge>
            <span>{formatCurrency(row.avgCostPerSession)} / session</span>
          </div>
        </div>
      ),
    },
    {
      key: 'sessions',
      label: 'Sessions',
      width: 'w-[8rem]',
      sortable: true,
      render: (row) => <span className="font-mono text-sm text-foreground">{formatCompactInteger(row.sessions)}</span>,
    },
    {
      key: 'messages',
      label: 'Messages',
      width: 'w-[6rem]',
      sortable: true,
      render: (row) => <span className="font-mono text-sm text-foreground">{formatCompactInteger(row.messages)}</span>,
    },
    {
      key: 'tokens',
      label: 'Tokens',
      width: 'w-[7rem]',
      sortable: true,
      render: (row) => (
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <span className="cursor-default font-mono text-sm text-foreground transition-opacity hover:opacity-80">
                {formatTokenCount(row.totalTokens)}
              </span>
            </TooltipTrigger>
            <TooltipContent side="top" className="font-mono">
              <p>{formatInteger(row.totalTokens)}</p>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      ),
    },
    {
      key: 'cost',
      label: 'Cost',
      width: 'w-[7rem]',
      sortable: true,
      render: (row) => <span className="font-mono text-sm text-foreground">{formatCompactCurrency(row.cost)}</span>,
    },
    {
      key: 'share',
      label: 'Share',
      width: 'w-[9rem]',
      sortable: false,
      render: (row) => (
        <div className="space-y-2">
          <Progress value={Math.max(row.costShare, row.cost > 0 ? 4 : 0)} />
          <div className="font-mono text-xs text-muted-foreground">{formatPercentage(row.costShare)}</div>
        </div>
      ),
    },
  ]

  // Loading state
  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Projects</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Project attention ranking from <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/projects</code>.
            </p>
          </div>
        </div>
        <DataPageSkeleton sections={['kpi-grid', 'table']} tableRows={6} />
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
            A table-first project view for spotting which repos are soaking up cost, sessions, and message volume.
          </p>
        </div>

        <div className="flex flex-col items-start gap-3 sm:flex-row sm:items-center">
          <div className="text-sm text-muted-foreground">
            Endpoint: <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/projects?period={period}</code>
          </div>
          <PeriodToggle value={period} onChange={handlePeriodChange} disabled={loading} />
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Projects failed to load</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>Retry</Button>
        </Alert>
      ) : null}

      {summary ? (
        <>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <MetricCard label="Tracked projects" value={formatInteger(summary.rows.length)}
              hint={summary.rows.length === 1 ? 'One project is visible' : 'Distinct projects aggregated from the OpenCode project table'} />
            <MetricCard label="Total project cost" value={formatCurrency(summary.totalCost)}
              hint={`${formatCompactInteger(summary.totalMessages)} assistant messages attributed`} />
            <MetricCard label="Sessions touched" value={formatInteger(summary.totalSessions)}
              hint={summary.activityLeader ? `${getProjectLabel(summary.activityLeader)} leads message volume` : 'Awaiting activity'} />
            <MetricCard label="Token load" value={formatTokenCount(summary.totalTokens)}
              hint={summary.costLeader ? `${getProjectLabel(summary.costLeader)} owns ${formatPercentage(summary.costLeader.costShare)} of spend` : 'No spend recorded yet'} />
          </div>

          <div className="grid gap-4 2xl:grid-cols-[minmax(0,1fr)_18rem] 2xl:items-start">
            <div className="min-w-0 space-y-4">
              {summary.empty ? (
                <Card>
                  <CardHeader>
                    <CardDescription>Empty state</CardDescription>
                    <CardTitle>No project activity recorded yet</CardTitle>
                  </CardHeader>
                  <CardContent className="text-sm text-muted-foreground">
                    <p>Once activity lands, this slice ranks projects by cost and shows message and token load.</p>
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
                      {summary.costLeader && <Badge tone="accent">Top spend · {getProjectLabel(summary.costLeader)}</Badge>}
                      {summary.activityLeader && <Badge>Top traffic · {getProjectLabel(summary.activityLeader)}</Badge>}
                    </div>
                  </CardHeader>

                  <CardContent className="space-y-4">
                    <DataTable<EnrichedProjectRow>
                      rows={summary.rows}
                      columns={columns}
                      sortState={sortState}
                      onSortChange={handleSortChange}
                      rowKey={(row) => row.name}
                      mobileCard={(row) => (
                        <div className="rounded-2xl border border-border/70 bg-panel/65 p-4">
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0">
                              <div className="truncate font-medium text-foreground">{getProjectLabel(row)}</div>
                              <div className="mt-1 flex flex-wrap items-center gap-2 text-xs uppercase tracking-[0.14em] text-muted-foreground">
                                <span>{formatCurrency(row.cost)}</span>
                                <span aria-hidden="true">•</span>
                                <span>{formatCompactInteger(row.sessions)} sessions</span>
                                <span aria-hidden="true">•</span>
                                <span>{formatCompactInteger(row.messages)} msgs</span>
                              </div>
                            </div>
                            <Badge>{formatPercentage(row.costShare)}</Badge>
                          </div>
                          <Progress className="mt-3" value={Math.max(row.costShare, row.sessions > 0 ? 4 : 0)} />
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
                              <TooltipProvider>
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <div className="mt-1 font-mono text-sm text-foreground">{formatTokenCount(row.totalTokens)}</div>
                                  </TooltipTrigger>
                                  <TooltipContent side="top" className="font-mono">
                                    <p>{formatInteger(row.totalTokens)}</p>
                                  </TooltipContent>
                                </Tooltip>
                              </TooltipProvider>
                            </div>
                            <div className="rounded-lg bg-background/40 px-2.5 py-2">
                              <div className="uppercase tracking-[0.14em]">$/session</div>
                              <div className="mt-1 font-mono text-sm text-foreground">{formatCurrency(row.avgCostPerSession)}</div>
                            </div>
                          </div>
                        </div>
                      )}
                      emptyState={
                        <div className="text-sm text-muted-foreground">
                          No project activity recorded yet.
                        </div>
                      }
                    />
                  </CardContent>
                </Card>
              )}
            </div>

            {/* Sidebar cues */}
            <Card className="hidden border-border/70 bg-panel/55 2xl:block 2xl:sticky self-start" style={{ top: 'var(--header-height)' }}>
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
                  <div className="mt-1 text-sm">
                    {summary.costLeader ? `${formatCurrency(summary.costLeader.cost)} across ${formatCompactInteger(summary.costLeader.sessions)} sessions` : 'Awaiting activity'}
                  </div>
                </div>
                <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Most active project</div>
                  <div className="mt-2 font-mono text-base text-foreground">
                    {summary.activityLeader ? getProjectLabel(summary.activityLeader) : 'No data'}
                  </div>
                  <div className="mt-1 text-sm">
                    {summary.activityLeader
                      ? `${formatCompactInteger(summary.activityLeader.messages)} messages · ${formatTokenCount(summary.activityLeader.totalTokens)} tokens`
                      : 'No usage recorded yet'}
                  </div>
                </div>
                <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                  <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Lowest spend / session</div>
                  <div className="mt-2 font-mono text-base text-foreground">
                    {summary.efficiencyLeader ? getProjectLabel(summary.efficiencyLeader) : 'Insufficient data'}
                  </div>
                  <div className="mt-1 text-sm">
                    {summary.efficiencyLeader
                      ? `${formatCurrency(summary.efficiencyLeader.avgCostPerSession)} per session in the current aggregate`
                      : 'Need at least one project with sessions'}
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
          ) : (
            <Card>
              <CardHeader className="gap-3 lg:flex-row lg:items-end lg:justify-between">
                <div className="space-y-1.5">
                  <CardDescription>Primary artifact</CardDescription>
                  <CardTitle>Project usage ranking</CardTitle>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <Badge tone="success">Dense table</Badge>
                  {summary.costLeader && <Badge tone="accent">Top spend · {getProjectLabel(summary.costLeader)}</Badge>}
                  {summary.activityLeader && <Badge>Top traffic · {getProjectLabel(summary.activityLeader)}</Badge>}
                </div>
              </CardHeader>

              <CardContent className="space-y-4">
                <DataTable<EnrichedProjectRow>
                  rows={summary.rows}
                  columns={columns}
                  sortState={sortState}
                  onSortChange={handleSortChange}
                  rowKey={(row) => row.project_id}
                  onRowClick={(row) => setSelectedProjectId(Number(row.project_id) || null)}
                  mobileCard={(row) => (
                    <div className="rounded-2xl border border-border/70 bg-panel/65 p-4">
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
                      <Progress className="mt-3" value={Math.max(row.costShare, row.cost > 0 ? 4 : 0)} />
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
                          <TooltipProvider>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <div className="mt-1 cursor-default font-mono text-sm text-foreground transition-opacity hover:opacity-80">
                                  {formatTokenCount(row.totalTokens)}
                                </div>
                              </TooltipTrigger>
                              <TooltipContent side="top" className="font-mono">
                                <p>{formatInteger(row.totalTokens)}</p>
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                        </div>
                        <div className="rounded-lg bg-background/40 px-2.5 py-2">
                          <div className="uppercase tracking-[0.14em]">$/session</div>
                          <div className="mt-1 font-mono text-sm text-foreground">{formatCurrency(row.avgCostPerSession)}</div>
                        </div>
                      </div>
                    </div>
                  )}
                  emptyState={
                    <div className="text-sm text-muted-foreground">
                      No project activity recorded yet.
                    </div>
                  }
                />
              </CardContent>
            </Card>
          )}

          {/* Sidebar cues */}
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
                <div className="mt-1 text-sm">
                  {summary.costLeader ? `${formatCurrency(summary.costLeader.cost)} across ${formatCompactInteger(summary.costLeader.sessions)} sessions` : 'Awaiting activity'}
                </div>
              </div>
              <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Most active project</div>
                <div className="mt-2 font-mono text-base text-foreground">
                  {summary.activityLeader ? getProjectLabel(summary.activityLeader) : 'No data'}
                </div>
                <div className="mt-1 text-sm">
                  {summary.activityLeader
                    ? `${formatCompactInteger(summary.activityLeader.messages)} messages · ${formatTokenCount(summary.activityLeader.totalTokens)} tokens`
                    : 'No usage recorded yet'}
                </div>
              </div>
              <div className="rounded-xl border border-border/70 bg-background/40 px-3 py-3">
                <div className="text-[11px] uppercase tracking-[0.16em] text-muted-foreground">Lowest spend / session</div>
                <div className="mt-2 font-mono text-base text-foreground">
                  {summary.efficiencyLeader ? getProjectLabel(summary.efficiencyLeader) : 'Insufficient data'}
                </div>
                <div className="mt-1 text-sm">
                  {summary.efficiencyLeader
                    ? `${formatCurrency(summary.efficiencyLeader.avgCostPerSession)} per session in the current aggregate`
                    : 'Need at least one project with sessions'}
                </div>
              </div>
            </CardContent>
          </Card>
        </>
      ) : null}

      {/* Project drilldown drawer */}
      <ProjectDrilldownDrawer
        projectId={selectedProjectId}
        period={period}
        onClose={() => setSelectedProjectId(null)}
      />
    </section>
  )
}

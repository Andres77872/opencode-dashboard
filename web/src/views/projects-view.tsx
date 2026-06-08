/* Projects — per-source project ranking (Vael). Costs are reported per source and
   never combined across sources. No fabricated data: the API has no per-project
   14d trend, branch name, or period-over-period delta, so those Vael mock columns
   are omitted. Token share is a real derived ratio (project tokens / total). */
import { useEffect, useMemo, useState } from 'react'
import {
  Card,
  StatCard,
  DataTable,
  VendorChip,
  Badge,
  Icon,
  Skeleton,
  ErrorState,
  Notice,
  Button,
  vendorMeta,
  type Column,
  type SortSpec,
} from '../components/vael'
import { Drawer } from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { usePeriodControls } from '../lib/use-period-controls'
import { usePeriodResource } from '../lib/use-period-resource'
import { getProjects, getProjectDetail } from '../lib/api'
import { getTokenTotal } from '../lib/token-breakdown'
import { getNextSortState, type SortDirection, type SortState } from '../lib/table-sort'
import {
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCurrencyWithProvenance,
  formatDateTime,
  formatInteger,
  formatTokenCount,
  safeDivide,
} from '../lib/format'
import type { ProjectDetail, ProjectEntry, SessionEntry, SourceID } from '../types/api'

type SortKey = 'project' | 'sessions' | 'messages' | 'tokens' | 'cost'

const DEFAULT_SORT_DIRECTIONS: Record<SortKey, SortDirection> = {
  project: 'asc',
  sessions: 'desc',
  messages: 'desc',
  tokens: 'desc',
  cost: 'desc',
}

const DEFAULT_TABLE_SORT: SortState<SortKey> = { key: 'cost', direction: 'desc' }

interface EnrichedProjectRow extends ProjectEntry {
  totalTokens: number
  tokenShare: number // 0..1
  avgCostPerSession: number
}

function getProjectLabel(project: ProjectEntry) {
  return project.project_name || 'Unnamed project'
}

function getProjectIdentifier(project: ProjectEntry) {
  if (!project.project_id) return 'unknown'
  return project.project_id.length > 12 ? project.project_id.slice(0, 12) : project.project_id
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

export function ProjectsView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [sortState, setSortState] = useState<SortState<SortKey> | null>(null)
  const [selectedProject, setSelectedProject] = useState<{ sourceId: SourceID; projectId: string } | null>(null)

  // Reset sort + drill-down when the period changes, mirroring other views.
  const { cacheKey } = usePeriodControls({
    onChange: () => {
      setSortState(null)
      setSelectedProject(null)
    },
  })
  const { data, loading, error } = usePeriodResource(getProjects, cacheKey)

  const sourceLabel = selectedSourceInfo?.label ?? selectedSourceId
  // Only keep the drawer open when the selection matches the active source.
  const selectedProjectId = selectedProject?.sourceId === selectedSourceId ? selectedProject.projectId : null

  const summary = useMemo(() => {
    if (!data) return null

    const totalCost = data.projects.reduce((a, p) => a + p.cost, 0)
    const totalSessions = data.projects.reduce((a, p) => a + p.sessions, 0)
    const totalMessages = data.projects.reduce((a, p) => a + p.messages, 0)

    const rows = data.projects.map<EnrichedProjectRow>((project) => ({
      ...project,
      totalTokens: getTokenTotal(project.tokens),
      avgCostPerSession: safeDivide(project.cost, project.sessions),
      tokenShare: 0, // filled below once the total is known
    }))
    const totalTokens = rows.reduce((a, r) => a + r.totalTokens, 0)
    for (const r of rows) r.tokenShare = safeDivide(r.totalTokens, totalTokens)

    const effectiveSort = sortState ?? DEFAULT_TABLE_SORT
    const sortedRows = [...rows].sort((left, right) => {
      const primary = compareRows(effectiveSort.key, left, right)
      const m = effectiveSort.direction === DEFAULT_SORT_DIRECTIONS[effectiveSort.key] ? 1 : -1
      const d = primary * m
      if (d !== 0) return d
      if (right.cost !== left.cost) return right.cost - left.cost
      return getProjectLabel(left).localeCompare(getProjectLabel(right))
    })

    return { rows: sortedRows, totalCost, totalSessions, totalMessages, totalTokens, empty: rows.length === 0 }
  }, [data, sortState])

  const onSort = (key: string) => setSortState((s) => getNextSortState(s, key as SortKey, DEFAULT_SORT_DIRECTIONS[key as SortKey]))

  const sortSpec: SortSpec | null = sortState ? { key: sortState.key, dir: sortState.direction } : null

  const handleProjectSelect = (project: EnrichedProjectRow) => {
    if (!project.project_id) return
    setSelectedProject({ sourceId: selectedSourceId, projectId: project.project_id })
  }

  const columns: Column<EnrichedProjectRow>[] = [
    {
      key: 'project',
      header: 'Project',
      sortable: true,
      width: '34%',
      render: (row) => (
        <span style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
          <Icon name="folder" size={16} color="var(--fg-muted)" />
          <span style={{ display: 'flex', flexDirection: 'column', gap: 3, minWidth: 0 }}>
            <span style={{ font: '600 13px/1.2 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {getProjectLabel(row)}
            </span>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, font: '400 11px/1 var(--font-mono)', color: 'var(--fg-faint)' }}>
              <span>id {getProjectIdentifier(row)}</span>
              <span aria-hidden="true">·</span>
              <span>{formatCurrencyWithProvenance(row.avgCostPerSession, row.cost_status, row.cost_provenance)} / session</span>
            </span>
          </span>
        </span>
      ),
    },
    { key: 'source', header: 'Source', render: (row) => <VendorChip id={row.source_id ?? selectedSourceId} /> },
    { key: 'sessions', header: 'Sessions', numeric: true, sortable: true, render: (row) => formatCompactInteger(row.sessions) },
    { key: 'messages', header: 'Messages', numeric: true, sortable: true, render: (row) => formatCompactInteger(row.messages) },
    {
      key: 'tokens',
      header: 'Tokens',
      numeric: true,
      sortable: true,
      render: (row) => <span title={formatInteger(row.totalTokens)}>{formatTokenCount(row.totalTokens)}</span>,
    },
    {
      key: 'cost',
      header: 'Est. cost',
      numeric: true,
      sortable: true,
      render: (row) => formatCompactCurrencyWithProvenance(row.cost, row.cost_status, row.cost_provenance),
    },
    {
      key: 'share',
      header: 'Token share',
      numeric: true,
      width: 140,
      render: (row) => {
        const pct = Math.round(row.tokenShare * 100)
        return (
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, justifyContent: 'flex-end' }}>
            <span style={{ width: 54, height: 6, borderRadius: 3, background: 'var(--ink-700)', overflow: 'hidden' }}>
              <span style={{ display: 'block', width: `${row.totalTokens > 0 ? Math.max(pct, 3) : 0}%`, height: '100%', background: vendorMeta(row.source_id ?? selectedSourceId).color }} />
            </span>
            <span style={{ width: 34, textAlign: 'right' }}>{pct}%</span>
          </span>
        )
      },
    },
  ]

  // Loading state
  if (loading && !data) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)', padding: 16 }}>
              <Skeleton width={90} height={11} />
              <Skeleton width={120} height={28} style={{ marginTop: 12 }} />
            </div>
          ))}
        </div>
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 320 }} />
      </div>
    )
  }

  if (!summary) {
    return <Card><ErrorState title="Projects failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && <Notice tone="warning" title="Projects partially loaded">{error}</Notice>}

      {/* KPI row */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: 12 }}>
        <StatCard
          accent
          label="Tracked projects"
          value={formatInteger(summary.rows.length)}
          hint={summary.rows.length === 1 ? 'One project visible' : `Distinct projects from ${sourceLabel}`}
        />
        <StatCard
          label="Total cost"
          value={formatCurrencyWithProvenance(summary.totalCost, data?.cost_status, data?.cost_provenance)}
          hint={`${formatCompactInteger(summary.totalMessages)} messages attributed`}
        />
        <StatCard label="Sessions touched" value={formatInteger(summary.totalSessions)} hint="across all projects" />
        <StatCard
          accent
          label="Token load"
          value={formatTokenCount(summary.totalTokens)}
          title={formatInteger(summary.totalTokens)}
          hint="combined across projects"
        />
      </div>

      {/* Ranking table */}
      {summary.empty ? (
        <Card>
          <Notice tone="info" title="No project activity recorded yet">
            Once activity lands, this view ranks {sourceLabel} projects by cost and shows message and token load.
          </Notice>
        </Card>
      ) : (
        <Card
          title="Project usage ranking"
          subtitle={`${summary.rows.length} projects · ${formatTokenCount(summary.totalTokens)} tokens · costs per source`}
          pad={0}
        >
          <DataTable
            columns={columns}
            rows={summary.rows}
            sort={sortSpec}
            onSort={onSort}
            onRowClick={handleProjectSelect}
            rowKey={(row) => row.project_id || getProjectLabel(row)}
          />
        </Card>
      )}

      {/* Drill-down drawer */}
      <ProjectDrilldownDrawer projectId={selectedProjectId} period={cacheKey} onClose={() => setSelectedProject(null)} />
    </div>
  )
}

// ── Drill-down drawer ──────────────────────────────────────────────

const SESSIONS_PER_PAGE = 10

function getSessionLabel(session: SessionEntry) {
  return session.title || 'Untitled session'
}

function getSessionProjectLabel(session: SessionEntry) {
  return session.project_name || session.project_id || 'No linked project'
}

function ProjectDrilldownDrawer({ projectId, period, onClose }: { projectId: string | null; period: string; onClose: () => void }) {
  const { selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [detail, setDetail] = useState<ProjectDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sessionPage, setSessionPage] = useState(1)
  const [requestNonce, setRequestNonce] = useState(0)

  // Reset paging/detail when the target project changes.
  useEffect(() => {
    setSessionPage(1)
    setDetail(null)
    setError(null)
    setRequestNonce(0)
  }, [projectId])

  useEffect(() => {
    if (projectId === null) return
    const activeProjectId = projectId
    const ctrl = new AbortController()

    async function load() {
      setLoading(true)
      setError(null)
      try {
        const next = await getProjectDetail(activeProjectId, period, sessionPage, SESSIONS_PER_PAGE, ctrl.signal, selectedSourceId)
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
  }, [projectId, period, sessionPage, requestNonce, selectedSourceId])

  const open = projectId !== null
  const totalPages = detail ? Math.max(1, Math.ceil(detail.total_sessions / SESSIONS_PER_PAGE)) : 0

  const sessionCols: Column<SessionEntry>[] = [
    {
      key: 'session',
      header: 'Session',
      render: (s) => (
        <span style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
          <span style={{ font: '500 13px/1.2 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{getSessionLabel(s)}</span>
          <span style={{ font: '400 11px/1 var(--font-mono)', color: 'var(--fg-faint)' }}>{getSessionProjectLabel(s)}</span>
        </span>
      ),
    },
    { key: 'activity', header: 'Activity', render: (s) => <span style={{ font: '400 12px/1 var(--font-mono)', color: 'var(--fg-muted)' }}>{formatDateTime(s.time_created)}</span> },
    { key: 'messages', header: 'Msgs', numeric: true, render: (s) => formatCompactInteger(s.message_count) },
    { key: 'cost', header: 'Cost', numeric: true, render: (s) => formatCompactCurrencyWithProvenance(s.cost, s.cost_status, s.cost_provenance) },
  ]

  return (
    <Drawer
      open={open}
      onClose={onClose}
      title={detail?.project_name || 'Project detail'}
      subtitle={`${selectedSourceInfo?.label ?? selectedSourceId} · ${period}`}
      width={640}
    >
      {loading && !detail ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 12 }}>
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)', padding: 14 }}>
                <Skeleton width={70} height={10} />
                <Skeleton width={90} height={24} style={{ marginTop: 10 }} />
              </div>
            ))}
          </div>
          <Skeleton height={180} radius="var(--radius-lg)" />
        </div>
      ) : error ? (
        <ErrorState title="Project detail failed to load" message={error} onRetry={() => setRequestNonce((v) => v + 1)} />
      ) : detail ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {/* Header chips */}
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
            <Badge tone="accent">Project drilldown</Badge>
            {detail.project_id && <Badge dot>id {detail.project_id.slice(0, 12)}</Badge>}
          </div>

          {/* Aggregate KPI cards */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 12 }}>
            <StatCard label="Sessions" value={formatInteger(detail.sessions)} hint="in selected period" />
            <StatCard label="Messages" value={formatInteger(detail.messages)} hint="assistant messages" />
            <StatCard
              label="Est. cost"
              value={formatCompactCurrencyWithProvenance(detail.cost, detail.cost_status, detail.cost_provenance)}
              hint={`${formatCurrencyWithProvenance(safeDivide(detail.cost, Math.max(1, detail.sessions)), detail.cost_status, detail.cost_provenance)} / session`}
            />
            <StatCard
              label="Token load"
              value={formatTokenCount(getTokenTotal(detail.tokens))}
              title={formatInteger(getTokenTotal(detail.tokens))}
              hint={`${formatCompactInteger(detail.tokens.input)} in · ${formatCompactInteger(detail.tokens.output)} out`}
            />
          </div>

          {/* Recent sessions */}
          <Card title="Recent sessions" subtitle="Most recent first" pad={0}>
            {detail.recent_sessions && detail.recent_sessions.length > 0 ? (
              <>
                <DataTable dense columns={sessionCols} rows={detail.recent_sessions} rowKey={(s) => s.id} />
                {totalPages > 1 && (
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, padding: '10px 14px', borderTop: '1px solid var(--border-subtle)' }}>
                    <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>
                      Page {sessionPage} of {totalPages} · {formatInteger(detail.total_sessions)} sessions
                    </span>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <Button variant="ghost" size="sm" disabled={sessionPage <= 1} onClick={() => setSessionPage((p) => Math.max(1, p - 1))}>Previous</Button>
                      <Button variant="ghost" size="sm" disabled={sessionPage >= totalPages} onClick={() => setSessionPage((p) => p + 1)}>Next</Button>
                    </div>
                  </div>
                )}
              </>
            ) : (
              <div style={{ padding: '24px 16px', textAlign: 'center', font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)' }}>
                No session activity for this project in the selected period.
              </div>
            )}
          </Card>
        </div>
      ) : null}
    </Drawer>
  )
}

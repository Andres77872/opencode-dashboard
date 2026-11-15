/* Project drill-down drawer — one project's totals and its sessions, paged.
   It opens on the totals the ranking row already holds, so there is something
   to read at once; the detail request then adds the worktree and the sessions. */
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  Button,
  Card,
  DataTable,
  Drawer,
  EmptyState,
  Notice,
  Pagination,
  Skeleton,
  StatCard,
  StatGrid,
  type Column,
} from '../vael'
import { useDashboardContext } from '../layout/dashboard-context'
import { getSessionLabel } from '../sessions/session-labels'
import { useHeldValue } from '../../lib/use-held-value'
import { getProjectDetail } from '../../lib/api'
import { useCachedResource } from '../../lib/use-cached-resource'
import { getTokenTotal } from '../../lib/token-breakdown'
import {
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCurrencyExact,
  formatCurrencyWithProvenance,
  formatDateTime,
  formatDuration,
  formatInteger,
  formatTokenCount,
  getCostLabel,
  isMissingCost,
  safeDivide,
} from '../../lib/format'
import type { ProjectDetail, ProjectEntry, SessionEntry } from '../../types/api'

const SESSIONS_PER_PAGE = 10

/** URL params that carry the selected time range from one view to another. */
const PERIOD_PARAMS = ['period', 'from', 'to', 'mode'] as const

export interface ProjectDrawerProps {
  /** The ranking row to drill into; null keeps the drawer closed. */
  project: ProjectEntry | null
  /** Period cache key the ranking was loaded for. */
  period: string
  periodLabel: string
  onClose: () => void
}

export function ProjectDrawer({ project, period, periodLabel, onClose }: ProjectDrawerProps) {
  const { selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const sourceLabel = selectedSourceInfo?.label ?? selectedSourceId

  return (
    <Drawer
      open={project !== null}
      onClose={onClose}
      title={project ? project.project_name || 'Unnamed project' : undefined}
      subtitle={`${sourceLabel} · ${periodLabel}`}
      width={640}
    >
      {/* Keyed so paging starts over with the project or the period, in the
          same render that changes them: no request for a page of the old one. */}
      {project && <ProjectDrawerBody key={`${project.project_id || project.project_name}|${period}`} project={project} period={period} />}
    </Drawer>
  )
}

function ProjectDrawerBody({ project, period }: { project: ProjectEntry; period: string }) {
  const {
    requestRefresh,
    selectedSourceId,
    sourceMetadata,
    sourceMetadataLoading,
    sourceAvailable,
    sourceStateError,
  } = useDashboardContext()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [page, setPage] = useState(1)
  const projectId = project.project_id ?? ''

  const scope = `${sourceMetadata?.sources_revision ?? 0}:${selectedSourceId}::${period}|${projectId}`
  const { data: loaded, loading, error } = useCachedResource<ProjectDetail>({
    key: `project-detail|${scope}|${page}`,
    ready: !sourceMetadataLoading,
    blocked: sourceAvailable ? null : (sourceStateError?.message ?? 'Selected source is unavailable'),
    // A project without an id cannot be asked for; its row totals still show.
    enabled: projectId !== '',
    load: (signal) => getProjectDetail(projectId, period, page, SESSIONS_PER_PAGE, signal, selectedSourceId),
    fallbackError: 'Failed to load project detail',
  })
  // Keeps the current page of sessions on screen while the next one loads.
  const detail = useHeldValue(scope, loaded)
  const pagePending = loading && loaded === null

  // Sessions already reads ?project_id=, and a session id only means something
  // within its source, so both links name the source and keep the time range.
  const sessionsHref = (sessionId?: string) => {
    const params = new URLSearchParams({ project_id: projectId, source: selectedSourceId })
    for (const key of PERIOD_PARAMS) {
      const value = searchParams.get(key)
      if (value) params.set(key, value)
    }
    if (sessionId) params.set('session', sessionId)
    return `/sessions?${params.toString()}`
  }

  const totals = detail ?? project
  const costStatus = totals.cost_status
  const costProvenance = totals.cost_provenance
  const costMissing = isMissingCost(costStatus, costProvenance)
  const totalTokens = getTokenTotal(totals.tokens)
  const sessions = detail?.recent_sessions ?? []

  const sessionColumns: Column<SessionEntry>[] = [
    {
      key: 'session',
      header: 'Session',
      width: '100%',
      render: (s) => {
        const label = getSessionLabel(s)
        return (
          // Inline-size containment: a long title truncates to the column
          // instead of pushing the numbers out of the drawer.
          <span style={{ display: 'flex', flexDirection: 'column', gap: 3, contain: 'inline-size', minWidth: 120 }}>
            <span title={label} style={{ font: '500 13px/1.25 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{label}</span>
            <span style={{ font: '400 11px/1.2 var(--font-mono)', color: 'var(--fg-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
              {formatDateTime(s.time_created)} · {formatDuration(s.time_created, s.time_updated)}
            </span>
          </span>
        )
      },
    },
    {
      key: 'messages',
      header: 'Msgs',
      numeric: true,
      render: (s) => <span title={`${formatInteger(s.message_count)} messages`}>{formatCompactInteger(s.message_count)}</span>,
    },
    {
      key: 'cost',
      header: getCostLabel(costStatus, costProvenance),
      numeric: true,
      render: (s) => formatCompactCurrencyWithProvenance(s.cost, s.cost_status ?? costStatus, s.cost_provenance ?? costProvenance),
    },
  ]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <StatGrid>
        <StatCard label="Sessions" value={formatInteger(totals.sessions)} hint="active in this range" />
        <StatCard label="Messages" value={formatCompactInteger(totals.messages)} title={`${formatInteger(totals.messages)} messages`} hint="in this project" />
        <StatCard
          label={getCostLabel(costStatus, costProvenance)}
          value={formatCurrencyWithProvenance(totals.cost, costStatus, costProvenance)}
          title={costMissing ? undefined : formatCurrencyExact(totals.cost)}
          hint={costMissing || totals.sessions === 0 ? 'this project' : `${formatCurrencyWithProvenance(safeDivide(totals.cost, totals.sessions), costStatus, costProvenance)} / session`}
        />
        <StatCard
          label="Tokens"
          value={formatTokenCount(totalTokens)}
          title={`${formatInteger(totalTokens)} tokens`}
          hint={`${formatTokenCount(totals.tokens.input)} in · ${formatTokenCount(totals.tokens.output)} out`}
        />
      </StatGrid>

      {(projectId || detail?.worktree) && (
        <Card bodyStyle={{ padding: '2px 16px' }}>
          {projectId && <Fact label="Project id" value={projectId} last={!detail?.worktree} />}
          {detail?.worktree && <Fact label="Worktree" value={detail.worktree} last />}
        </Card>
      )}

      {!projectId ? (
        <Notice tone="info" title="Sessions can't be listed for this project">
          The source recorded this activity without a project id, so there is nothing to look its sessions up by.
        </Notice>
      ) : (
        <>
          {error && (
            <Notice
              tone="warning"
              title={detail ? "Couldn't refresh — showing the last loaded data" : "Couldn't load this project's sessions"}
              action={<Button size="sm" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}
            >
              {error}
            </Notice>
          )}
          <Card
            flush
            // The API lists a project's sessions newest first without bounding
            // them to the range, unlike the totals above; say so.
            title="Recent sessions"
            subtitle="All time, most recent first"
            action={<Button size="sm" iconLeft="external-link" onClick={() => navigate(sessionsHref())}>Open in Sessions</Button>}
          >
            {!detail ? (
              error ? (
                <EmptyState icon="alert-triangle" title="Sessions unavailable" description="The totals above come from the project ranking." />
              ) : (
                <div role="status" aria-label="Loading sessions" style={{ display: 'flex', flexDirection: 'column', gap: 14, padding: 16 }}>
                  {Array.from({ length: 5 }).map((_, i) => (
                    <Skeleton key={i} height={14} width={`${88 - i * 9}%`} />
                  ))}
                </div>
              )
            ) : sessions.length === 0 ? (
              <EmptyState icon="clock" title="No sessions recorded" description="This project has no recorded sessions to list." />
            ) : (
              <>
                <div aria-busy={pagePending} style={{ opacity: pagePending ? 0.55 : 1, transition: 'opacity var(--dur-fast) var(--ease-out)' }}>
                  <DataTable
                    bare
                    columns={sessionColumns}
                    rows={sessions}
                    rowKey={(s) => s.id}
                    onRowClick={(s) => navigate(sessionsHref(s.id))}
                  />
                </div>
                {detail.total_sessions > SESSIONS_PER_PAGE && (
                  <Pagination
                    page={page}
                    pageSize={SESSIONS_PER_PAGE}
                    total={detail.total_sessions}
                    noun="sessions"
                    onPageChange={setPage}
                    disabled={pagePending}
                  />
                )}
              </>
            )}
          </Card>
        </>
      )}
    </div>
  )
}

/**
 * A labelled identifier shown in full. Ids and paths are too long for one line
 * and are what a reader copies, so they wrap and select as a whole.
 */
function Fact({ label, value, last = false }: { label: string; value: string; last?: boolean }) {
  return (
    <div style={{ padding: '10px 0', borderBottom: last ? 'none' : '1px solid var(--border-subtle)' }}>
      <div style={{ font: '400 12px/1.2 var(--font-ui)', color: 'var(--fg-muted)' }}>{label}</div>
      <div style={{ marginTop: 4, font: '500 12px/1.5 var(--font-mono)', color: 'var(--fg-secondary)', overflowWrap: 'anywhere', userSelect: 'all' }}>{value}</div>
    </div>
  )
}

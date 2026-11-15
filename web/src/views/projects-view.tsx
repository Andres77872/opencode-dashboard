/* Projects — per-source project ranking (Vael). Costs are reported per source and
   never combined across sources. No fabricated data: the API has no per-project
   trend, branch name, or period-over-period delta, so none is shown. Token
   share is a real derived ratio (project tokens / total). */
import { useMemo, useState } from 'react'
import {
  Button,
  Card,
  DataTable,
  EmptyState,
  ErrorState,
  Icon,
  Notice,
  SearchInput,
  ShareBar,
  StatCard,
  StatGrid,
  VendorChip,
  ViewSkeleton,
  vendorMeta,
  type Column,
  type SortSpec,
} from '../components/vael'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { ProjectDrawer } from '../components/projects/project-drawer'
import { usePeriodControls, useResetOnChange } from '../lib/use-period-controls'
import { usePeriodResource } from '../lib/use-period-resource'
import { useMediaQuery } from '../lib/use-media-query'
import { periodTriggerLabel } from '../lib/period-label'
import { isTranscriptSource } from '../lib/message-display'
import { getProjects } from '../lib/api'
import { getTokenTotal } from '../lib/token-breakdown'
import { getNextSortState, type SortDirection, type SortState } from '../lib/table-sort'
import {
  formatCompactCurrencyWithProvenance,
  formatCompactInteger,
  formatCount,
  formatCurrencyExact,
  formatCurrencyWithProvenance,
  formatInteger,
  formatShare,
  formatShortId,
  formatTokenCount,
  getCostLabel,
  isMissingCost,
  safeDivide,
} from '../lib/format'
import type { ProjectEntry, SourceID } from '../types/api'

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
  /** Unique within the list; the project id when there is one. */
  rowKey: string
  /** Unnamed, or sharing its name with another project in the list. */
  ambiguousName: boolean
  totalTokens: number
  tokenShare: number // 0..1
  avgCostPerSession: number
}

interface ProjectSelection {
  sourceId: SourceID
  period: string
  rowKey: string
}

function getProjectLabel(project: ProjectEntry) {
  return project.project_name || 'Unnamed project'
}

/**
 * The id as a secondary line under the name. It is only worth the space when
 * the name alone does not identify the row; the full id is always in the
 * row's tooltip and in the drawer. Path-style ids (Claude Code's
 * "-home-user-code-app") share their prefix across projects, so their
 * distinguishing tail is what is shown.
 */
function getProjectIdHint(project: ProjectEntry): string | null {
  const id = project.project_id
  if (!id) return null
  if (/^[-/]/.test(id)) return id.length > 26 ? `…${id.slice(-25)}` : id
  return formatShortId(id)
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
  const [selection, setSelection] = useState<ProjectSelection | null>(null)
  const [filter, setFilter] = useState('')
  const compact = useMediaQuery('(max-width: 760px)')

  const { cacheKey, state: periodState } = usePeriodControls()
  const periodLabel = periodTriggerLabel(periodState)
  // Sort, filter and the open drawer describe one source's ranking for one
  // range; start over when either changes.
  useResetOnChange(`${selectedSourceId}|${cacheKey}`, () => {
    setSortState(null)
    setFilter('')
    setSelection(null)
  })
  const { data, loading, error } = usePeriodResource(getProjects, cacheKey, { cacheId: 'projects' })

  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name
  const effectiveSort = sortState ?? DEFAULT_TABLE_SORT

  const summary = useMemo(() => {
    if (!data) return null

    const nameCounts = new Map<string, number>()
    for (const project of data.projects) {
      const name = getProjectLabel(project)
      nameCounts.set(name, (nameCounts.get(name) ?? 0) + 1)
    }
    const seen = new Map<string, number>()
    const rows = data.projects.map<EnrichedProjectRow>((project) => {
      const base = project.project_id || getProjectLabel(project)
      const repeat = seen.get(base) ?? 0
      seen.set(base, repeat + 1)
      return {
        ...project,
        rowKey: repeat === 0 ? base : `${base}#${repeat}`,
        ambiguousName: !project.project_name || (nameCounts.get(getProjectLabel(project)) ?? 0) > 1,
        totalTokens: getTokenTotal(project.tokens),
        avgCostPerSession: safeDivide(project.cost, project.sessions),
        tokenShare: 0, // filled below once the total is known
      }
    })
    const totalTokens = rows.reduce((sum, row) => sum + row.totalTokens, 0)
    for (const row of rows) row.tokenShare = safeDivide(row.totalTokens, totalTokens)

    return {
      rows,
      totalCost: rows.reduce((sum, row) => sum + row.cost, 0),
      totalSessions: rows.reduce((sum, row) => sum + row.sessions, 0),
      totalMessages: rows.reduce((sum, row) => sum + row.messages, 0),
      totalTokens,
      topByTokens: rows.reduce<EnrichedProjectRow | null>((top, row) => (!top || row.totalTokens > top.totalTokens ? row : top), null),
      // Rows carry their own source only when a list mixes several.
      mixedSources: rows.some((row) => row.source_id && row.source_id !== selectedSourceId),
    }
  }, [data, selectedSourceId])

  // Matches the TUI's `/` filter, which matches on project name *or* id.
  const visibleRows = useMemo(() => {
    const rows = summary?.rows ?? []
    const needle = filter.trim().toLowerCase()
    const matching = needle
      ? rows.filter((row) => getProjectLabel(row).toLowerCase().includes(needle) || (row.project_id ?? '').toLowerCase().includes(needle))
      : rows
    const flip = effectiveSort.direction === DEFAULT_SORT_DIRECTIONS[effectiveSort.key] ? 1 : -1
    return [...matching].sort((left, right) => {
      const primary = compareRows(effectiveSort.key, left, right) * flip
      if (primary !== 0) return primary
      if (right.cost !== left.cost) return right.cost - left.cost
      return getProjectLabel(left).localeCompare(getProjectLabel(right))
    })
  }, [summary?.rows, filter, effectiveSort])

  if (!data || !summary) {
    if (loading) return <ViewSkeleton stats={4} panels={[420]} label="Loading projects" />
    return <Card><ErrorState title="Projects failed to load" message={error ?? undefined} onRetry={requestRefresh} /></Card>
  }

  // The drawer belongs to the source and range it was opened under: once
  // either changes, the selection no longer names a row of this ranking.
  const selectedRow = selection && selection.sourceId === selectedSourceId && selection.period === cacheKey
    ? summary.rows.find((row) => row.rowKey === selection.rowKey) ?? null
    : null

  const costStatus = data.cost_status
  const costProvenance = data.cost_provenance
  const costMissing = isMissingCost(costStatus, costProvenance)
  const costLabel = getCostLabel(costStatus, costProvenance)
  const count = summary.rows.length

  // The table always shows its sort: the ranking opens ordered by cost, and
  // clicking that header reverses it instead of appearing to do nothing.
  const sortSpec: SortSpec = { key: effectiveSort.key, dir: effectiveSort.direction }
  const onSort = (key: string) =>
    setSortState((current) => getNextSortState(current ?? DEFAULT_TABLE_SORT, key as SortKey, DEFAULT_SORT_DIRECTIONS[key as SortKey]))

  const projectCell = (row: EnrichedProjectRow, sub: string) => (
    <span style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
      <Icon name="folder" size={16} color="var(--fg-muted)" style={{ flexShrink: 0 }} />
      {/* Inline-size containment lets the name truncate to the column instead
          of the column growing to the longest name. */}
      <span style={{ display: 'flex', flexDirection: 'column', gap: 3, flex: 1, contain: 'inline-size', minWidth: compact ? 120 : 180 }}>
        <span title={getProjectLabel(row)} style={{ font: '600 13px/1.25 var(--font-ui)', color: 'var(--fg-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {getProjectLabel(row)}
        </span>
        <span title={row.project_id || undefined} style={{ font: '400 11px/1.2 var(--font-mono)', color: 'var(--fg-faint)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {sub}
        </span>
      </span>
    </span>
  )

  const costPerSession = (row: EnrichedProjectRow) => {
    const status = row.cost_status ?? costStatus
    const provenance = row.cost_provenance ?? costProvenance
    if (isMissingCost(status, provenance)) return 'cost unknown'
    return `${formatCurrencyWithProvenance(row.avgCostPerSession, status, provenance)} / session`
  }

  const projectColumn: Column<EnrichedProjectRow> = {
    key: 'project',
    header: 'Project',
    sortable: true,
    width: '100%',
    render: compact
      // On a phone the counts fold into the name cell.
      ? (row) => projectCell(row, `${formatCount(row.sessions, 'session')} · ${formatTokenCount(row.totalTokens)} tokens`)
      : (row) => projectCell(row, [row.ambiguousName ? getProjectIdHint(row) : null, costPerSession(row)].filter(Boolean).join(' · ')),
  }

  const costColumn: Column<EnrichedProjectRow> = {
    key: 'cost',
    header: costLabel,
    numeric: true,
    sortable: true,
    render: (row) => {
      const status = row.cost_status ?? costStatus
      const provenance = row.cost_provenance ?? costProvenance
      return (
        <span title={isMissingCost(status, provenance) ? undefined : formatCurrencyExact(row.cost)}>
          {formatCompactCurrencyWithProvenance(row.cost, status, provenance)}
        </span>
      )
    },
  }

  const columns: Column<EnrichedProjectRow>[] = compact
    ? [projectColumn, costColumn]
    : [
        projectColumn,
        ...(summary.mixedSources
          ? [{ key: 'source', header: 'Source', render: (row: EnrichedProjectRow) => <VendorChip id={row.source_id ?? selectedSourceId} /> }]
          : []),
        { key: 'sessions', header: 'Sessions', numeric: true, sortable: true, render: (row) => formatCompactInteger(row.sessions) },
        {
          key: 'messages',
          header: 'Messages',
          numeric: true,
          sortable: true,
          render: (row) => <span title={`${formatInteger(row.messages)} messages`}>{formatCompactInteger(row.messages)}</span>,
        },
        {
          key: 'tokens',
          header: 'Tokens',
          numeric: true,
          sortable: true,
          render: (row) => <span title={`${formatInteger(row.totalTokens)} tokens`}>{formatTokenCount(row.totalTokens)}</span>,
        },
        costColumn,
        {
          key: 'share',
          header: 'Token share',
          numeric: true,
          render: (row) => <ShareBar share={row.tokenShare} color={vendorMeta(row.source_id ?? selectedSourceId).color} />,
        },
      ]

  const refreshNotice = error && (
    <Notice
      tone="warning"
      title="Couldn't refresh — showing the last loaded data"
      action={<Button size="sm" iconLeft="refresh" onClick={requestRefresh}>Retry</Button>}
    >
      {error}
    </Notice>
  )

  if (count === 0) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {refreshNotice}
        <Card>
          <EmptyState
            icon="folder"
            title="No project activity in this range"
            description={
              isTranscriptSource(selectedSourceInfo)
                ? `No ${sourceLabel} activity was found in readable local transcripts for this range. Try a longer range.`
                : `${sourceLabel} recorded no project activity in this range. Try a longer range.`
            }
          />
        </Card>
      </div>
    )
  }

  const needle = filter.trim()
  const filterBox = (
    <SearchInput value={filter} onChange={setFilter} placeholder="Filter projects…" label="Filter projects by name or id" width={compact ? '100%' : 260} />
  )

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {refreshNotice}

      <StatGrid>
        <StatCard accent label="Projects" value={formatInteger(count)} hint={periodLabel} />
        <StatCard
          label={costLabel}
          value={formatCurrencyWithProvenance(summary.totalCost, costStatus, costProvenance)}
          title={costMissing ? undefined : formatCurrencyExact(summary.totalCost)}
          hint={`${formatCompactInteger(summary.totalMessages)} messages`}
        />
        <StatCard
          label="Sessions"
          value={formatInteger(summary.totalSessions)}
          hint={costMissing || summary.totalSessions === 0 ? 'across all projects' : `${formatCurrencyWithProvenance(safeDivide(summary.totalCost, summary.totalSessions), costStatus, costProvenance)} / session`}
        />
        <StatCard
          label="Tokens"
          value={formatTokenCount(summary.totalTokens)}
          title={`${formatInteger(summary.totalTokens)} tokens`}
          hint={summary.topByTokens && summary.totalTokens > 0 ? `${formatShare(summary.topByTokens.totalTokens, summary.totalTokens)} in ${getProjectLabel(summary.topByTokens)}` : 'across all projects'}
        />
      </StatGrid>

      <Card
        flush
        title="Project ranking"
        subtitle={
          needle
            ? `${formatInteger(visibleRows.length)} of ${formatCount(count, 'project')} match`
            : `${formatCount(count, 'project')} · select one to see its sessions`
        }
        action={compact ? undefined : filterBox}
      >
        {/* On a phone the filter box gets a full-width row of its own. */}
        {compact && <div style={{ padding: '12px 16px', borderBottom: '1px solid var(--border-subtle)' }}>{filterBox}</div>}
        {visibleRows.length === 0 ? (
          <EmptyState
            icon="search"
            title="No projects match"
            description={`No project name or id contains “${needle}”.`}
            action={<Button size="sm" onClick={() => setFilter('')}>Clear filter</Button>}
          />
        ) : (
          <DataTable
            bare
            columns={columns}
            rows={visibleRows}
            sort={sortSpec}
            onSort={onSort}
            onRowClick={(row) => setSelection({ sourceId: selectedSourceId, period: cacheKey, rowKey: row.rowKey })}
            rowKey={(row) => row.rowKey}
          />
        )}
      </Card>

      <ProjectDrawer project={selectedRow} period={cacheKey} periodLabel={periodLabel} onClose={() => setSelection(null)} />
    </div>
  )
}

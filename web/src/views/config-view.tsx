/* Config — four sections behind one tab row: which sources feed the dashboard,
   the selected source's config file, its pricing aliases, and the assistant's
   providers. The active section lives in the URL hash, so each one is a link
   (the assistant launcher opens /config#assistant-providers).

   A section is mounted the first time it is opened and then kept, hidden, when
   another is shown: its requests are not made until it is needed, and unsaved
   form input survives a look at another tab. */
import { useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Button, Card, EmptyState, ErrorState, Notice, Skeleton, Tabs, vendorMeta } from '../components/vael'
import { ConfigHeader } from '../components/config/config-header'
import { PricingAliases } from '../components/config/pricing-aliases'
import { DataSources } from '../components/config/data-sources'
import { AssistantProviders } from '../components/config/assistant-providers'
import { ConfigTreePane } from '../components/config/config-tree'
import { SectionNav } from '../components/config/section-nav'
import { SourcePane } from '../components/config/source-pane'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { usePeriodResource } from '../lib/use-period-resource'
import { getConfig } from '../lib/api'
import { useMediaQuery } from '../lib/use-media-query'
import {
  ALL_SECTIONS,
  buildConfigSummary,
  buildSectionProjections,
  normalizeConfigStats,
  normalizeSearchQuery,
  resolveConfigDocument,
} from '../lib/config-utils'
import type { ConfigStats, SourceID } from '../types/api'
import type { ConfigViewMode } from '../types/config'

type ConfigSection = 'data-sources' | 'source-config' | 'pricing-aliases' | 'assistant-providers'

const SECTION_LABELS: Record<ConfigSection, string> = {
  'data-sources': 'Data sources',
  'source-config': 'Source config',
  'pricing-aliases': 'Pricing aliases',
  'assistant-providers': 'Assistant providers',
}
const SECTION_ORDER = Object.keys(SECTION_LABELS) as ConfigSection[]
const DEFAULT_SECTION: ConfigSection = 'data-sources'

function sectionFromHash(hash: string): ConfigSection {
  const value = hash.replace(/^#/, '')
  return (SECTION_ORDER as string[]).includes(value) ? (value as ConfigSection) : DEFAULT_SECTION
}

export function ConfigView() {
  const { hash, search } = useLocation()
  const navigate = useNavigate()
  const { sources, sourceAvailable, selectedSourceId, selectedSourceInfo, sourceStateError } = useDashboardContext()
  const active = sectionFromHash(hash)
  const tabsRef = useRef<HTMLDivElement>(null)

  // Sections already opened stay mounted. Adjusted during render (not in an
  // effect) so the newly opened section is in the same commit as its tab.
  const [visited, setVisited] = useState<ReadonlySet<ConfigSection>>(() => new Set([active]))
  if (!visited.has(active)) setVisited(new Set([...visited, active]))

  // A link to a section from elsewhere on the page (the assistant launcher)
  // can arrive while this view is scrolled: bring the tab row back into view.
  useEffect(() => {
    tabsRef.current?.scrollIntoView?.({ block: 'nearest' })
  }, [active])

  const selectSection = (next: ConfigSection) => {
    // The query string carries the selected source; only the hash changes.
    navigate({ search, hash: `#${next}` }, { replace: true })
  }

  const enabledCount = sources.filter((source) => source.enabled !== false).length
  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name
  const tabs = SECTION_ORDER.map((value) => ({
    value,
    label: SECTION_LABELS[value],
    count: value === 'data-sources' && sources.length > 0 ? `${enabledCount}/${sources.length}` : undefined,
  }))

  const sourceUnavailable = (
    <Card>
      <EmptyState
        icon="database"
        title={`${sourceLabel} is not available`}
        description={sourceStateError?.message ?? 'This section reads from the selected source. Turn the source on, or pick another one in the bar above.'}
        action={<Button size="sm" variant="secondary" onClick={() => selectSection('data-sources')}>Open data sources</Button>}
      />
    </Card>
  )

  const panels: Record<ConfigSection, () => ReactNode> = {
    'data-sources': () => <DataSources />,
    'source-config': () => (sourceAvailable ? <SourceConfigExplorer /> : sourceUnavailable),
    'pricing-aliases': () => (sourceAvailable ? <PricingAliases /> : sourceUnavailable),
    'assistant-providers': () => <AssistantProviders />,
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div ref={tabsRef} style={{ scrollMarginTop: 12 }}>
        <Tabs<ConfigSection> tabs={tabs} value={active} onChange={selectSection} ariaLabel="Config sections" />
      </div>
      {SECTION_ORDER.map((section) =>
        visited.has(section) ? (
          <div key={section} id={section} role="tabpanel" aria-label={SECTION_LABELS[section]} hidden={section !== active}>
            {panels[section]()}
          </div>
        ) : null,
      )}
    </div>
  )
}

function SourceConfigExplorer() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [searchValue, setSearchValue] = useState('')
  const [viewMode, setViewMode] = useState<ConfigViewMode>('tree')
  const [selectedSection, setSelectedSection] = useState<string>(ALL_SECTIONS)
  const [openDepthOverride, setOpenDepthOverride] = useState<number | null>(null)
  const [treeEpoch, setTreeEpoch] = useState(0)
  const narrow = useMediaQuery('(max-width: 880px)')

  // The config endpoint takes no period, so the "period" is a fixed key. The
  // file is cached like any other resource: coming back to this tab shows it
  // at once and a refresh re-reads it in the background.
  const { data: rawData, loading, error } = usePeriodResource<ConfigStats>(
    (_period: string, signal?: AbortSignal, sourceId?: SourceID) => getConfig(signal, sourceId),
    'file',
    { cacheId: 'config' },
  )

  const sourceLabel = selectedSourceInfo?.label ?? vendorMeta(selectedSourceId).name

  const data = useMemo(() => normalizeConfigStats(rawData ?? null), [rawData])
  const doc = useMemo(() => resolveConfigDocument(data), [data])
  const summary = useMemo(() => buildConfigSummary(data, doc), [data, doc])
  const searchQuery = useMemo(() => normalizeSearchQuery(searchValue), [searchValue])
  const projections = useMemo(
    () => buildSectionProjections(summary, searchQuery),
    [searchQuery, summary],
  )

  // Reset navigation state when the underlying config file changes
  // (adjust-state-during-render, per react.dev "You Might Not Need an Effect").
  const resetKey = `${data?.source_id ?? ''}:${data?.path ?? ''}`
  const [prevResetKey, setPrevResetKey] = useState(resetKey)
  if (prevResetKey !== resetKey) {
    setPrevResetKey(resetKey)
    setSelectedSection(ALL_SECTIONS)
    setOpenDepthOverride(null)
    setSearchValue('')
    setViewMode('tree')
  }

  const sectionKeys = useMemo(() => summary?.sections.map((s) => s.key) ?? [], [summary])
  const effectiveSelected =
    selectedSection === ALL_SECTIONS || sectionKeys.includes(selectedSection)
      ? selectedSection
      : ALL_SECTIONS

  const treeAvailable = Boolean(summary && !summary.parseError && summary.sections.length > 0)
  const effectiveViewMode: ConfigViewMode = treeAvailable ? viewMode : 'source'
  const openDepth = openDepthOverride ?? (effectiveSelected === ALL_SECTIONS ? 0 : 1)

  const visibleProjections = useMemo(() => {
    if (effectiveSelected === ALL_SECTIONS) return projections
    return projections.filter((p) => p.section.key === effectiveSelected)
  }, [effectiveSelected, projections])

  const handleSelectSection = (key: string) => {
    setSelectedSection(key)
    setOpenDepthOverride(null)
    setTreeEpoch((e) => e + 1)
  }
  const handleExpandAll = () => {
    setOpenDepthOverride(Number.POSITIVE_INFINITY)
    setTreeEpoch((e) => e + 1)
  }
  const handleCollapseAll = () => {
    setOpenDepthOverride(0)
    setTreeEpoch((e) => e + 1)
  }

  const paneColumns = narrow ? 'minmax(0, 1fr)' : '220px minmax(0, 1fr)'

  // --- Loading ---
  if (loading && !data) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }} aria-busy="true" aria-label="Loading config">
        <Card pad={14}>
          <Skeleton width="min(320px, 70%)" height={13} />
          <Skeleton width="min(220px, 50%)" height={12} style={{ marginTop: 14 }} />
        </Card>
        <div style={{ display: 'grid', gridTemplateColumns: paneColumns, gap: 12, alignItems: 'start' }}>
          {!narrow && (
            <Card pad={12}>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} width="80%" height={12} />
                ))}
              </div>
            </Card>
          )}
          <Card style={{ height: 320 }} />
        </div>
      </div>
    )
  }

  // --- Hard error (no data at all) ---
  if (error && !data) {
    return (
      <Card>
        <ErrorState title="Config failed to load" message={error} onRetry={requestRefresh} />
      </Card>
    )
  }

  if (!data || !doc || !summary) {
    return (
      <Card>
        <EmptyState icon="settings" title="No config available" description={`There is no ${sourceLabel} config snapshot to inspect.`} />
      </Card>
    )
  }

  const status: 'present' | 'missing' | 'parse-error' = !data.exists
    ? 'missing'
    : summary.parseError
      ? 'parse-error'
      : 'present'

  const sourceOnly = data.exists && !treeAvailable && doc.raw.trim() !== ''
  const commentsOnly = summary.emptyObject && doc.raw.trim() !== '' && !summary.parseError

  const visibleSectionCount = projections.filter((p) => p.filteredValue !== null).length
  const matchSummary =
    searchQuery && effectiveViewMode === 'tree' && summary.sections.length > 0
      ? `${visibleSectionCount}/${summary.sections.length} sections`
      : null

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && (
        <Notice
          tone="warning"
          title="Refresh failed — showing the last loaded config"
          action={<Button size="sm" variant="secondary" onClick={requestRefresh}>Retry</Button>}
        >
          {error}
        </Notice>
      )}

      <ConfigHeader
        data={data}
        doc={doc}
        insights={summary.insights}
        status={status}
        searchValue={searchValue}
        onSearchChange={setSearchValue}
        viewMode={effectiveViewMode}
        onViewModeChange={setViewMode}
        showModeToggle={data.exists && treeAvailable && doc.raw !== ''}
        showFilter={data.exists && (treeAvailable || sourceOnly)}
        matchSummary={matchSummary}
      />

      {!data.exists ? (
        <Card>
          <EmptyState
            icon="file-text"
            title="No config file found"
            description={`There is no file at the ${sourceLabel} config path. Once one exists it is shown here as a browsable tree and as highlighted source.`}
          />
        </Card>
      ) : summary.parseError ? (
        <>
          <Notice tone="warning" title="The file could not be parsed">
            {summary.parseError} {doc.raw ? 'The redacted source is shown below.' : 'No redacted copy of the file is available.'}
          </Notice>
          {doc.raw ? (
            <SourcePane doc={doc} searchQuery={searchQuery} />
          ) : (
            <Card>
              <EmptyState icon="alert-triangle" title="Source unavailable" description="The file exists but could not be parsed, and no redacted copy of its text was provided." />
            </Card>
          )}
        </>
      ) : commentsOnly ? (
        <>
          <Notice tone="info" title="No structured values">
            The file parsed but holds no keys — probably only comments. The source is shown below.
          </Notice>
          <SourcePane doc={doc} searchQuery={searchQuery} />
        </>
      ) : summary.emptyObject ? (
        <Card>
          <EmptyState
            icon="file-text"
            title="The config file is empty"
            description="The file exists but has no keys to inspect. It may be blank, intentionally minimal, or fully redacted."
          />
        </Card>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: paneColumns, gap: 12, alignItems: 'start' }}>
          <SectionNav
            projections={projections}
            selectedKey={effectiveSelected}
            onSelect={handleSelectSection}
            searchActive={Boolean(searchQuery)}
            horizontal={narrow}
          />
          {effectiveViewMode === 'tree' ? (
            <ConfigTreePane
              projections={visibleProjections}
              selectedKey={effectiveSelected}
              searchActive={Boolean(searchQuery)}
              openDepth={openDepth}
              treeEpoch={treeEpoch}
              onExpandAll={handleExpandAll}
              onCollapseAll={handleCollapseAll}
              onClearFilter={() => setSearchValue('')}
            />
          ) : (
            <SourcePane doc={doc} searchQuery={searchQuery} />
          )}
        </div>
      )}
    </div>
  )
}

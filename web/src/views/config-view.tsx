/* Config — format-aware two-pane explorer over the redacted config snapshot
   (JSON / TOML / YAML). Left: section navigator. Right: structured tree or
   syntax-highlighted source, toggled in the header. Periodless: the config
   endpoint takes no period, so we lean on usePeriodResource with a fixed
   period arg + cachePeriods:false (re-fetches on source change / refresh).
   No fabricated data — everything derives from the redacted payload. */
import { useEffect, useMemo, useRef, useState } from 'react'
import { Card, EmptyState, ErrorState, Notice, Skeleton, vendorMeta } from '../components/vael'
import { ConfigHeader } from '../components/config/config-header'
import { PricingAliases } from '../components/config/pricing-aliases'
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

const COPY_RESET_MS = 1600

export function ConfigView() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <SourceConfigExplorer />
      <PricingAliases />
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
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const copyResetRef = useRef<number | null>(null)
  const narrow = useMediaQuery('(max-width: 880px)')

  // Periodless: fetcher ignores the period arg; cachePeriods:false always re-fetches.
  const { data: rawData, loading, error } = usePeriodResource<ConfigStats>(
    (_p: string, signal?: AbortSignal, sourceId?: SourceID) => getConfig(signal, sourceId),
    '7d',
    { cachePeriods: false },
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

  useEffect(() => {
    return () => {
      if (copyResetRef.current !== null) window.clearTimeout(copyResetRef.current)
    }
  }, [])

  const handleCopy = async (copyId: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopiedId(copyId)
      if (copyResetRef.current !== null) window.clearTimeout(copyResetRef.current)
      copyResetRef.current = window.setTimeout(() => setCopiedId(null), COPY_RESET_MS)
    } catch {
      setCopiedId(null)
    }
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

  // --- Loading ---
  if (loading && !data) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', padding: 16 }}>
          <Skeleton width={320} height={13} />
          <Skeleton width={220} height={12} style={{ marginTop: 14 }} />
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: narrow ? '1fr' : '220px minmax(0, 1fr)', gap: 12, alignItems: 'start' }}>
          <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', padding: 12, display: 'flex', flexDirection: 'column', gap: 10 }}>
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} width="80%" height={12} />
            ))}
          </div>
          <div style={{ background: 'var(--ink-800)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-xl)', height: 420 }} />
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
        <EmptyState icon="settings" title="No config available" description={`No ${sourceLabel} config snapshot to inspect.`} />
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

  const header = (
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
      copiedId={copiedId}
      onCopy={handleCopy}
    />
  )

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {error && <Notice tone="warning" title="Config partially loaded">{error}</Notice>}

      {header}

      {!data.exists ? (
        <Card>
          <EmptyState
            icon="file-text"
            title="No config file found"
            description={`The resolved ${sourceLabel} config path has no file to inspect. Once a config exists, it is parsed into a browsable tree and a highlighted source view automatically.`}
          />
        </Card>
      ) : summary.parseError ? (
        <>
          <Notice tone="warning" title="Structured parsing failed">
            {summary.parseError} — {doc.raw ? 'the redacted source is shown below.' : 'no safe raw snapshot is available for this file.'}
          </Notice>
          {doc.raw ? (
            <SourcePane doc={doc} searchQuery={searchQuery} copiedId={copiedId} onCopy={handleCopy} />
          ) : (
            <Card>
              <EmptyState icon="alert-triangle" title="Source unavailable" description="The file exists but could not be parsed, and no redacted raw text was provided." />
            </Card>
          )}
        </>
      ) : commentsOnly ? (
        <>
          <Notice tone="info" title="No structured values">
            The file parsed successfully but holds no keys — likely only comments. The source is shown below.
          </Notice>
          <SourcePane doc={doc} searchQuery={searchQuery} copiedId={copiedId} onCopy={handleCopy} />
        </>
      ) : summary.emptyObject ? (
        <Card>
          <EmptyState
            icon="file-text"
            title="Config present but empty"
            description="The file exists, but the redacted payload has no inspectable keys. It may be blank, intentionally minimal, or fully redacted before aggregation."
          />
        </Card>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: narrow ? '1fr' : '220px minmax(0, 1fr)', gap: 12, alignItems: 'start' }}>
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
              copiedId={copiedId}
              onCopy={handleCopy}
            />
          ) : (
            <SourcePane doc={doc} searchQuery={searchQuery} copiedId={copiedId} onCopy={handleCopy} />
          )}
        </div>
      )}
    </div>
  )
}

import { useEffect, useMemo, useRef, useState } from 'react'
import { ConfigOverviewTab } from '../components/config/config-overview-tab'
import { ConfigRawJsonPanel } from '../components/config/config-raw-json-panel'
import { ConfigSectionPanel } from '../components/config/config-section-panel'
import { ConfigSkeleton } from '../components/config/config-skeleton'
import { ConfigStateCard } from '../components/config/config-state-card'
import { ConfigSummaryMetrics } from '../components/config/config-summary-metrics'
import { ConfigWorkspaceHeader } from '../components/config/config-workspace-header'
import { CopyButton } from '../components/config/copy-button'
import { useDashboardContext } from '../components/layout/dashboard-context'
import { Alert } from '../components/ui/alert'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs'
import { buildConfigSummary, buildSectionProjections, normalizeSearchQuery, serializeConfigValue, titleizeKey } from '../lib/config-utils'
import { getConfig } from '../lib/api'
import { usePeriodResource } from '../lib/use-period-resource'
import type { ConfigStats, SourceID } from '../types/api'
import type { DailyPeriod } from '../types/api'

/**
 * Backward-compat shim for config content.
 * Old API returns content as a string (JSON-encoded), new API returns it as an object.
 */
function resolveContent(raw: ConfigStats): ConfigStats {
  if (raw.content && typeof raw.content === 'string') {
    try {
      return { ...raw, content: JSON.parse(raw.content) }
    } catch {
      return raw
    }
  }

  return raw
}

export function ConfigView() {
  const { requestRefresh, selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const [searchValue, setSearchValue] = useState('')
  const [activeTab, setActiveTab] = useState('overview')
  const [hasChosenTab, setHasChosenTab] = useState(false)
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const copyResetRef = useRef<number | null>(null)

  // Config is periodless — use a fixed '7d' as period arg (fetcher ignores it).
  // cachePeriods: false ensures always re-fetch on refreshNonce.
  const { data: rawData, loading, error } = usePeriodResource(
    (_p: string, signal?: AbortSignal, sourceId?: SourceID) => getConfig(signal, sourceId),
    '7d' as DailyPeriod,
    { cachePeriods: false },
  )
  const sourceLabel = selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  // Apply backward-compat shim for config content
  const data = useMemo(() => (rawData ? resolveContent(rawData) : null), [rawData])

  useEffect(() => {
    return () => {
      if (copyResetRef.current !== null) {
        window.clearTimeout(copyResetRef.current)
      }
    }
  }, [])

  const searchQuery = useMemo(() => normalizeSearchQuery(searchValue), [searchValue])
  const summary = useMemo(() => buildConfigSummary(data), [data])
  const sectionProjections = useMemo(() => buildSectionProjections(summary, searchQuery), [searchQuery, summary])

  const effectiveActiveTab = useMemo(() => {
    if (!summary) {
      return activeTab
    }

    const availableTabs = new Set(['overview', 'raw', ...summary.sections.map((section) => section.key)])

    if (!hasChosenTab && summary.sections.length > 0) {
      return summary.sections[0].key
    }

    if (!availableTabs.has(activeTab)) {
      return summary.sections[0]?.key ?? 'overview'
    }

    return activeTab
  }, [activeTab, hasChosenTab, summary])

  const handleActiveTabChange = (value: string) => {
    setHasChosenTab(true)
    setActiveTab(value)
  }

  const visibleSectionCount = sectionProjections.filter((projection) => projection.filteredValue !== null).length
  const totalSectionCount = summary?.sections.length ?? 0

  const handleRetry = () => {
    requestRefresh()
  }

  const handleCopy = async (copyId: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopiedId(copyId)

      if (copyResetRef.current !== null) {
        window.clearTimeout(copyResetRef.current)
      }

      copyResetRef.current = window.setTimeout(() => {
        setCopiedId(null)
      }, 1600)
    } catch {
      setCopiedId(null)
    }
  }

  if (loading && !data) {
    return (
      <section className="space-y-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-2">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Config</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Redacted {sourceLabel} config/source snapshot from <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/config</code>.
            </p>
          </div>
        </div>
        <ConfigSkeleton />
      </section>
    )
  }

  const contentValue = typeof data?.content === 'string' ? data.content : JSON.stringify(data?.content, null, 2)

  return (
    <section className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Config</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Redacted {sourceLabel} config/source snapshot from <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/config</code>.
          </p>
        </div>

        <div className="text-sm text-muted-foreground">
          Endpoint: <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">/api/v1/config{selectedSourceId !== 'opencode' ? `?source=${selectedSourceId}` : ''}</code>
        </div>
      </div>

      {error ? (
        <Alert tone="danger" className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div className="font-medium text-foreground">Config failed to load</div>
            <div className="text-sm opacity-90">{error}</div>
          </div>
          <Button variant="ghost" onClick={handleRetry}>
            Retry
          </Button>
        </Alert>
      ) : null}

      {summary ? (
        <>
          <ConfigSummaryMetrics data={data} summary={summary} />

          {data?.redacted ? (
            <Alert tone="warning">
              Sensitive values in this {sourceLabel} snapshot were redacted before display.
            </Alert>
          ) : null}

          <ConfigWorkspaceHeader
            data={data}
            searchValue={searchValue}
            searchQuery={searchQuery}
            visibleSectionCount={visibleSectionCount}
            totalSectionCount={totalSectionCount}
            copiedId={copiedId}
            onCopy={handleCopy}
            onSearchChange={setSearchValue}
            onClearFilter={() => setSearchValue('')}
          />

          {!data?.exists ? (
            <ConfigStateCard description="Empty state" title="No config file found">
              <p>
                The route checked the resolved {sourceLabel} source/config path and found no file to inspect.
              </p>
              <p>
                {selectedSourceId === 'claude_code'
                  ? 'Once Claude Code config or transcript-derived source metadata is available, the payload will be organized into focused section tabs automatically.'
                  : <>Once <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">opencode.json</code> exists, the payload will be organized into focused section tabs automatically.</>}
              </p>
            </ConfigStateCard>
          ) : summary.parseError ? (
            <ConfigStateCard
              description="Fallback state"
              title="Structured parsing failed"
              actions={
                data.content ? (
                  <CopyButton copyId="parse-error-raw" copiedId={copiedId} label="Copy JSON" value={contentValue} onCopy={handleCopy} />
                ) : null
              }
            >
              <p>
                The config endpoint returned a payload, but the browser could not re-parse it. The raw redacted snapshot is still available below.
              </p>
              <Alert tone="warning">{summary.parseError}</Alert>
              <div className="max-h-[32rem] overflow-auto rounded-xl border border-border/70 bg-background/40 p-3 font-mono text-xs leading-6 text-foreground">
                <pre className="whitespace-pre-wrap break-words">{contentValue}</pre>
              </div>
            </ConfigStateCard>
          ) : summary.emptyObject ? (
            <ConfigStateCard description="Empty state" title="Config file is present but has no inspectable keys">
              <p>
                The file exists, but the redacted JSON payload is effectively empty. This usually means the file is blank, intentionally minimal, or all meaningful content was removed before aggregation.
              </p>
            </ConfigStateCard>
          ) : (
            <Tabs value={effectiveActiveTab} onValueChange={handleActiveTabChange} className="space-y-4">
              <div className="overflow-x-auto">
                <TabsList aria-label="Configuration sections" className="min-w-max">
                  <TabsTrigger value="overview">Overview</TabsTrigger>
                  {summary.sections.map((section) => (
                    <TabsTrigger key={section.key} value={section.key}>
                      {titleizeKey(section.key)}
                    </TabsTrigger>
                  ))}
                  <TabsTrigger value="raw">Raw JSON</TabsTrigger>
                </TabsList>
              </div>

              <TabsContent value="overview">
                <ConfigOverviewTab
                  sectionProjections={sectionProjections}
                  searchQuery={searchQuery}
                  onOpenSection={handleActiveTabChange}
                />
              </TabsContent>

              {sectionProjections.map((projection) => (
                <TabsContent key={projection.section.key} value={projection.section.key}>
                  {projection.filteredValue ? (
                    <ConfigSectionPanel
                      section={projection.section}
                      visibleValue={projection.filteredValue}
                      searchActive={Boolean(searchQuery)}
                      copiedId={copiedId}
                      onCopy={handleCopy}
                    />
                  ) : (
                    <ConfigStateCard
                      description="Focused section"
                      title={titleizeKey(projection.section.key)}
                      actions={
                        <>
                          <Button type="button" variant="outline" onClick={() => setSearchValue('')}>
                            Clear filter
                          </Button>
                          <CopyButton
                            copyId={`${projection.section.key}-copy-empty`}
                            copiedId={copiedId}
                            label="Copy section"
                            value={serializeConfigValue(projection.section.value)}
                            onCopy={handleCopy}
                          />
                        </>
                      }
                    >
                      <Alert tone="warning">
                        No keys or values in <span className="font-medium text-foreground">{projection.section.key}</span> match the active filter.
                      </Alert>
                    </ConfigStateCard>
                  )}
                </TabsContent>
              ))}

              <TabsContent value="raw">
                <ConfigRawJsonPanel content={contentValue} copiedId={copiedId} onCopy={handleCopy} />
              </TabsContent>
            </Tabs>
          )}
        </>
      ) : null}
    </section>
  )
}

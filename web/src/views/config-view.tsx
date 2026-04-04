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
import type { ConfigStats } from '../types/api'

export function ConfigView() {
  const { refreshNonce, requestRefresh, setLastUpdatedAt, setRefreshing } = useDashboardContext()
  const [data, setData] = useState<ConfigStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [searchValue, setSearchValue] = useState('')
  const [activeTab, setActiveTab] = useState('overview')
  const [copiedId, setCopiedId] = useState<string | null>(null)
  const hasLoadedOnceRef = useRef(false)
  const hasChosenInitialTabRef = useRef(false)
  const copyResetRef = useRef<number | null>(null)

  useEffect(() => {
    return () => {
      if (copyResetRef.current !== null) {
        window.clearTimeout(copyResetRef.current)
      }
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    let ignore = false

    async function loadConfig() {
      setRefreshing(true)
      setError(null)

      if (!hasLoadedOnceRef.current) {
        setLoading(true)
      }

      try {
        const next = await getConfig(controller.signal)

        if (ignore) {
          return
        }

        setData(next)
        hasLoadedOnceRef.current = true
        setLastUpdatedAt(new Date())
      } catch (caught) {
        if (controller.signal.aborted || ignore) {
          return
        }

        setError(caught instanceof Error ? caught.message : 'Failed to load config')
      } finally {
        if (!controller.signal.aborted && !ignore) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }

    void loadConfig()

    return () => {
      ignore = true
      controller.abort()
    }
  }, [refreshNonce, setLastUpdatedAt, setRefreshing])

  const searchQuery = useMemo(() => normalizeSearchQuery(searchValue), [searchValue])
  const summary = useMemo(() => buildConfigSummary(data), [data])
  const sectionProjections = useMemo(() => buildSectionProjections(summary, searchQuery), [searchQuery, summary])

  useEffect(() => {
    if (!summary) {
      return
    }

    const availableTabs = new Set(['overview', 'raw', ...summary.sections.map((section) => section.key)])

    if (!hasChosenInitialTabRef.current && summary.sections.length > 0) {
      setActiveTab(summary.sections[0].key)
      hasChosenInitialTabRef.current = true
      return
    }

    if (!availableTabs.has(activeTab)) {
      setActiveTab(summary.sections[0]?.key ?? 'overview')
    }
  }, [activeTab, summary])

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
      <section className="space-y-4">
        <div className="flex flex-col gap-2 lg:flex-row lg:items-end lg:justify-between">
          <div className="space-y-1">
            <Badge tone="accent">Live route</Badge>
            <h2 className="text-2xl font-semibold tracking-tight text-foreground">Config</h2>
            <p className="max-w-3xl text-sm text-muted-foreground">
              Focused inspection of <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/config</code>, reorganized into a section-first explorer instead of a scattered JSON wall.
            </p>
          </div>
        </div>
        <ConfigSkeleton />
      </section>
    )
  }

  return (
    <section className="space-y-4">
      <div className="flex flex-col gap-2 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-1">
          <Badge tone="accent">Live route</Badge>
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">Config</h2>
          <p className="max-w-3xl text-sm text-muted-foreground">
            Browse the redacted OpenCode config snapshot through focused sections, collapsible nested groups, and search that keeps the JSON structure readable instead of dumping everything at once.
          </p>
        </div>

        <div className="text-sm text-muted-foreground">
          Endpoint: <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">/api/v1/config</code>
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
                Nothing is broken. The route checked the resolved XDG config path and found no file to inspect, so the UI stays honest about where it looked.
              </p>
              <p>
                Once <code className="rounded bg-white/6 px-1.5 py-0.5 font-mono text-xs">opencode.json</code> exists, this workspace will organize the redacted payload into focused section tabs automatically.
              </p>
            </ConfigStateCard>
          ) : summary.parseError ? (
            <ConfigStateCard
              description="Fallback state"
              title="Structured parsing failed"
              actions={
                data.content ? (
                  <CopyButton copyId="parse-error-raw" copiedId={copiedId} label="Copy JSON" value={data.content} onCopy={handleCopy} />
                ) : null
              }
            >
              <p>
                The config endpoint returned a payload, but the browser could not re-parse it into the explorer. The raw redacted snapshot is still available below so inspection does not stop.
              </p>
              <Alert tone="warning">{summary.parseError}</Alert>
              <div className="max-h-[32rem] overflow-auto rounded-xl border border-border/70 bg-background/40 p-3 font-mono text-xs leading-6 text-foreground">
                <pre className="whitespace-pre-wrap break-words">{data.content}</pre>
              </div>
            </ConfigStateCard>
          ) : summary.emptyObject ? (
            <ConfigStateCard description="Empty state" title="Config file is present but has no inspectable keys">
              <p>
                The file exists, but the redacted JSON payload is effectively empty. That usually means the file is blank, intentionally minimal, or all meaningful content was removed before aggregation.
              </p>
            </ConfigStateCard>
          ) : (
            <Tabs value={activeTab} onValueChange={setActiveTab} className="space-y-3">
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
                  onOpenSection={setActiveTab}
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
                <ConfigRawJsonPanel content={data?.content} copiedId={copiedId} onCopy={handleCopy} />
              </TabsContent>
            </Tabs>
          )}
        </>
      ) : null}
    </section>
  )
}

import { useEffect, useMemo, useState } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { ConfigView } from './views/config-view'
import { DashboardLayout } from './components/layout/dashboard-layout'
import { DashboardProvider } from './components/layout/dashboard-provider'
import { DailyView } from './views/daily-view'
import { ModelsView } from './views/models-view'
import { OverviewView } from './views/overview-view'
import { ProjectsView } from './views/projects-view'
import { SessionsView } from './views/sessions-view'
import { ToolsView } from './views/tools-view'
import { getSources } from './lib/api'
import { useSourceState } from './lib/use-source-state'
import type { SourceListResponse } from './types/api'

function fallbackSourceMetadata(reason: string): SourceListResponse {
  return {
    default_source_id: 'opencode',
    startup_source_id: 'opencode',
    sources: [
      {
        id: 'opencode',
        label: 'OpenCode',
        kind: 'sqlite',
        available: true,
        default: true,
        selected: true,
        read_only: true,
        local_only: true,
        capabilities: [],
        diagnostics: {
          status: 'metadata-fallback',
          reason,
        },
      },
    ],
  }
}

function App() {
  const [lastUpdatedAt, setLastUpdatedAt] = useState<Date | null>(null)
  const [isRefreshing, setRefreshing] = useState(false)
  const [refreshTick, setRefreshTick] = useState(0)
  const [sourceMetadata, setSourceMetadata] = useState<SourceListResponse | null>(null)
  const [sourceMetadataLoading, setSourceMetadataLoading] = useState(true)
  const [sourceMetadataError, setSourceMetadataError] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()

    async function loadSources() {
      setSourceMetadataLoading(true)
      setSourceMetadataError(null)

      try {
        const next = await getSources(controller.signal)
        if (controller.signal.aborted) return
        setSourceMetadata(next)
      } catch (caught) {
        if (controller.signal.aborted) return
        const message = caught instanceof Error ? caught.message : 'Failed to load source metadata'
        setSourceMetadataError(message)
        setSourceMetadata(fallbackSourceMetadata(message))
      } finally {
        if (!controller.signal.aborted) {
          setSourceMetadataLoading(false)
        }
      }
    }

    void loadSources()

    return () => controller.abort()
  }, [])

  const sourceState = useSourceState(sourceMetadata)

  const contextValue = useMemo(
    () => ({
      lastUpdatedAt,
      isRefreshing,
      refreshNonce: refreshTick,
      requestRefresh: () => setRefreshTick((value) => value + 1),
      setLastUpdatedAt,
      setRefreshing,
      sourceMetadata,
      sourceMetadataLoading,
      sourceMetadataError,
      sources: sourceMetadata?.sources ?? [],
      ...sourceState,
    }),
    [isRefreshing, lastUpdatedAt, refreshTick, sourceMetadata, sourceMetadataError, sourceMetadataLoading, sourceState],
  )

  return (
    <DashboardProvider value={contextValue}>
      <Routes>
        <Route path="/" element={<DashboardLayout />}>
          <Route index element={<Navigate to="/overview" replace />} />
          <Route path="overview" element={<OverviewView />} />
          <Route path="daily" element={<DailyView />} />
          <Route path="models" element={<ModelsView />} />
          <Route path="tools" element={<ToolsView />} />
          <Route path="projects" element={<ProjectsView />} />
          <Route path="sessions" element={<SessionsView />} />
          <Route path="config" element={<ConfigView />} />
          <Route path="*" element={<Navigate to="/overview" replace />} />
        </Route>
      </Routes>
    </DashboardProvider>
  )
}

export default App

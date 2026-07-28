import { lazy, useEffect, useMemo, useState } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { DashboardLayout } from './components/layout/dashboard-layout'
import { DashboardProvider } from './components/layout/dashboard-provider'
import { getSources } from './lib/api'
import { useSourceState } from './lib/use-source-state'
import type { SourceListResponse } from './types/api'

// Each view carries its own charts, tables and formatters, and a session only
// ever opens a few of them. Loading them per route keeps the initial bundle to
// the shell instead of every screen at once; DashboardLayout renders the
// Suspense boundary, so the chrome stays put while a view arrives.
const OverviewView = lazy(() => import('./views/overview-view').then((m) => ({ default: m.OverviewView })))
const DailyView = lazy(() => import('./views/daily-view').then((m) => ({ default: m.DailyView })))
const ModelsView = lazy(() => import('./views/models-view').then((m) => ({ default: m.ModelsView })))
const ToolsView = lazy(() => import('./views/tools-view').then((m) => ({ default: m.ToolsView })))
const ProjectsView = lazy(() => import('./views/projects-view').then((m) => ({ default: m.ProjectsView })))
const SessionsView = lazy(() => import('./views/sessions-view').then((m) => ({ default: m.SessionsView })))
const ConfigView = lazy(() => import('./views/config-view').then((m) => ({ default: m.ConfigView })))

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

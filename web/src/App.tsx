import { useMemo, useState } from 'react'
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

function App() {
  const [lastUpdatedAt, setLastUpdatedAt] = useState<Date | null>(null)
  const [isRefreshing, setRefreshing] = useState(false)
  const [refreshTick, setRefreshTick] = useState(0)

  const contextValue = useMemo(
    () => ({
      lastUpdatedAt,
      isRefreshing,
      refreshNonce: refreshTick,
      requestRefresh: () => setRefreshTick((value) => value + 1),
      setLastUpdatedAt,
      setRefreshing,
    }),
    [isRefreshing, lastUpdatedAt, refreshTick],
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

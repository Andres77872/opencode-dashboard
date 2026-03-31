import { Outlet } from 'react-router-dom'
import { Header } from './header'
import { PrimaryNav } from './primary-nav'
import { Alert } from '../ui/alert'

export function DashboardLayout() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <Header />
      <PrimaryNav orientation="horizontal" />

      <div className="px-4 pb-8 sm:px-6 xl:px-8">
        <div className="mb-6">
          <Alert tone="info" className="text-info/95">
            Live web slices now cover the shell, typed client, Overview, Daily trends, Models ranking, Tools usage, Projects analysis, Sessions drill-down, and Config inspection.
          </Alert>
        </div>

        <div className="flex gap-6 xl:items-start">
          <PrimaryNav orientation="vertical" />
          <main className="min-w-0 flex-1">
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  )
}

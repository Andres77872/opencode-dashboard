import { Outlet } from 'react-router-dom'
import { Header } from './header'
import { PrimaryNav } from './primary-nav'
import { SidebarProvider } from './sidebar-context'

export function DashboardLayout() {
  return (
    <SidebarProvider>
      <div className="min-h-screen bg-background text-foreground">
        <Header />
        <div className="flex px-4 pb-8 sm:px-6 xl:px-8">
          <PrimaryNav />
          <main className="min-w-0 flex-1 mx-auto w-full max-w-7xl">
            <Outlet />
          </main>
        </div>
      </div>
    </SidebarProvider>
  )
}

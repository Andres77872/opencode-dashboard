import { Outlet } from 'react-router-dom'
import { Header } from './header'
import { PrimaryNav } from './primary-nav'
import { SidebarProvider } from './sidebar-context'

export function DashboardLayout() {
  return (
    <SidebarProvider>
      <div className="flex min-h-screen text-foreground">
        <PrimaryNav />
        <div className="flex min-w-0 flex-1 flex-col">
          <Header />
          <main className="flex-1 px-6 pb-8 pt-6 xl:px-8">
            <div className="mx-auto w-full max-w-7xl">
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </SidebarProvider>
  )
}

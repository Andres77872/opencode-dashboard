import { useEffect, useRef } from 'react'
import { Outlet } from 'react-router-dom'
import { Header } from './header'
import { FilterBar } from './filter-bar'
import { PrimaryNav } from './primary-nav'
import { SidebarProvider } from './sidebar-context'
import { SourceNotice } from '../source/source-notice'

export function DashboardLayout() {
  const stickyRef = useRef<HTMLDivElement>(null)

  // Track the combined height of the sticky header + filter bar and expose it as
  // --header-height, so sticky in-page sidebar cards offset below the whole stack.
  useEffect(() => {
    const el = stickyRef.current
    if (!el) return

    const ro = new ResizeObserver(([entry]) => {
      document.documentElement.style.setProperty('--header-height', `${entry.contentRect.height}px`)
    })
    ro.observe(el)
    document.documentElement.style.setProperty(
      '--header-height',
      `${el.getBoundingClientRect().height}px`,
    )
    return () => ro.disconnect()
  }, [])

  return (
    <SidebarProvider>
      <div className="flex min-h-screen text-foreground">
        <PrimaryNav />
        <div className="flex min-w-0 flex-1 flex-col">
          <div ref={stickyRef} className="sticky top-0 z-20">
            <Header />
            <FilterBar />
          </div>
          <main className="flex-1 px-6 pb-8 pt-6 xl:px-8">
            <div className="mx-auto w-full max-w-7xl">
              <SourceNotice />
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </SidebarProvider>
  )
}

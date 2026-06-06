import { useEffect, useRef } from 'react'
import { NavLink } from 'react-router-dom'
import { PanelLeftClose, PanelLeftOpen, X, Hexagon } from 'lucide-react'
import { navItems } from './nav-items'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'
import { useDashboardContext } from './dashboard-context'
import { useSidebar } from './sidebar-context'

export function PrimaryNav() {
  const { mobileOpen, collapsed, closeMobile, toggleCollapsed } = useSidebar()
  const { selectedSourceId, selectedSourceInfo } = useDashboardContext()
  const drawerRef = useRef<HTMLDivElement>(null)
  const sourceLabel = selectedSourceInfo?.label ?? (selectedSourceId === 'claude_code' ? 'Claude Code' : 'OpenCode')

  // Close mobile drawer on Escape
  useEffect(() => {
    if (!mobileOpen) return
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeMobile()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [mobileOpen, closeMobile])

  // Prevent body scroll when mobile drawer is open
  useEffect(() => {
    if (mobileOpen) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => { document.body.style.overflow = '' }
  }, [mobileOpen])

  return (
    <>
      {/* Mobile backdrop */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/60 xl:hidden"
          onClick={closeMobile}
          aria-hidden="true"
        />
      )}

      {/* Mobile drawer — slide from left */}
      <nav
        ref={drawerRef}
        aria-label="Primary"
        className={cn(
          'fixed left-0 top-0 z-40 flex h-full w-72 flex-col border-r border-sidebar-border bg-sidebar shadow-2xl transition-transform duration-300 ease-in-out xl:hidden',
          mobileOpen ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="flex h-14 items-center justify-between border-b border-sidebar-border px-4">
          <div className="flex items-center gap-2.5">
            <Hexagon className="size-5 text-accent" />
            <span className="text-sm font-semibold text-sidebar-foreground">{sourceLabel}</span>
          </div>
          <Button variant="ghost" size="icon-sm" onClick={closeMobile} aria-label="Close navigation">
            <X className="size-4" />
          </Button>
        </div>
        <div className="flex-1 overflow-y-auto px-3 py-4">
          <div className="flex flex-col gap-1">
            {navItems.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.href}
                  to={item.href}
                  onClick={closeMobile}
                  className={({ isActive }) =>
                    cn(
                      'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                      isActive
                        ? 'bg-sidebar-accent text-sidebar-accent-foreground'
                        : 'text-sidebar-foreground/60 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground',
                    )
                  }
                >
                  <Icon className="size-4 shrink-0" />
                  <span>{item.label}</span>
                </NavLink>
              )
            })}
          </div>
        </div>
      </nav>

      {/* Desktop sidebar — structural full-height rail with collapse */}
      <nav
        aria-label="Primary"
        className={cn(
          'hidden xl:flex xl:flex-col xl:sticky xl:top-0 xl:h-screen xl:shrink-0 xl:border-r xl:border-sidebar-border xl:bg-sidebar',
          'transition-all duration-300 ease-in-out',
          collapsed ? 'xl:w-16' : 'xl:w-64',
        )}
      >
        {/* Brand area */}
        <div
          className={cn(
            'flex h-14 shrink-0 items-center border-b border-sidebar-border',
            collapsed ? 'justify-center px-0' : 'gap-2.5 px-4',
          )}
        >
          <Hexagon className="size-5 shrink-0 text-accent" />
          {!collapsed && (
            <span className="text-sm font-semibold text-sidebar-foreground">{sourceLabel}</span>
          )}
        </div>

        {/* Nav items */}
        <div className="flex-1 overflow-y-auto px-3 py-4">
          <div className="flex flex-col gap-1">
            {navItems.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.href}
                  to={item.href}
                  className={({ isActive }) =>
                    cn(
                      'flex items-center gap-3 rounded-lg text-sm font-medium transition-colors',
                      collapsed ? 'justify-center px-0 py-2' : 'px-3 py-2',
                      isActive
                        ? 'bg-sidebar-accent text-sidebar-accent-foreground'
                        : 'text-sidebar-foreground/60 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground',
                    )
                  }
                >
                  <Icon className="size-4 shrink-0" />
                  {!collapsed && <span>{item.label}</span>}
                </NavLink>
              )
            })}
          </div>
        </div>

        {/* Collapse toggle */}
        <div className="shrink-0 border-t border-sidebar-border px-3 py-3">
          <button
            type="button"
            onClick={toggleCollapsed}
            className={cn(
              'flex w-full items-center gap-2 rounded-lg px-3 py-2 text-xs text-sidebar-foreground/50 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground',
              collapsed && 'justify-center px-0',
            )}
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          >
            {collapsed ? (
              <PanelLeftOpen className="size-4" />
            ) : (
              <>
                <PanelLeftClose className="size-4" />
                <span>Collapse</span>
              </>
            )}
          </button>
        </div>
      </nav>
    </>
  )
}

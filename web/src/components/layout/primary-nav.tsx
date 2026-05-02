import { useEffect, useRef } from 'react'
import { NavLink } from 'react-router-dom'
import { PanelLeftClose, PanelLeftOpen, X } from 'lucide-react'
import { navItems } from './nav-items'
import { Badge } from '../ui/badge'
import { Button } from '../ui/button'
import { cn } from '../../lib/utils'
import { useSidebar } from './sidebar-context'

export function PrimaryNav() {
  const { mobileOpen, collapsed, closeMobile, toggleCollapsed } = useSidebar()
  const drawerRef = useRef<HTMLDivElement>(null)

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
          className="fixed inset-0 z-30 bg-black/50 xl:hidden"
          onClick={closeMobile}
          aria-hidden="true"
        />
      )}

      {/* Mobile drawer — slide from left */}
      <nav
        ref={drawerRef}
        aria-label="Primary"
        className={cn(
          'fixed left-0 top-0 z-40 flex h-full w-72 flex-col border-r border-border/70 bg-background shadow-2xl transition-transform duration-300 ease-in-out xl:hidden',
          mobileOpen ? 'translate-x-0' : '-translate-x-full',
        )}
      >
        <div className="flex items-center justify-between border-b border-border/70 px-4 py-4">
          <span className="text-sm font-semibold text-foreground">Navigation</span>
          <Button variant="ghost" size="icon-sm" onClick={closeMobile} aria-label="Close navigation">
            <X className="size-4" />
          </Button>
        </div>
        <div className="flex-1 overflow-y-auto px-3 py-4">
          <div className="flex flex-col gap-1.5">
            {navItems.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.href}
                  to={item.href}
                  onClick={closeMobile}
                  className={({ isActive }) =>
                    cn(
                      'flex items-center gap-3 rounded-xl border px-3 py-2.5 text-sm font-medium transition-colors',
                      isActive
                        ? 'border-accent/45 bg-accent/12 text-foreground'
                        : 'border-transparent bg-transparent text-muted-foreground hover:border-border/70 hover:bg-white/4 hover:text-foreground',
                    )
                  }
                >
                  {({ isActive }) => (
                    <>
                      <Icon className="size-4 shrink-0" />
                      <span className="flex-1">{item.label}</span>
                      <Badge tone={isActive ? 'accent' : 'default'}>Live</Badge>
                    </>
                  )}
                </NavLink>
              )
            })}
          </div>
        </div>
      </nav>

      {/* Desktop sidebar — collapsible rail */}
      <nav
        aria-label="Primary"
        className={cn(
          'hidden transition-all duration-300 ease-in-out xl:block xl:flex-none',
          collapsed ? 'xl:w-16' : 'xl:w-64',
        )}
      >
        <div
          className={cn(
            'sticky flex flex-col gap-2 rounded-2xl border border-border/70 bg-panel/75 p-3',
            'top-[var(--header-height)]',
          )}
          style={{ marginTop: '0' }}
        >
          {navItems.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.href}
                to={item.href}
                className={({ isActive }) =>
                  cn(
                    'group flex items-center justify-between gap-3 rounded-xl border px-3 py-2 text-sm transition-colors w-full',
                    isActive
                      ? 'border-accent/45 bg-accent/12 text-foreground'
                      : 'border-transparent bg-transparent text-muted-foreground hover:border-border/70 hover:bg-white/4 hover:text-foreground',
                    collapsed && 'justify-center px-0',
                  )
                }
              >
                {({ isActive }) => (
                  <>
                    <Icon className="size-4 shrink-0" />
                    {!collapsed && (
                      <>
                        <span className="flex-1 font-medium">{item.label}</span>
                        <Badge tone={isActive ? 'accent' : 'default'}>Live</Badge>
                      </>
                    )}
                  </>
                )}
              </NavLink>
            )
          })}

          {/* Collapse toggle */}
          <div className="mt-1 border-t border-border/40 pt-2">
            <button
              type="button"
              onClick={toggleCollapsed}
              className="flex w-full items-center justify-center gap-2 rounded-xl px-3 py-2 text-xs text-muted-foreground transition-colors hover:bg-white/4 hover:text-foreground"
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
        </div>
      </nav>
    </>
  )
}

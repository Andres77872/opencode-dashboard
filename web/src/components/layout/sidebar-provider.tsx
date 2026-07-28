import { useState, useCallback, useMemo, type ReactNode } from 'react'

import { SidebarContext } from './sidebar-context'

export function SidebarProvider({ children }: { children: ReactNode }) {
  const [mobileOpen, setMobileOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(false)

  const toggleMobile = useCallback(() => setMobileOpen((v) => !v), [])
  const closeMobile = useCallback(() => setMobileOpen(false), [])
  const toggleCollapsed = useCallback(() => setCollapsed((v) => !v), [])

  // Memoized so consumers of useSidebar do not re-render on every render of
  // whatever mounts the provider.
  const value = useMemo(
    () => ({ mobileOpen, collapsed, toggleMobile, closeMobile, toggleCollapsed }),
    [mobileOpen, collapsed, toggleMobile, closeMobile, toggleCollapsed],
  )

  return <SidebarContext.Provider value={value}>{children}</SidebarContext.Provider>
}

import { useState, useCallback, useMemo, type ReactNode } from 'react'

import { SidebarContext } from './sidebar-context'

export function SidebarProvider({ children }: { children: ReactNode }) {
  const [mobileOpen, setMobileOpen] = useState(false)

  const toggleMobile = useCallback(() => setMobileOpen((v) => !v), [])
  const closeMobile = useCallback(() => setMobileOpen(false), [])

  // Memoized so consumers of useSidebar do not re-render on every render of
  // whatever mounts the provider.
  const value = useMemo(() => ({ mobileOpen, toggleMobile, closeMobile }), [mobileOpen, toggleMobile, closeMobile])

  return <SidebarContext.Provider value={value}>{children}</SidebarContext.Provider>
}

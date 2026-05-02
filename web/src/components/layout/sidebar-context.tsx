import { createContext, useContext, useState, useCallback, type ReactNode } from 'react'

interface SidebarContextValue {
  /** Mobile drawer open state */
  mobileOpen: boolean
  /** Desktop collapsed rail state */
  collapsed: boolean
  toggleMobile: () => void
  closeMobile: () => void
  toggleCollapsed: () => void
}

const SidebarContext = createContext<SidebarContextValue | null>(null)

export function SidebarProvider({ children }: { children: ReactNode }) {
  const [mobileOpen, setMobileOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(false)

  const toggleMobile = useCallback(() => setMobileOpen((v) => !v), [])
  const closeMobile = useCallback(() => setMobileOpen(false), [])
  const toggleCollapsed = useCallback(() => setCollapsed((v) => !v), [])

  return (
    <SidebarContext.Provider value={{ mobileOpen, collapsed, toggleMobile, closeMobile, toggleCollapsed }}>
      {children}
    </SidebarContext.Provider>
  )
}

export function useSidebar() {
  const ctx = useContext(SidebarContext)
  if (!ctx) throw new Error('useSidebar must be used within SidebarProvider')
  return ctx
}

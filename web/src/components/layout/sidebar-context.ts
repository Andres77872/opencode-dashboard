import { createContext, useContext } from 'react'

/** Ties the topbar's menu button to the drawer it controls via aria-controls. */
export const NAV_DRAWER_ID = 'vael-nav-drawer'

export interface SidebarContextValue {
  /** Mobile drawer open state */
  mobileOpen: boolean
  /** Desktop collapsed rail state */
  collapsed: boolean
  toggleMobile: () => void
  closeMobile: () => void
  toggleCollapsed: () => void
}

/** Shared by SidebarProvider (which owns the state) and useSidebar. It lives
    apart from the provider component so neither file mixes component and
    non-component exports, which would break Fast Refresh. */
export const SidebarContext = createContext<SidebarContextValue | null>(null)

export function useSidebar(): SidebarContextValue {
  const ctx = useContext(SidebarContext)
  if (!ctx) throw new Error('useSidebar must be used within SidebarProvider')
  return ctx
}

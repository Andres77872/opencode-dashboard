import { createContext, useContext } from 'react'

export interface DashboardContextValue {
  lastUpdatedAt: Date | null
  isRefreshing: boolean
  refreshNonce: number
  requestRefresh: () => void
  setRefreshing: (value: boolean) => void
  setLastUpdatedAt: (value: Date | null) => void
}

export const DashboardContext = createContext<DashboardContextValue | null>(null)

export function useDashboardContext() {
  const context = useContext(DashboardContext)

  if (!context) {
    throw new Error('useDashboardContext must be used within DashboardProvider')
  }

  return context
}

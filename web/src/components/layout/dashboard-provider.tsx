import type { ReactNode } from 'react'
import { DashboardContext } from './dashboard-context'
import type { DashboardContextValue } from './dashboard-context'

export function DashboardProvider({
  value,
  children,
}: {
  value: DashboardContextValue
  children: ReactNode
}) {
  return <DashboardContext.Provider value={value}>{children}</DashboardContext.Provider>
}

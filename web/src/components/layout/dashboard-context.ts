import { createContext, useContext } from 'react'
import type { SourceID, SourceInfo, SourceListResponse } from '../../types/api'
import type { SourceStateError } from '../../lib/use-source-state'

export interface DashboardContextValue {
  lastUpdatedAt: Date | null
  isRefreshing: boolean
  refreshNonce: number
  requestRefresh: () => void
  /**
   * Registers an in-flight data request with the global "refreshing"
   * indicator and returns its release callback (safe to call more than once).
   * The indicator stays on until every registered request has released.
   */
  trackRefresh: () => () => void
  setLastUpdatedAt: (value: Date | null) => void
  updateSourceMetadata?: (metadata: SourceListResponse) => void
  sourceMetadata: SourceListResponse | null
  sourceMetadataLoading: boolean
  sourceMetadataError: string | null
  sources: SourceInfo[]
  selectedSourceId: SourceID
  selectedSourceInfo: SourceInfo | null
  sourceAvailable: boolean
  sourceStateError: SourceStateError | null
  setSelectedSourceId: (sourceId: SourceID) => void
}

export const DashboardContext = createContext<DashboardContextValue | null>(null)

export function useDashboardContext() {
  const context = useContext(DashboardContext)

  if (!context) {
    throw new Error('useDashboardContext must be used within DashboardProvider')
  }

  return context
}

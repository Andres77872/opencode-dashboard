import type { ConfigFormat } from './api'

export type ConfigJsonPrimitive = string | number | boolean | null

export type ConfigViewMode = 'tree' | 'source'

/** Format-aware projection of a ConfigStats payload for rendering. */
export interface ConfigDocument {
  format: ConfigFormat
  /** Redacted file text; synthesized from content on legacy payloads. */
  raw: string
  /** True when raw was synthesized client-side rather than shipped by the API. */
  rawSynthesized: boolean
  parseError: string | null
  hasStructured: boolean
}

export type ConfigJsonValue = ConfigJsonPrimitive | ConfigJsonObject | ConfigJsonValue[]

export interface ConfigJsonObject {
  [key: string]: ConfigJsonValue
}

export interface ConfigSection {
  key: string
  value: ConfigJsonValue
}

export interface ConfigInsights {
  leafValues: number
  redactedValues: number
  collections: number
}

export interface ParsedConfigState {
  parsed: ConfigJsonValue | null
  parseError: string | null
}

export interface ConfigSummary {
  sections: ConfigSection[]
  insights: ConfigInsights
  parseError: string | null
  emptyObject: boolean
}

export interface ConfigSectionProjection {
  section: ConfigSection
  filteredValue: ConfigJsonValue | null
  insights: ConfigInsights
  filteredInsights: ConfigInsights | null
}

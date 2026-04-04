export type ConfigJsonPrimitive = string | number | boolean | null

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

import { formatInteger } from './format'
import type { ConfigStats } from '../types/api'
import type {
  ConfigInsights,
  ConfigJsonObject,
  ConfigJsonPrimitive,
  ConfigJsonValue,
  ConfigSection,
  ConfigSectionProjection,
  ConfigSummary,
  ParsedConfigState,
} from '../types/config'

export const REDACTED_VALUE = '[REDACTED]'

export const EMPTY_CONFIG_INSIGHTS: ConfigInsights = {
  leafValues: 0,
  redactedValues: 0,
  collections: 0,
}

export function isObject(value: ConfigJsonValue): value is ConfigJsonObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function isPrimitive(value: ConfigJsonValue): value is ConfigJsonPrimitive {
  return !isObject(value) && !Array.isArray(value)
}

export function isRedactedValue(value: ConfigJsonValue) {
  return typeof value === 'string' && value === REDACTED_VALUE
}

export function titleizeKey(key: string) {
  if (!key) {
    return 'Root'
  }

  return key
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/[_.-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
}

export function formatDisplayLabel(label: string) {
  return /^\[\d+\]$/.test(label) ? label : titleizeKey(label)
}

export function formatPrimitiveValue(value: ConfigJsonPrimitive) {
  if (value === null) {
    return 'null'
  }

  if (typeof value === 'boolean') {
    return value ? 'true' : 'false'
  }

  return String(value)
}

export function formatPrimitiveType(value: ConfigJsonPrimitive) {
  if (value === null) {
    return 'Null'
  }

  if (typeof value === 'boolean') {
    return 'Boolean'
  }

  if (typeof value === 'number') {
    return 'Number'
  }

  return 'String'
}

export function summarizeValue(value: ConfigJsonValue) {
  if (Array.isArray(value)) {
    return `${formatInteger(value.length)} item${value.length === 1 ? '' : 's'}`
  }

  if (isObject(value)) {
    const count = Object.keys(value).length
    return `${formatInteger(count)} key${count === 1 ? '' : 's'}`
  }

  if (value === null) {
    return 'Null'
  }

  return typeof value
}

export function collectInsights(value: ConfigJsonValue): ConfigInsights {
  if (isPrimitive(value)) {
    return {
      leafValues: 1,
      redactedValues: isRedactedValue(value) ? 1 : 0,
      collections: 0,
    }
  }

  if (Array.isArray(value)) {
    return value.reduce<ConfigInsights>(
      (accumulator, item) => {
        const next = collectInsights(item)

        return {
          leafValues: accumulator.leafValues + next.leafValues,
          redactedValues: accumulator.redactedValues + next.redactedValues,
          collections: accumulator.collections + next.collections,
        }
      },
      { leafValues: 0, redactedValues: 0, collections: 1 },
    )
  }

  return Object.values(value).reduce<ConfigInsights>(
    (accumulator, item) => {
      const next = collectInsights(item)

      return {
        leafValues: accumulator.leafValues + next.leafValues,
        redactedValues: accumulator.redactedValues + next.redactedValues,
        collections: accumulator.collections + next.collections,
      }
    },
    { leafValues: 0, redactedValues: 0, collections: 1 },
  )
}

export function parseConfigContent(content?: string): ParsedConfigState {
  if (!content) {
    return {
      parsed: null,
      parseError: 'The API did not return a config payload to inspect.',
    }
  }

  try {
    return {
      parsed: JSON.parse(content) as ConfigJsonValue,
      parseError: null,
    }
  } catch (error) {
    return {
      parsed: null,
      parseError: error instanceof Error ? error.message : 'Failed to parse config JSON',
    }
  }
}

export function getSections(value: ConfigJsonValue | null): ConfigSection[] {
  if (!value) {
    return []
  }

  if (isObject(value)) {
    const entries = Object.entries(value)
    if (entries.length > 0) {
      return entries.map(([key, sectionValue]) => ({ key, value: sectionValue }))
    }
  }

  return [{ key: 'root', value }]
}

export function normalizeSearchQuery(query: string) {
  return query.trim().toLowerCase()
}

export function filterConfigValue(label: string, value: ConfigJsonValue, searchQuery: string): ConfigJsonValue | null {
  if (!searchQuery) {
    return value
  }

  if (label.toLowerCase().includes(searchQuery)) {
    return value
  }

  if (isPrimitive(value)) {
    return formatPrimitiveValue(value).toLowerCase().includes(searchQuery) ? value : null
  }

  if (Array.isArray(value)) {
    const filteredItems = value.flatMap((item, index) => {
      const filtered = filterConfigValue(`[${index}]`, item, searchQuery)
      return filtered === null ? [] : [filtered]
    })

    return filteredItems.length > 0 ? filteredItems : null
  }

  const filteredEntries = Object.entries(value).flatMap(([key, item]) => {
    if (key.toLowerCase().includes(searchQuery)) {
      return [[key, item] as [string, ConfigJsonValue]]
    }

    const filtered = filterConfigValue(key, item, searchQuery)
    return filtered === null ? [] : [[key, filtered] as [string, ConfigJsonValue]]
  })

  return filteredEntries.length > 0 ? Object.fromEntries(filteredEntries) : null
}

export function serializeConfigValue(value: ConfigJsonValue) {
  return isPrimitive(value) ? formatPrimitiveValue(value) : JSON.stringify(value, null, 2)
}

export function buildConfigSummary(data: ConfigStats | null): ConfigSummary | null {
  if (!data) {
    return null
  }

  const parsedState = parseConfigContent(data.content)
  const sections = getSections(parsedState.parsed)

  return {
    sections,
    insights: parsedState.parsed ? collectInsights(parsedState.parsed) : EMPTY_CONFIG_INSIGHTS,
    parseError: parsedState.parseError,
    emptyObject: data.exists && sections.length === 0,
  }
}

export function buildSectionProjections(summary: ConfigSummary | null, searchQuery: string): ConfigSectionProjection[] {
  if (!summary) {
    return []
  }

  return summary.sections.map((section) => {
    const filteredValue = filterConfigValue(section.key, section.value, searchQuery)

    return {
      section,
      filteredValue,
      insights: collectInsights(section.value),
      filteredInsights: filteredValue ? collectInsights(filteredValue) : null,
    }
  })
}

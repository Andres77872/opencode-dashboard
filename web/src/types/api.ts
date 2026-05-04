export interface CacheStats {
  read: number
  write: number
}

export interface TokenStats {
  input: number
  output: number
  reasoning: number
  cache: CacheStats
}

export const DAILY_PERIOD_VALUES = ['1h', '6h', '12h', '24h', '72h', '1d', '7d', '14d', '30d', '1y', 'all'] as const

export type DailyPeriod = (typeof DAILY_PERIOD_VALUES)[number]

export type Granularity = 'day' | 'hour'

export type PeriodMode = 'preset' | 'custom'

export interface CustomPeriod {
  from: string // ISO 8601 "YYYY-MM-DD"
  to?: string // ISO 8601 "YYYY-MM-DD", optional — defaults to now
}

export function isDailyPeriod(value: string | null): value is DailyPeriod {
  return value !== null && DAILY_PERIOD_VALUES.includes(value as DailyPeriod)
}

// Validates that a string is a valid YYYY-MM-DD date.
// Uses regex + roundtrip to catch rollover dates (e.g., "2026-02-31" → "2026-03-03").
function isValidISODate(dateStr: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(dateStr)) return false
  const parsed = new Date(dateStr + 'T00:00:00')
  if (isNaN(parsed.getTime())) return false
  // Roundtrip check: format back to YYYY-MM-DD and compare
  const y = parsed.getFullYear()
  const m = String(parsed.getMonth() + 1).padStart(2, '0')
  const d = String(parsed.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}` === dateStr
}

export function isValidCustomRange(from: string, to?: string): boolean {
  if (!isValidISODate(from)) return false

  if (to !== undefined) {
    if (!isValidISODate(to)) return false
    return new Date(from + 'T00:00:00') <= new Date(to + 'T00:00:00')
  }

  return true
}

export interface DayStats {
  date: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
}

export interface DailyStats {
  days: DayStats[]
  granularity: Granularity
}

export interface AvgTokenStats {
  input: number
  output: number
  reasoning: number
  cache_read: number
  cache_write: number
}

export interface ModelEntry {
  model_id: string
  provider_id: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
  avg_tokens_per_message?: AvgTokenStats
  avg_tokens_per_session?: AvgTokenStats
}

export interface ModelStats {
  models: ModelEntry[]
}

export interface ToolEntry {
  name: string
  invocations: number
  successes: number
  failures: number
  sessions: number
}

export interface ToolStats {
  tools: ToolEntry[]
}

export interface ProjectEntry {
  project_id: string
  project_name: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
}

export interface ProjectStats {
  projects: ProjectEntry[]
}

export interface DimensionDayStats {
  date: string
  dimension_key: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
}

export interface DailyDimensionStats {
  days: DimensionDayStats[]
  dimension: string
  period: string
}

export interface ProjectDetail {
  project_id: string
  project_name: string
  worktree?: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
  recent_sessions?: SessionEntry[]
  total_sessions: number
}

export interface ConfigStats {
  path: string
  exists: boolean
  content?: Record<string, unknown>
}

export interface SessionEntry {
  id: string
  title: string
  project_id: string
  project_name: string
  time_created: string
  time_updated: string
  message_count: number
  cost: number
}

export interface SessionList {
  sessions: SessionEntry[]
  total: number
  page: number
  page_size: number
}

export interface SessionMessage {
  id: string
  role: string
  time_created: string
  cost?: number
  tokens?: TokenStats
  model_id?: string
  provider_id?: string
  agent?: string
}

export interface SessionDetail {
  id: string
  title: string
  project_id: string
  project_name: string
  directory: string
  time_created: string
  time_updated: string
  messages: SessionMessage[]
  total_cost: number
  total_tokens: TokenStats
  message_count: number
}

export interface MessageEntry {
  id: string
  session_id: string
  session_title: string
  role: string
  time_created: string
  cost: number
  tokens?: TokenStats
  model_id?: string
  provider_id?: string
}

export interface MessageList {
  messages: MessageEntry[]
  total: number
  page: number
  page_size: number
}

export interface MessagePart {
  type: 'text' | 'reasoning'
  text: string
}

export interface ToolTime {
  start?: number
  end?: number
  compacted?: number
}

export interface ToolState {
  status: 'pending' | 'running' | 'completed' | 'error'
  input?: Record<string, unknown>
  output?: string
  title?: string
  error?: string
  metadata?: Record<string, unknown>
  time?: ToolTime
}

export interface ToolPart {
  type: 'tool'
  call_id: string
  tool: string
  state: ToolState
}

export interface MessageContent {
  text_parts: MessagePart[]
  reasoning_parts: MessagePart[]
  tool_parts: ToolPart[]
}

export interface MessageDetail extends MessageEntry {
  content: MessageContent
}

export interface OverviewStats {
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
  cost_per_day: number
  days: number
}

export interface ApiErrorResponse {
  error: string
  message: string
  code: number
}

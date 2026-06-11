export interface CacheStats {
  read: number
  write: number
}

export const SOURCE_ID_VALUES = ['opencode', 'claude_code', 'codex'] as const

export type SourceID = (typeof SOURCE_ID_VALUES)[number]

export const DEFAULT_SOURCE_ID: SourceID = 'opencode'

export function isSourceID(value: string | null): value is SourceID {
  return value !== null && SOURCE_ID_VALUES.includes(value as SourceID)
}

export interface SourceDiagnostics {
  status?: string
  reason?: string
  scanned_files?: number
  malformed_lines?: number
  unsupported_events?: number
}

export interface CostPolicy {
  status?: string
  currency?: string
  pricing_snapshot_id?: string
  note?: string
}

export interface PrivacyInfo {
  plaintext_transcripts?: boolean
  read_only?: boolean
  local_only?: boolean
  redaction?: boolean
  warnings?: string[]
}

export interface SourceInfo {
  id: SourceID
  label: string
  kind: 'sqlite' | 'jsonl' | string
  available: boolean
  default: boolean
  selected?: boolean
  path?: string
  path_source?: string
  read_only: boolean
  local_only: boolean
  capabilities: string[]
  warnings?: string[]
  diagnostics?: SourceDiagnostics
  cost_policy?: CostPolicy
  privacy?: PrivacyInfo
}

export interface SourceListResponse {
  default_source_id: SourceID
  startup_source_id?: SourceID
  sources: SourceInfo[]
}

export interface CacheSourceStatus {
  source_id: SourceID
  label: string
  available: boolean
  cached: boolean
  needs_sync: boolean
  status?: string
  reason?: string
  last_synced_ms?: number
  safe_cutoff_ms?: number
  fresh_through_ms?: number
  fill_attempt_ms?: number
  fill_error?: string
}

export interface CacheStatusResponse {
  enabled: boolean
  path?: string
  source?: string
  active: boolean
  last_updated_ms?: number
  sources?: CacheSourceStatus[]
  sync?: CacheSyncStatus
}

export interface CacheSyncStatus {
  running: boolean
  status: string
  mode?: CacheSyncMode
  target?: string
  current_source_id?: string
  total: number
  completed: number
  current_phase?: string
  items_done?: number
  items_total?: number
  safe_cutoff_ms?: number
  started_at_ms?: number
  updated_at_ms?: number
  finished_at_ms?: number
  error?: string
  logs?: CacheLogEntry[]
}

export type CacheSyncMode = 'incremental' | 'rebuild'

export interface CacheLogEntry {
  time_ms: number
  level: 'info' | 'error' | string
  source_id?: SourceID | string
  message: string
}

export type CostStatus = 'reported' | 'computed' | 'approximate' | 'estimated_api_equivalent' | 'mixed' | 'missing'

export interface CostProvenance {
  status: CostStatus
  currency?: string
  pricing_snapshot_id?: string
  pricing_source?: string
  missing_count?: number
  computed_count?: number
  reported_count?: number
  note?: string
}

export interface TruncationInfo {
  truncated?: boolean
  original_bytes?: number
  display_bytes?: number
}

export interface SourceTagged {
  source_id?: SourceID
  cost_status?: CostStatus
  cost_provenance?: CostProvenance
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
  const [y, m, d] = dateStr.split('-').map(Number)
  const parsed = new Date(Date.UTC(y, m - 1, d))
  if (isNaN(parsed.getTime())) return false
  const uy = parsed.getUTCFullYear()
  const um = String(parsed.getUTCMonth() + 1).padStart(2, '0')
  const ud = String(parsed.getUTCDate()).padStart(2, '0')
  return `${uy}-${um}-${ud}` === dateStr
}

export function isValidCustomRange(from: string, to?: string): boolean {
  if (!isValidISODate(from)) return false

  if (to !== undefined) {
    if (!isValidISODate(to)) return false
    return from <= to
  }

  return true
}

export interface DayStats extends SourceTagged {
  date: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
}

export interface DailyStats extends SourceTagged {
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

export interface ModelEntry extends SourceTagged {
  model_id: string
  provider_id: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
  avg_tokens_per_message?: AvgTokenStats
  avg_tokens_per_session?: AvgTokenStats
}

export interface ModelStats extends SourceTagged {
  models: ModelEntry[]
}

export interface ToolEntry {
  source_id?: SourceID
  name: string
  invocations: number
  successes: number
  failures: number
  sessions: number
}

export interface ToolStats {
  source_id?: SourceID
  tools: ToolEntry[]
}

export interface ProjectEntry extends SourceTagged {
  project_id: string
  project_name: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
}

export interface ProjectStats extends SourceTagged {
  projects: ProjectEntry[]
}

export interface DimensionDayStats extends SourceTagged {
  date: string
  dimension_key: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
}

export interface DailyDimensionStats extends SourceTagged {
  days: DimensionDayStats[]
  dimension: string
  period: string
}

export interface ProjectDetail extends SourceTagged {
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
  source_id?: SourceID
  path: string
  exists: boolean
  content?: Record<string, unknown>
  redacted?: boolean
}

export interface SessionEntry extends SourceTagged {
  id: string
  title: string
  project_id: string
  project_name: string
  time_created: string
  time_updated: string
  message_count: number
  cost: number
}

export interface SessionList extends SourceTagged {
  sessions: SessionEntry[]
  total: number
  page: number
  page_size: number
}

export interface SessionMessage extends SourceTagged {
  id: string
  role: string
  time_created: string
  cost?: number
  tokens?: TokenStats
  model_id?: string
  provider_id?: string
  agent?: string
  is_subagent?: boolean
}

export interface SessionDetail extends SourceTagged {
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

export interface MessageEntry extends SourceTagged {
  id: string
  session_id: string
  session_title: string
  role: string
  time_created: string
  cost: number
  tokens?: TokenStats
  model_id?: string
  provider_id?: string
  agent?: string
  is_subagent?: boolean
  folded_assistant_calls?: number
  folded_tool_calls?: number
  folded_token_updates?: number
}

export interface MessageList extends SourceTagged {
  messages: MessageEntry[]
  total: number
  page: number
  page_size: number
}

export interface MessagePart {
  type: 'text' | 'reasoning'
  text: string
  truncation?: TruncationInfo
  redacted?: boolean
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
  truncation?: TruncationInfo
  redacted?: boolean
}

export interface ToolPart {
  source_id?: SourceID
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

export interface OverviewStats extends SourceTagged {
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
  cost_per_day: number
  days: number
}

// ── All-sources Overview (cross-source aggregate) ──────────────────
// Returned by GET /api/v1/overview/all. The Overview view merges every source;
// cost is shown only per source (sources[].overview.cost), never combined.

export interface SourceOverview {
  source_id: SourceID
  label?: string
  overview: OverviewStats
  message_share: number // 0..1
  token_share: number // 0..1
  messages_per_session: number
  tokens_per_message: AvgTokenStats
  trend?: DayStats[]
}

export interface SourceLoadError {
  source_id: SourceID
  message: string
}

export interface AllSourcesOverview {
  total: OverviewStats
  sources: SourceOverview[]
  messages_per_session: number
  tokens_per_message: AvgTokenStats
  token_distribution: TokenStats
  top_models: ModelEntry[]
  top_projects: ProjectEntry[]
  top_tools: ToolEntry[]
  errors?: SourceLoadError[]
}

export interface ApiErrorResponse {
  error: string
  message: string
  code: number
}

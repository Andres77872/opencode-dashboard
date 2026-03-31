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

export type DailyPeriod = '7d' | '30d'

export interface DayStats {
  date: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
}

export interface DailyStats {
  days: DayStats[]
}

export interface ModelEntry {
  model_id: string
  provider_id: string
  sessions: number
  messages: number
  cost: number
  tokens: TokenStats
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

export interface ConfigStats {
  path: string
  exists: boolean
  content?: string
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

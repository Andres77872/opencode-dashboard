import type { DailyPeriod } from './api'

export type AssistantRole = 'user' | 'assistant'

export interface AssistantMessage {
  role: AssistantRole
  content: string
  signature?: string
}

/** One delegable specialist advertised by the backend agent roster. */
export interface AssistantSpecialist {
  id: string
  title: string
  purpose: string
}

export interface AssistantStatusResponse {
  available: boolean
  provider: string
  model: string
  reason?: string
  privacy_notice: string
  consent_version: string
  capabilities: string[]
  specialists?: AssistantSpecialist[]
  sessions_persisted: boolean
}

export interface AssistantRequestContext {
  route?: string
  source?: string
  /** A supported preset. Mutually exclusive with from/to. */
  period?: DailyPeriod
  /** Inclusive UTC calendar date for a custom range. */
  from?: string
  /** Inclusive UTC calendar date; omitted for an open-ended range through now. */
  to?: string
  timezone?: string
}

/**
 * Display-only context reconstructed from persisted chat history. The server
 * intentionally keeps one period label in storage for backward compatibility,
 * even when a request originally used structured from/to fields.
 */
export interface AssistantStoredContext {
  route?: string
  source?: string
  period?: string
  timezone?: string
}

export interface AssistantChatRequest {
  messages: AssistantMessage[]
  context?: AssistantRequestContext
  consent_version: string
  session_id?: string
}

/**
 * Provider token accounting for a turn, a session, or one specialist run. Every
 * counter is optional evidence: zero means the provider reported nothing, never
 * that the work was free.
 */
export interface AssistantUsage {
  requests: number
  input_tokens: number
  output_tokens: number
  cached_input_tokens?: number
  reasoning_tokens?: number
  total_tokens: number
}

/**
 * One analytics tool invocation in a completed turn: normalized arguments for
 * executable calls, or redacted arguments for rejected calls, plus the
 * safe result envelope the model received. Specialist calls carry their agent
 * id and parent delegation.
 */
export interface AssistantToolCall {
  call_id: string
  name: string
  agent?: string
  parent_call_id?: string
  round?: number
  arguments?: unknown
  result?: unknown
  ok: boolean
  duration_ms?: number
}

/** One delegated specialist investigation and what it cost to produce. */
export interface AssistantSubagentRun {
  call_id: string
  agent: string
  title?: string
  task?: string
  status?: string
  report?: string
  error?: string
  rounds?: number
  tools_used?: string[]
  usage?: AssistantUsage
  duration_ms?: number
}

export interface AssistantChatResponse {
  message: AssistantMessage & { role: 'assistant'; signature: string }
  model: string
  provider?: string
  agent?: string
  rounds?: number
  duration_ms?: number
  usage?: AssistantUsage
  tools_used: string[]
  tool_calls?: AssistantToolCall[]
  subagents?: AssistantSubagentRun[]
  notices?: string[]
  session_id?: string
  session_title?: string
  session_usage?: AssistantUsage
}

export interface AssistantStreamStartEvent {
  type: 'start'
  model: string
}

/** Marks the beginning of one provider round for the named agent. */
export interface AssistantStreamRoundStartEvent {
  type: 'round_start'
  agent?: string
  round: number
  parent_call_id?: string
}

export interface AssistantStreamContentDeltaEvent {
  type: 'content_delta'
  delta: string
}

export interface AssistantStreamContentResetEvent {
  type: 'content_reset'
}

export interface AssistantStreamToolStartEvent {
  type: 'tool_start'
  call_id: string
  name: string
  agent?: string
  parent_call_id?: string
  round?: number
  arguments?: unknown
}

export interface AssistantStreamToolFinishEvent {
  type: 'tool_finish'
  call_id: string
  name: string
  ok: boolean
  agent?: string
  parent_call_id?: string
  round?: number
  result?: unknown
  duration_ms?: number
}

/** Payload shared by the specialist lifecycle events. */
export interface AssistantStreamSubagentInfo {
  agent: string
  title?: string
  task?: string
  status?: string
  report?: string
  rounds?: number
  tools_used?: string[]
  usage?: AssistantUsage
  error?: string
}

export interface AssistantStreamSubagentStartEvent {
  type: 'subagent_start'
  call_id: string
  agent?: string
  subagent: AssistantStreamSubagentInfo
}

export interface AssistantStreamSubagentFinishEvent {
  type: 'subagent_finish'
  call_id: string
  ok: boolean
  agent?: string
  duration_ms?: number
  subagent: AssistantStreamSubagentInfo
}

export interface AssistantStreamCompleteEvent {
  type: 'complete'
  message: AssistantMessage & { role: 'assistant'; signature: string }
  model: string
  provider?: string
  agent?: string
  rounds?: number
  duration_ms?: number
  usage?: AssistantUsage
  tools_used: string[]
  tool_calls: AssistantToolCall[]
  subagents: AssistantSubagentRun[]
  notices?: string[]
  session_id?: string
  session_title?: string
  session_usage?: AssistantUsage
}

export interface AssistantStreamErrorEvent {
  type: 'error'
  message: string
}

export type AssistantStreamEvent =
  | AssistantStreamStartEvent
  | AssistantStreamRoundStartEvent
  | AssistantStreamContentDeltaEvent
  | AssistantStreamContentResetEvent
  | AssistantStreamToolStartEvent
  | AssistantStreamToolFinishEvent
  | AssistantStreamSubagentStartEvent
  | AssistantStreamSubagentFinishEvent
  | AssistantStreamCompleteEvent
  | AssistantStreamErrorEvent

/** A persisted assistant conversation summary from the server chat log. */
export interface AssistantChatSessionSummary {
  id: string
  title: string
  provider?: string
  model?: string
  consent_version?: string
  created_ms: number
  updated_ms: number
  message_count: number
  turn_count?: number
  tool_call_count?: number
  subagent_count?: number
  duration_ms?: number
  usage?: AssistantUsage
}

export interface AssistantChatSessionListResponse {
  sessions: AssistantChatSessionSummary[]
}

/** One persisted tool invocation stored with an assistant message. */
export interface AssistantChatStoredToolCall {
  index: number
  name: string
  call_ref?: string
  parent_call_ref?: string
  agent?: string
  round?: number
  arguments?: unknown
  result?: unknown
  ok: boolean
  duration_ms: number
}

/** One persisted specialist run stored with an assistant message. */
export interface AssistantChatStoredSubagentRun {
  index: number
  call_ref?: string
  agent: string
  title?: string
  task?: string
  status?: string
  report?: string
  error?: string
  rounds?: number
  tools_used?: string[]
  duration_ms?: number
  usage?: AssistantUsage
}

export interface AssistantChatStoredMessage {
  id: number
  role: AssistantRole
  content: string
  signature?: string
  model?: string
  agent?: string
  created_ms: number
  turn_index?: number
  rounds?: number
  duration_ms?: number
  usage?: AssistantUsage
  context?: AssistantStoredContext
  notices?: string[]
  tool_calls?: AssistantChatStoredToolCall[]
  subagents?: AssistantChatStoredSubagentRun[]
}

export interface AssistantChatSessionDetail {
  session: AssistantChatSessionSummary
  messages: AssistantChatStoredMessage[]
}

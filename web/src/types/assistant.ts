export type AssistantRole = 'user' | 'assistant'

export interface AssistantMessage {
  role: AssistantRole
  content: string
  signature?: string
}

export interface AssistantStatusResponse {
  available: boolean
  provider: string
  model: string
  reason?: string
  privacy_notice: string
  consent_version: string
  capabilities: string[]
  sessions_persisted: boolean
}

export interface AssistantRequestContext {
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
 * One analytics tool invocation in a completed turn: the validated input
 * arguments and the safe result envelope the model received.
 */
export interface AssistantToolCall {
  call_id: string
  name: string
  arguments?: unknown
  result?: unknown
  ok: boolean
  duration_ms?: number
}

export interface AssistantChatResponse {
  message: AssistantMessage & { role: 'assistant'; signature: string }
  model: string
  tools_used: string[]
  tool_calls?: AssistantToolCall[]
  session_id?: string
}

export interface AssistantStreamStartEvent {
  type: 'start'
  model: string
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
  arguments?: unknown
}

export interface AssistantStreamToolFinishEvent {
  type: 'tool_finish'
  call_id: string
  name: string
  ok: boolean
  result?: unknown
  duration_ms?: number
}

export interface AssistantStreamCompleteEvent {
  type: 'complete'
  message: AssistantMessage & { role: 'assistant'; signature: string }
  model: string
  tools_used: string[]
  tool_calls: AssistantToolCall[]
  session_id?: string
}

export interface AssistantStreamErrorEvent {
  type: 'error'
  message: string
}

export type AssistantStreamEvent =
  | AssistantStreamStartEvent
  | AssistantStreamContentDeltaEvent
  | AssistantStreamContentResetEvent
  | AssistantStreamToolStartEvent
  | AssistantStreamToolFinishEvent
  | AssistantStreamCompleteEvent
  | AssistantStreamErrorEvent

/** A persisted assistant conversation summary from the server chat log. */
export interface AssistantChatSessionSummary {
  id: string
  title: string
  provider?: string
  model?: string
  created_ms: number
  updated_ms: number
  message_count: number
}

export interface AssistantChatSessionListResponse {
  sessions: AssistantChatSessionSummary[]
}

/** One persisted tool invocation stored with an assistant message. */
export interface AssistantChatStoredToolCall {
  index: number
  name: string
  arguments?: unknown
  result?: unknown
  ok: boolean
  duration_ms: number
}

export interface AssistantChatStoredMessage {
  id: number
  role: AssistantRole
  content: string
  signature?: string
  model?: string
  created_ms: number
  tool_calls?: AssistantChatStoredToolCall[]
}

export interface AssistantChatSessionDetail {
  session: AssistantChatSessionSummary
  messages: AssistantChatStoredMessage[]
}

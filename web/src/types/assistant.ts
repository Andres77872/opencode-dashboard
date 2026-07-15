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
}

export interface AssistantChatResponse {
  message: AssistantMessage & { role: 'assistant'; signature: string }
  model: string
  tools_used: string[]
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
}

export interface AssistantStreamToolFinishEvent {
  type: 'tool_finish'
  call_id: string
  name: string
  ok: boolean
}

export interface AssistantStreamCompleteEvent {
  type: 'complete'
  message: AssistantMessage & { role: 'assistant'; signature: string }
  model: string
  tools_used: string[]
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

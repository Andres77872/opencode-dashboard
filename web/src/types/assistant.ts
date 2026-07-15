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

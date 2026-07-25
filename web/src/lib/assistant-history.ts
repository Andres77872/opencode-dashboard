import type { AssistantMessage } from '../types/assistant'

export const MAX_ASSISTANT_HISTORY_MESSAGES = 20
// This is a serialized-message budget, leaving headroom under the backend's
// 64 KiB HTTP envelope for context, consent metadata, brackets, and commas.
export const MAX_ASSISTANT_HISTORY_BYTES = 56 * 1024

const textEncoder = new TextEncoder()

/**
 * Removes turns the user abandoned — a stopped or failed answer plus the prompt
 * that provoked it — before the history is put on the wire.
 *
 * Two reasons this has to drop the pair rather than just the answer. The
 * backend rejects any assistant message that carries no signature, and a
 * partial answer never received one. And boundAssistantHistory stops at the
 * first role that does not alternate, so leaving the orphaned prompt behind
 * would silently truncate the replay to the newest message.
 */
export function dropAbandonedTurns<T extends { role: AssistantMessage['role']; stopped?: unknown }>(
  messages: T[],
): T[] {
  const result: T[] = []
  for (const message of messages) {
    if (message.role === 'assistant' && message.stopped) {
      if (result[result.length - 1]?.role === 'user') result.pop()
      continue
    }
    result.push(message)
  }
  return result
}

/**
 * Bounds the stateless chat payload while preserving chronological order.
 * Callers append the current user prompt before invoking this helper, so the
 * newest prompt is always retained as the final message.
 */
export function boundAssistantHistory(
  messages: AssistantMessage[],
  limit = MAX_ASSISTANT_HISTORY_MESSAGES,
  byteLimit = MAX_ASSISTANT_HISTORY_BYTES,
): AssistantMessage[] {
  const safeLimit = Math.max(1, Math.floor(limit))
  const safeByteLimit = Math.max(1, Math.floor(byteLimit))
  const start = Math.max(0, messages.length - safeLimit)
  const result: AssistantMessage[] = []
  let bytes = 0
  let expectedRole: AssistantMessage['role'] = 'user'

  // Build a contiguous suffix so truncation never creates a misleading gap in
  // the conversation. The newest item is always retained; callers append the
  // current (UI-bounded) user prompt before invoking this helper.
  for (let index = messages.length - 1; index >= start; index -= 1) {
    const message = messages[index]
    // A failed or stopped request can leave an unmatched user prompt visible
    // in memory. End the replay suffix there instead of sending adjacent roles
    // or presenting that failed prompt as completed history.
    if (message.role !== expectedRole) break
    // Count JSON wire bytes, not only raw content: quotes, backslashes, and
    // control characters expand when the request body is serialized.
    const messageBytes = textEncoder.encode(JSON.stringify(message)).byteLength
    if (result.length > 0 && bytes + messageBytes > safeByteLimit) break
    result.unshift(message)
    bytes += messageBytes
    expectedRole = expectedRole === 'user' ? 'assistant' : 'user'
  }

  // Signed stateless history must begin with a user turn. If a count/byte
  // boundary lands on an assistant answer, drop that orphaned answer.
  if (result[0]?.role === 'assistant') result.shift()

  return result
}

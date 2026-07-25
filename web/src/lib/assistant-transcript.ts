import type { AssistantRole } from '../types/assistant'

export const TRANSCRIPT_HEADINGS: Record<AssistantRole, string> = {
  user: '## You',
  assistant: '## Analytics assistant',
}

/**
 * Renders a conversation as Markdown source for the clipboard.
 *
 * Message content is already Markdown, so it is emitted verbatim — the point of
 * copying is to recover what the model wrote, not a re-serialization of what
 * the panel rendered. Empty turns (an answer stopped before its first token)
 * carry nothing worth pasting and are skipped.
 */
export function conversationToMarkdown(
  messages: Array<{ role: AssistantRole; content: string }>,
): string {
  return messages
    .filter((message) => message.content.trim() !== '')
    .map((message) => `${TRANSCRIPT_HEADINGS[message.role]}\n\n${message.content.trim()}`)
    .join('\n\n')
}

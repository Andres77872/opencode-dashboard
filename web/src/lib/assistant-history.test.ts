import assert from 'node:assert/strict'
import test from 'node:test'
import {
  boundAssistantHistory,
  dropAbandonedTurns,
  MAX_ASSISTANT_HISTORY_BYTES,
  MAX_ASSISTANT_HISTORY_MESSAGES,
} from './assistant-history.ts'
import type { AssistantMessage } from '../types/assistant.ts'

test('keeps only the newest bounded conversation in chronological order', () => {
  const messages: AssistantMessage[] = Array.from({ length: 25 }, (_, index) => ({
    role: index % 2 === 0 ? 'user' : 'assistant',
    content: `message-${index}`,
  }))

  const bounded = boundAssistantHistory(messages)

  assert.equal(bounded.length, MAX_ASSISTANT_HISTORY_MESSAGES - 1)
  assert.equal(bounded[0].content, 'message-6')
  assert.equal(bounded.at(-1)?.content, 'message-24')
  assert.equal(bounded.at(-1)?.role, 'user')
  assert.equal(messages.length, 25, 'does not mutate the in-memory conversation')
})

test('always retains at least the newest prompt for invalid small limits', () => {
  const messages: AssistantMessage[] = [
    { role: 'assistant', content: 'older' },
    { role: 'user', content: 'current prompt' },
  ]

  assert.deepEqual(boundAssistantHistory(messages, 0), [{ role: 'user', content: 'current prompt' }])
})

test('keeps a contiguous suffix within the backend history byte budget', () => {
  const block = 'x'.repeat(20 * 1024)
  const messages: AssistantMessage[] = [
    { role: 'user', content: `old-${block}` },
    { role: 'assistant', content: `middle-${block}` },
    { role: 'user', content: `new-${block}` },
  ]

  const bounded = boundAssistantHistory(messages)

  assert.equal(bounded.length, 1)
  assert.equal(bounded[0].content, messages[2].content)
  assert.ok(bounded.reduce((sum, message) => sum + Buffer.byteLength(message.content), 0) <= MAX_ASSISTANT_HISTORY_BYTES)
})

test('counts UTF-8 bytes and always retains the current prompt', () => {
  const messages: AssistantMessage[] = [
    { role: 'assistant', content: '😀😀' },
    { role: 'user', content: 'current' },
  ]

  assert.deepEqual(boundAssistantHistory(messages, 20, 10), [{ role: 'user', content: 'current' }])
  assert.deepEqual(boundAssistantHistory([{ role: 'user', content: '😀😀😀' }], 20, 1), [
    { role: 'user', content: '😀😀😀' },
  ])
})

test('does not replay an unmatched prompt from a failed request', () => {
  const messages: AssistantMessage[] = [
    { role: 'user', content: 'failed prompt' },
    { role: 'user', content: 'current prompt' },
  ]

  assert.deepEqual(boundAssistantHistory(messages), [
    { role: 'user', content: 'current prompt' },
  ])
})

test('budgets escaped JSON wire bytes below the HTTP request envelope', () => {
  const quoteHeavy = '"'.repeat(12_000)
  const messages: AssistantMessage[] = [
    { role: 'user', content: quoteHeavy },
    { role: 'assistant', content: quoteHeavy, signature: 'signed' },
    { role: 'user', content: quoteHeavy },
    { role: 'assistant', content: quoteHeavy, signature: 'signed' },
    { role: 'user', content: 'current' },
  ]

  const bounded = boundAssistantHistory(messages)
  const wireBytes = Buffer.byteLength(JSON.stringify(bounded))
  assert.ok(wireBytes < 64 * 1024, `serialized history was ${wireBytes} bytes`)
  assert.equal(bounded.at(-1)?.content, 'current')
  assert.equal(bounded[0]?.role, 'user')
})

test('an abandoned turn is dropped as a pair so roles keep alternating', () => {
  const messages = [
    { role: 'user' as const, content: 'one' },
    { role: 'assistant' as const, content: 'first answer', signature: 'sig-1' },
    { role: 'user' as const, content: 'two' },
    { role: 'assistant' as const, content: 'partial', stopped: 'stopped' as const },
    { role: 'user' as const, content: 'three' },
  ]

  assert.deepEqual(dropAbandonedTurns(messages), [
    { role: 'user', content: 'one' },
    { role: 'assistant', content: 'first answer', signature: 'sig-1' },
    { role: 'user', content: 'three' },
  ])
})

test('history before an abandoned turn still reaches the wire', () => {
  // Dropping only the unsigned answer would leave two adjacent user turns, and
  // boundAssistantHistory would stop there — sending just the newest prompt.
  const messages = [
    { role: 'user' as const, content: 'one' },
    { role: 'assistant' as const, content: 'first answer', signature: 'sig-1' },
    { role: 'user' as const, content: 'two' },
    { role: 'assistant' as const, content: 'partial', stopped: 'failed' as const },
    { role: 'user' as const, content: 'three' },
  ]

  const bounded = boundAssistantHistory(
    dropAbandonedTurns(messages).map(({ role, content, signature }) => ({ role, content, signature })),
  )
  assert.equal(bounded.length, 3)
  assert.deepEqual(bounded.map((message) => message.content), ['one', 'first answer', 'three'])
  assert.equal(bounded.every((message) => message.role !== 'assistant' || message.signature), true)
})

test('leading and trailing abandoned turns are removed without leaving orphans', () => {
  assert.deepEqual(
    dropAbandonedTurns([
      { role: 'user' as const, content: 'one' },
      { role: 'assistant' as const, content: 'partial', stopped: 'stopped' as const },
    ]),
    [],
  )
  assert.deepEqual(
    dropAbandonedTurns([
      { role: 'assistant' as const, content: 'orphan partial', stopped: 'stopped' as const },
      { role: 'user' as const, content: 'one' },
    ]),
    [{ role: 'user', content: 'one' }],
  )
})

test('dropAbandonedTurns leaves completed conversations and its input untouched', () => {
  const messages = [
    { role: 'user' as const, content: 'one' },
    { role: 'assistant' as const, content: 'answer', signature: 'sig-1' },
  ]
  const snapshot = JSON.stringify(messages)

  assert.deepEqual(dropAbandonedTurns(messages), messages)
  assert.equal(JSON.stringify(messages), snapshot)
})

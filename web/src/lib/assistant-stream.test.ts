import assert from 'node:assert/strict'
import test from 'node:test'
import { ApiClientError, streamAssistantChat } from './api.ts'
import {
  AssistantStreamProtocolError,
  AssistantStreamRemoteError,
  MAX_ASSISTANT_STREAM_RECORD_BYTES,
  parseAssistantStreamEvent,
  readAssistantStream,
} from './assistant-stream.ts'
import type {
  AssistantChatRequest,
  AssistantStreamEvent,
} from '../types/assistant.ts'

const encoder = new TextEncoder()

function byteStream(chunks: Uint8Array[]): ReadableStream<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(chunk)
      controller.close()
    },
  })
}

function textStream(value: string): ReadableStream<Uint8Array> {
  return byteStream([encoder.encode(value)])
}

function completeEvent(content = 'Résumé 😀') {
  return {
    type: 'complete',
    message: { role: 'assistant', content, signature: 'signed-response' },
    model: 'MiniMax-M3',
    tools_used: ['get_daily_usage'],
    tool_calls: [],
  } as const
}

function wireEvents(events: readonly object[]): string {
  return `${events.map((event) => JSON.stringify(event)).join('\n')}\n`
}

test('parses every event shape into the discriminated stream union', () => {
  const events = [
    { type: 'start', model: 'MiniMax-M3' },
    { type: 'content_delta', delta: 'hello' },
    { type: 'content_reset' },
    { type: 'tool_start', call_id: 'call-1', name: 'get_daily_usage' },
    { type: 'tool_finish', call_id: 'call-1', name: 'get_daily_usage', ok: true },
    completeEvent('hello'),
    { type: 'error', message: 'provider failed safely' },
  ]

  assert.deepEqual(
    events.map((event, index) => parseAssistantStreamEvent(JSON.stringify(event), index + 1)),
    events,
  )
})

test('parses tool input/output, per-call durations, session ids, and canonical tool_calls', () => {
  const toolStart = {
    type: 'tool_start',
    call_id: 'call-1',
    name: 'get_overview',
    arguments: { source: 'opencode', period: '7d' },
  }
  const toolFinish = {
    type: 'tool_finish',
    call_id: 'call-1',
    name: 'get_overview',
    ok: true,
    result: { ok: true, data: { sessions: 4 } },
    duration_ms: 32,
  }
  const complete = {
    type: 'complete',
    message: { role: 'assistant', content: 'Report.', signature: 'signed' },
    model: 'MiniMax-M3',
    tools_used: ['get_overview'],
    tool_calls: [{
      call_id: 'call-1',
      name: 'get_overview',
      ok: true,
      arguments: { source: 'opencode', period: '7d' },
      result: { ok: true, data: { sessions: 4 } },
      duration_ms: 32,
    }],
    session_id: 'cs_0123456789abcdef0123456789abcdef',
  }

  assert.deepEqual(parseAssistantStreamEvent(JSON.stringify(toolStart)), toolStart)
  assert.deepEqual(parseAssistantStreamEvent(JSON.stringify(toolFinish)), toolFinish)
  assert.deepEqual(parseAssistantStreamEvent(JSON.stringify(complete)), complete)
})

test('decodes arbitrary network and multi-byte UTF-8 chunk boundaries incrementally', async () => {
  const expected = [
    { type: 'start', model: 'MiniMax-M3' },
    { type: 'tool_start', call_id: 'call-1', name: 'get_daily_usage' },
    { type: 'tool_finish', call_id: 'call-1', name: 'get_daily_usage', ok: true },
    { type: 'content_delta', delta: 'Résumé ' },
    { type: 'content_delta', delta: '😀' },
    completeEvent(),
  ] as const
  const bytes = encoder.encode(wireEvents(expected))
  const chunks = Array.from(bytes, (_, index) => bytes.slice(index, index + 1))
  const received: AssistantStreamEvent[] = []

  const complete = await readAssistantStream(byteStream(chunks), (event) => received.push(event))

  assert.deepEqual(received, expected)
  assert.deepEqual(complete, expected.at(-1))
})

test('accepts CRLF records and ignores blank lines', async () => {
  const wire = [
    '',
    JSON.stringify({ type: 'start', model: 'MiniMax-M3' }),
    '   ',
    JSON.stringify(completeEvent('done')),
    '',
  ].join('\r\n')
  const received: AssistantStreamEvent[] = []

  const complete = await readAssistantStream(textStream(wire), (event) => received.push(event))

  assert.equal(received.length, 2)
  assert.equal(complete.message.content, 'done')
})

test('retains the exact complete content covered by the server signature', async () => {
  const signedContent = '\n  Exact signed report.  \n'
  const complete = await readAssistantStream(textStream(wireEvents([
    { type: 'start', model: 'MiniMax-M3' },
    completeEvent(signedContent),
  ])), () => undefined)

  assert.equal(complete.message.content, signedContent)
})

test('rejects an oversized newline-free record before the pending buffer can grow without limit', async () => {
  const bytes = encoder.encode('x'.repeat(MAX_ASSISTANT_STREAM_RECORD_BYTES + 1))
  const chunks: Uint8Array[] = []
  for (let offset = 0; offset < bytes.byteLength; offset += 4096) {
    chunks.push(bytes.slice(offset, offset + 4096))
  }

  await assert.rejects(
    readAssistantStream(byteStream(chunks), () => undefined),
    (error: unknown) => (
      error instanceof AssistantStreamProtocolError &&
      error.message.includes(`exceeds ${MAX_ASSISTANT_STREAM_RECORD_BYTES} bytes`)
    ),
  )
})

test('rejects malformed, unknown, and structurally invalid records', () => {
  const cases = [
    '{broken',
    '[]',
    JSON.stringify({ type: 'future_event' }),
    JSON.stringify({ type: 'start', model: '' }),
    JSON.stringify({ type: 'content_delta', delta: 7 }),
    JSON.stringify({ type: 'content_reset', extra: true }),
    JSON.stringify({ type: 'tool_start', call_id: 'call-1', name: 'tool', unexpected: true }),
    JSON.stringify({ type: 'tool_finish', call_id: 'call-1', name: 'tool', ok: 'yes' }),
    JSON.stringify({ type: 'tool_finish', call_id: 'call-1', name: 'tool', ok: true, duration_ms: 'fast' }),
    JSON.stringify({ ...completeEvent(), tools_used: [''] }),
    JSON.stringify({ ...completeEvent(), session_id: '' }),
    JSON.stringify({ ...completeEvent(), tool_calls: [{ name: 'missing-fields' }] }),
    JSON.stringify({ type: 'error', message: '' }),
  ]

  for (const line of cases) {
    assert.throws(
      () => parseAssistantStreamEvent(line, 9),
      (error: unknown) => error instanceof AssistantStreamProtocolError && error.message.includes('line 9'),
      line,
    )
  }
})

test('enforces stream ordering and one terminal complete event', async () => {
  const cases = [
    wireEvents([{ type: 'content_delta', delta: 'early' }, completeEvent()]),
    wireEvents([{ type: 'start', model: 'MiniMax-M3' }, { type: 'start', model: 'MiniMax-M3' }, completeEvent()]),
    wireEvents([{ type: 'start', model: 'MiniMax-M3' }, completeEvent(), { type: 'content_delta', delta: 'late' }]),
    wireEvents([
      { type: 'start', model: 'MiniMax-M3' },
      { type: 'tool_finish', call_id: 'missing', name: 'get_daily_usage', ok: true },
      completeEvent(),
    ]),
    wireEvents([
      { type: 'start', model: 'MiniMax-M3' },
      { type: 'tool_start', call_id: 'call-1', name: 'get_daily_usage' },
      { type: 'tool_finish', call_id: 'call-1', name: 'get_overview', ok: true },
      completeEvent(),
    ]),
    wireEvents([
      { type: 'start', model: 'MiniMax-M3' },
      { type: 'tool_start', call_id: 'call-1', name: 'get_daily_usage' },
      completeEvent(),
    ]),
  ]

  for (const wire of cases) {
    await assert.rejects(
      readAssistantStream(textStream(wire), () => undefined),
      AssistantStreamProtocolError,
    )
  }
})

test('rejects a non-newline-terminated record as truncated', async () => {
  const wire = wireEvents([{ type: 'start', model: 'MiniMax-M3' }]) + JSON.stringify(completeEvent())

  await assert.rejects(
    readAssistantStream(textStream(wire), () => undefined),
    (error: unknown) => error instanceof AssistantStreamProtocolError && error.message.includes('truncated event'),
  )
})

test('rejects a clean EOF that does not contain complete', async () => {
  const wire = wireEvents([
    { type: 'start', model: 'MiniMax-M3' },
    { type: 'content_delta', delta: 'partial' },
  ])

  await assert.rejects(
    readAssistantStream(textStream(wire), () => undefined),
    (error: unknown) => error instanceof AssistantStreamProtocolError && error.message.includes('before a complete event'),
  )
})

test('delivers a streamed error event and then rejects with its safe message', async () => {
  const received: AssistantStreamEvent[] = []
  const wire = wireEvents([
    { type: 'start', model: 'MiniMax-M3' },
    { type: 'error', message: 'MiniMax could not complete the report' },
  ])

  await assert.rejects(
    readAssistantStream(textStream(wire), (event) => received.push(event)),
    (error: unknown) => error instanceof AssistantStreamRemoteError && error.message === 'MiniMax could not complete the report',
  )
  assert.deepEqual(received.at(-1), { type: 'error', message: 'MiniMax could not complete the report' })
})

test('streamAssistantChat posts NDJSON request and returns complete after callbacks', async () => {
  const payload: AssistantChatRequest = {
    messages: [{ role: 'user', content: 'What changed?' }],
    consent_version: 'analytics-assistant-v1',
  }
  const expected = [
    { type: 'start', model: 'MiniMax-M3' },
    { type: 'content_delta', delta: 'Done' },
    completeEvent('Done'),
  ] as const
  const originalFetch = globalThis.fetch
  let requestURL = ''
  let requestInit: RequestInit | undefined
  globalThis.fetch = (async (input, init) => {
    requestURL = String(input)
    requestInit = init
    return new Response(wireEvents(expected), {
      status: 200,
      headers: { 'Content-Type': 'application/x-ndjson' },
    })
  }) as typeof fetch

  try {
    const received: AssistantStreamEvent[] = []
    const complete = await streamAssistantChat(payload, (event) => received.push(event))

    assert.equal(requestURL, '/api/v1/assistant/chat/stream')
    assert.equal(requestInit?.method, 'POST')
    assert.equal(new Headers(requestInit?.headers).get('Accept'), 'application/x-ndjson')
    assert.equal(new Headers(requestInit?.headers).get('Content-Type'), 'application/json')
    assert.equal(requestInit?.cache, 'no-store')
    assert.deepEqual(JSON.parse(String(requestInit?.body)), payload)
    assert.deepEqual(received, expected)
    assert.deepEqual(complete, expected.at(-1))
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('streamAssistantChat maps a non-2xx JSON response before reading a stream', async () => {
  const originalFetch = globalThis.fetch
  globalThis.fetch = (async () => new Response(JSON.stringify({
    error: 'Too Many Requests',
    message: 'assistant is busy; try again shortly',
    code: 429,
  }), {
    status: 429,
    headers: { 'Content-Type': 'application/json' },
  })) as typeof fetch

  try {
    await assert.rejects(
      streamAssistantChat({
        messages: [{ role: 'user', content: 'Report' }],
        consent_version: 'analytics-assistant-v1',
      }, () => undefined),
      (error: unknown) => (
        error instanceof ApiClientError &&
        error.status === 429 &&
        error.message === 'assistant is busy; try again shortly'
      ),
    )
  } finally {
    globalThis.fetch = originalFetch
  }
})

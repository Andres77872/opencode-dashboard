import type {
  AssistantStreamCompleteEvent,
  AssistantStreamEvent,
  AssistantStreamSubagentInfo,
  AssistantSubagentRun,
  AssistantToolCall,
  AssistantUsage,
} from '../types/assistant'

type AssistantStreamEventHandler = (event: AssistantStreamEvent) => void

type JSONRecord = Record<string, unknown>

// The service bounds visible assistant content to 16 KiB. This larger wire cap
// leaves room for JSON escaping while preventing a newline-free response from
// growing the pending record buffer without limit.
export const MAX_ASSISTANT_STREAM_RECORD_BYTES = 256 * 1024

export class AssistantStreamProtocolError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AssistantStreamProtocolError'
  }
}

export class AssistantStreamRemoteError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AssistantStreamRemoteError'
  }
}

function isRecord(value: unknown): value is JSONRecord {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function hasExactKeys(value: JSONRecord, expected: readonly string[]): boolean {
  const keys = Object.keys(value)
  return keys.length === expected.length && expected.every((key) => Object.hasOwn(value, key))
}

/** Requires every listed required key, and rejects keys outside required+optional. */
function hasOnlyKeys(value: JSONRecord, required: readonly string[], optional: readonly string[]): boolean {
  if (!required.every((key) => Object.hasOwn(value, key))) return false
  return Object.keys(value).every((key) => required.includes(key) || optional.includes(key))
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && value.trim() !== ''
}

function isOptionalNumber(value: unknown): value is number | undefined {
  return value === undefined || typeof value === 'number'
}

function isOptionalString(value: unknown): value is string | undefined {
  return value === undefined || typeof value === 'string'
}

function isOptionalStringArray(value: unknown): value is string[] | undefined {
  return value === undefined || (Array.isArray(value) && value.every((item) => typeof item === 'string'))
}

/** Usage counters are optional evidence: every present field must be numeric. */
function isValidUsage(value: unknown): value is AssistantUsage & JSONRecord {
  if (value === undefined) return true
  if (!isRecord(value)) return false
  return Object.entries(value).every(([key, entry]) => (
    ['requests', 'input_tokens', 'output_tokens', 'cached_input_tokens', 'reasoning_tokens', 'total_tokens']
      .includes(key) && typeof entry === 'number'
  ))
}

function isValidToolCall(value: unknown): value is AssistantToolCall & JSONRecord {
  return (
    isRecord(value) &&
    hasOnlyKeys(
      value,
      ['call_id', 'name', 'ok'],
      ['arguments', 'result', 'duration_ms', 'agent', 'parent_call_id', 'round'],
    ) &&
    isNonEmptyString(value.call_id) &&
    isNonEmptyString(value.name) &&
    typeof value.ok === 'boolean' &&
    isOptionalNumber(value.duration_ms) &&
    isOptionalNumber(value.round) &&
    isOptionalString(value.agent) &&
    isOptionalString(value.parent_call_id)
  )
}

function isValidSubagentRun(value: unknown): value is AssistantSubagentRun & JSONRecord {
  return (
    isRecord(value) &&
    hasOnlyKeys(
      value,
      ['call_id', 'agent'],
      ['title', 'task', 'status', 'report', 'error', 'rounds', 'tools_used', 'usage', 'duration_ms'],
    ) &&
    isNonEmptyString(value.call_id) &&
    isNonEmptyString(value.agent) &&
    isOptionalString(value.title) &&
    isOptionalString(value.task) &&
    isOptionalString(value.status) &&
    isOptionalString(value.report) &&
    isOptionalString(value.error) &&
    isOptionalNumber(value.rounds) &&
    isOptionalNumber(value.duration_ms) &&
    isOptionalStringArray(value.tools_used) &&
    isValidUsage(value.usage)
  )
}

/** The nested payload shared by the specialist lifecycle events. */
function isValidSubagentInfo(value: unknown): value is AssistantStreamSubagentInfo & JSONRecord {
  return (
    isRecord(value) &&
    hasOnlyKeys(
      value,
      ['agent'],
      ['title', 'task', 'status', 'report', 'error', 'rounds', 'tools_used', 'usage'],
    ) &&
    isNonEmptyString(value.agent) &&
    isOptionalString(value.title) &&
    isOptionalString(value.task) &&
    isOptionalString(value.status) &&
    isOptionalString(value.report) &&
    isOptionalString(value.error) &&
    isOptionalNumber(value.rounds) &&
    isOptionalStringArray(value.tools_used) &&
    isValidUsage(value.usage)
  )
}

function subagentInfo(value: AssistantStreamSubagentInfo & JSONRecord): AssistantStreamSubagentInfo {
  return {
    agent: value.agent,
    ...(value.title !== undefined ? { title: value.title } : {}),
    ...(value.task !== undefined ? { task: value.task } : {}),
    ...(value.status !== undefined ? { status: value.status } : {}),
    ...(value.report !== undefined ? { report: value.report } : {}),
    ...(value.error !== undefined ? { error: value.error } : {}),
    ...(value.rounds !== undefined ? { rounds: value.rounds } : {}),
    ...(value.tools_used !== undefined ? { tools_used: [...value.tools_used] } : {}),
    ...(value.usage !== undefined ? { usage: { ...value.usage } } : {}),
  }
}

function invalidEvent(lineNumber: number, message: string): AssistantStreamProtocolError {
  return new AssistantStreamProtocolError(`Invalid assistant stream event on line ${lineNumber}: ${message}`)
}

/** Parses and validates one newline-delimited assistant stream event. */
export function parseAssistantStreamEvent(line: string, lineNumber = 1): AssistantStreamEvent {
  let value: unknown
  try {
    value = JSON.parse(line)
  } catch {
    throw invalidEvent(lineNumber, 'event is not valid JSON')
  }

  if (!isRecord(value) || typeof value.type !== 'string') {
    throw invalidEvent(lineNumber, 'event must be a JSON object with a string type')
  }

  switch (value.type) {
    case 'start':
      if (!hasExactKeys(value, ['type', 'model']) || !isNonEmptyString(value.model)) {
        throw invalidEvent(lineNumber, 'start requires a non-empty model')
      }
      return { type: 'start', model: value.model }

    case 'content_delta':
      if (!hasExactKeys(value, ['type', 'delta']) || typeof value.delta !== 'string') {
        throw invalidEvent(lineNumber, 'content_delta requires a string delta')
      }
      return { type: 'content_delta', delta: value.delta }

    case 'content_reset':
      if (!hasExactKeys(value, ['type'])) {
        throw invalidEvent(lineNumber, 'content_reset does not accept additional fields')
      }
      return { type: 'content_reset' }

    case 'round_start':
      if (
        !hasOnlyKeys(value, ['type', 'round'], ['agent', 'parent_call_id']) ||
        typeof value.round !== 'number' ||
        !Number.isFinite(value.round) ||
        value.round < 1 ||
        !isOptionalString(value.agent) ||
        !isOptionalString(value.parent_call_id)
      ) {
        throw invalidEvent(lineNumber, 'round_start requires a positive round number')
      }
      return {
        type: 'round_start',
        round: value.round,
        ...(value.agent !== undefined ? { agent: value.agent } : {}),
        ...(value.parent_call_id !== undefined ? { parent_call_id: value.parent_call_id } : {}),
      }

    case 'tool_start':
      if (
        !hasOnlyKeys(value, ['type', 'call_id', 'name'], ['arguments', 'agent', 'parent_call_id', 'round']) ||
        !isNonEmptyString(value.call_id) ||
        !isNonEmptyString(value.name) ||
        !isOptionalString(value.agent) ||
        !isOptionalString(value.parent_call_id) ||
        !isOptionalNumber(value.round)
      ) {
        throw invalidEvent(lineNumber, 'tool_start requires non-empty call_id and name fields')
      }
      return {
        type: 'tool_start',
        call_id: value.call_id,
        name: value.name,
        ...(value.agent !== undefined ? { agent: value.agent } : {}),
        ...(value.parent_call_id !== undefined ? { parent_call_id: value.parent_call_id } : {}),
        ...(typeof value.round === 'number' ? { round: value.round } : {}),
        ...(value.arguments !== undefined ? { arguments: value.arguments } : {}),
      }

    case 'tool_finish':
      if (
        !hasOnlyKeys(value, ['type', 'call_id', 'name', 'ok'], ['result', 'duration_ms', 'agent', 'parent_call_id', 'round']) ||
        !isNonEmptyString(value.call_id) ||
        !isNonEmptyString(value.name) ||
        typeof value.ok !== 'boolean' ||
        !isOptionalNumber(value.duration_ms) ||
        !isOptionalNumber(value.round) ||
        !isOptionalString(value.agent) ||
        !isOptionalString(value.parent_call_id)
      ) {
        throw invalidEvent(lineNumber, 'tool_finish requires non-empty call_id and name fields and a boolean ok')
      }
      return {
        type: 'tool_finish',
        call_id: value.call_id,
        name: value.name,
        ok: value.ok,
        ...(value.agent !== undefined ? { agent: value.agent } : {}),
        ...(value.parent_call_id !== undefined ? { parent_call_id: value.parent_call_id } : {}),
        ...(typeof value.round === 'number' ? { round: value.round } : {}),
        ...(value.result !== undefined ? { result: value.result } : {}),
        ...(typeof value.duration_ms === 'number' ? { duration_ms: value.duration_ms } : {}),
      }

    case 'subagent_start':
      if (
        !hasOnlyKeys(value, ['type', 'call_id', 'subagent'], ['agent']) ||
        !isNonEmptyString(value.call_id) ||
        !isOptionalString(value.agent) ||
        !isValidSubagentInfo(value.subagent)
      ) {
        throw invalidEvent(lineNumber, 'subagent_start requires a call_id and a specialist')
      }
      return {
        type: 'subagent_start',
        call_id: value.call_id,
        ...(value.agent !== undefined ? { agent: value.agent } : {}),
        subagent: subagentInfo(value.subagent),
      }

    case 'subagent_finish':
      if (
        !hasOnlyKeys(value, ['type', 'call_id', 'ok', 'subagent'], ['agent', 'duration_ms']) ||
        !isNonEmptyString(value.call_id) ||
        typeof value.ok !== 'boolean' ||
        !isOptionalString(value.agent) ||
        !isOptionalNumber(value.duration_ms) ||
        !isValidSubagentInfo(value.subagent)
      ) {
        throw invalidEvent(lineNumber, 'subagent_finish requires a call_id, a boolean ok, and a specialist')
      }
      return {
        type: 'subagent_finish',
        call_id: value.call_id,
        ok: value.ok,
        ...(value.agent !== undefined ? { agent: value.agent } : {}),
        ...(typeof value.duration_ms === 'number' ? { duration_ms: value.duration_ms } : {}),
        subagent: subagentInfo(value.subagent),
      }

    case 'complete': {
      if (
        !hasOnlyKeys(
          value,
          ['type', 'message', 'model', 'tools_used'],
          ['tool_calls', 'subagents', 'session_id', 'session_title', 'session_usage', 'usage',
            'provider', 'agent', 'rounds', 'duration_ms', 'notices'],
        ) ||
        !isRecord(value.message) ||
        !hasExactKeys(value.message, ['role', 'content', 'signature']) ||
        value.message.role !== 'assistant' ||
        !isNonEmptyString(value.message.content) ||
        !isNonEmptyString(value.message.signature) ||
        !isNonEmptyString(value.model) ||
        !Array.isArray(value.tools_used) ||
        !value.tools_used.every(isNonEmptyString) ||
        (value.session_id !== undefined && !isNonEmptyString(value.session_id)) ||
        !isOptionalString(value.session_title) ||
        !isOptionalString(value.provider) ||
        !isOptionalString(value.agent) ||
        !isOptionalNumber(value.rounds) ||
        !isOptionalNumber(value.duration_ms) ||
        !isOptionalStringArray(value.notices) ||
        !isValidUsage(value.usage) ||
        !isValidUsage(value.session_usage) ||
        (value.tool_calls !== undefined &&
          (!Array.isArray(value.tool_calls) || !value.tool_calls.every(isValidToolCall))) ||
        (value.subagents !== undefined &&
          (!Array.isArray(value.subagents) || !value.subagents.every(isValidSubagentRun)))
      ) {
        throw invalidEvent(lineNumber, 'complete contains an invalid assistant message, model, tools_used, tool_calls, subagents, usage, or session reference')
      }
      const toolCalls = Array.isArray(value.tool_calls) ? value.tool_calls.filter(isValidToolCall) : []
      const subagents = Array.isArray(value.subagents) ? value.subagents.filter(isValidSubagentRun) : []
      return {
        type: 'complete',
        message: {
          role: 'assistant',
          content: value.message.content,
          signature: value.message.signature,
        },
        model: value.model,
        ...(value.provider !== undefined ? { provider: value.provider } : {}),
        ...(value.agent !== undefined ? { agent: value.agent } : {}),
        ...(typeof value.rounds === 'number' ? { rounds: value.rounds } : {}),
        ...(typeof value.duration_ms === 'number' ? { duration_ms: value.duration_ms } : {}),
        ...(value.usage !== undefined ? { usage: { ...value.usage } } : {}),
        ...(value.notices !== undefined ? { notices: [...value.notices] } : {}),
        tools_used: [...value.tools_used],
        tool_calls: toolCalls.map((call) => ({
          call_id: call.call_id,
          name: call.name,
          ok: call.ok,
          ...(call.agent !== undefined ? { agent: call.agent } : {}),
          ...(call.parent_call_id !== undefined ? { parent_call_id: call.parent_call_id } : {}),
          ...(typeof call.round === 'number' ? { round: call.round } : {}),
          ...(call.arguments !== undefined ? { arguments: call.arguments } : {}),
          ...(call.result !== undefined ? { result: call.result } : {}),
          ...(typeof call.duration_ms === 'number' ? { duration_ms: call.duration_ms } : {}),
        })),
        subagents: subagents.map((run) => ({ ...run, ...(run.tools_used ? { tools_used: [...run.tools_used] } : {}) })),
        ...(typeof value.session_id === 'string' ? { session_id: value.session_id } : {}),
        ...(typeof value.session_title === 'string' ? { session_title: value.session_title } : {}),
        ...(value.session_usage !== undefined ? { session_usage: { ...value.session_usage } } : {}),
      }
    }

    case 'error':
      if (!hasExactKeys(value, ['type', 'message']) || !isNonEmptyString(value.message)) {
        throw invalidEvent(lineNumber, 'error requires a non-empty message')
      }
      return { type: 'error', message: value.message }

    default:
      throw invalidEvent(lineNumber, `unsupported event type ${JSON.stringify(value.type)}`)
  }
}

/**
 * Consumes a UTF-8 NDJSON assistant response and delivers complete, validated
 * events. Records must be newline-terminated so an interrupted final record can
 * never be mistaken for a successful event.
 */
export async function readAssistantStream(
  stream: ReadableStream<Uint8Array>,
  onEvent: AssistantStreamEventHandler,
): Promise<AssistantStreamCompleteEvent> {
  const reader = stream.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: true })
  let buffer = ''
  let lineNumber = 0
  let started = false
  let complete: AssistantStreamCompleteEvent | null = null
  let pendingRecordBytes = 0
  const seenToolCalls = new Set<string>()
  const runningToolCalls = new Map<string, string>()
  const runningSubagents = new Map<string, string>()

  const accountRecordBytes = (chunk: Uint8Array) => {
    let segmentStart = 0
    for (let index = 0; index < chunk.byteLength; index += 1) {
      if (chunk[index] !== 0x0a) continue
      pendingRecordBytes += index - segmentStart
      if (pendingRecordBytes > MAX_ASSISTANT_STREAM_RECORD_BYTES) {
        throw new AssistantStreamProtocolError(
          `Assistant stream event exceeds ${MAX_ASSISTANT_STREAM_RECORD_BYTES} bytes`,
        )
      }
      pendingRecordBytes = 0
      segmentStart = index + 1
    }
    pendingRecordBytes += chunk.byteLength - segmentStart
    if (pendingRecordBytes > MAX_ASSISTANT_STREAM_RECORD_BYTES) {
      throw new AssistantStreamProtocolError(
        `Assistant stream event exceeds ${MAX_ASSISTANT_STREAM_RECORD_BYTES} bytes`,
      )
    }
  }

  const handleLine = (rawLine: string) => {
    lineNumber += 1
    const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine
    if (line.trim() === '') return
    if (complete) {
      throw invalidEvent(lineNumber, 'received an event after complete')
    }

    const event = parseAssistantStreamEvent(line, lineNumber)
    if (event.type === 'error') {
      onEvent(event)
      throw new AssistantStreamRemoteError(event.message)
    }
    if (!started) {
      if (event.type !== 'start') {
        throw invalidEvent(lineNumber, 'the first event must be start')
      }
      started = true
    } else if (event.type === 'start') {
      throw invalidEvent(lineNumber, 'received more than one start event')
    }

    if (event.type === 'tool_start') {
      if (seenToolCalls.has(event.call_id)) {
        throw invalidEvent(lineNumber, `tool call ${JSON.stringify(event.call_id)} started more than once`)
      }
      seenToolCalls.add(event.call_id)
      runningToolCalls.set(event.call_id, event.name)
    } else if (event.type === 'tool_finish') {
      const startedName = runningToolCalls.get(event.call_id)
      if (startedName === undefined) {
        throw invalidEvent(lineNumber, `tool call ${JSON.stringify(event.call_id)} finished before it started`)
      }
      if (startedName !== event.name) {
        throw invalidEvent(lineNumber, `tool call ${JSON.stringify(event.call_id)} changed names before it finished`)
      }
      runningToolCalls.delete(event.call_id)
    } else if (event.type === 'subagent_start') {
      if (seenToolCalls.has(event.call_id)) {
        throw invalidEvent(lineNumber, `call ${JSON.stringify(event.call_id)} started more than once`)
      }
      seenToolCalls.add(event.call_id)
      runningSubagents.set(event.call_id, event.subagent.agent)
    } else if (event.type === 'subagent_finish') {
      const startedAgent = runningSubagents.get(event.call_id)
      if (startedAgent === undefined) {
        throw invalidEvent(lineNumber, `specialist run ${JSON.stringify(event.call_id)} finished before it started`)
      }
      if (startedAgent !== event.subagent.agent) {
        throw invalidEvent(lineNumber, `specialist run ${JSON.stringify(event.call_id)} changed specialists before it finished`)
      }
      runningSubagents.delete(event.call_id)
    } else if (event.type === 'complete' && (runningToolCalls.size > 0 || runningSubagents.size > 0)) {
      throw invalidEvent(lineNumber, 'complete arrived while an analytics tool or specialist was still running')
    }

    onEvent(event)
    if (event.type === 'complete') complete = event
  }

  const drainLines = () => {
    let newline = buffer.indexOf('\n')
    while (newline >= 0) {
      handleLine(buffer.slice(0, newline))
      buffer = buffer.slice(newline + 1)
      newline = buffer.indexOf('\n')
    }
  }

  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      accountRecordBytes(value)
      try {
        buffer += decoder.decode(value, { stream: true })
      } catch {
        throw new AssistantStreamProtocolError('Assistant stream contains invalid UTF-8')
      }
      drainLines()
    }

    try {
      buffer += decoder.decode()
    } catch {
      throw new AssistantStreamProtocolError('Assistant stream ended with truncated UTF-8')
    }
    drainLines()
    if (buffer.trim() !== '') {
      throw new AssistantStreamProtocolError('Assistant stream ended with a truncated event')
    }
    if (!complete) {
      throw new AssistantStreamProtocolError('Assistant stream ended before a complete event')
    }
    return complete
  } catch (error) {
    try {
      await reader.cancel(error)
    } catch {
      // Preserve the parsing, callback, network, or AbortError that stopped us.
    }
    throw error
  } finally {
    reader.releaseLock()
  }
}

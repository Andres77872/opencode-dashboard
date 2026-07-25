import type {
  AssistantChatStoredMessage,
  AssistantStreamEvent,
  AssistantSubagentRun,
  AssistantToolCall,
  AssistantUsage,
} from '../types/assistant'

export type ActivityStatus = 'running' | 'complete' | 'failed'

/** One analytics tool invocation, live or restored. */
export interface ToolActivity {
  kind: 'tool'
  id: string
  name: string
  status: ActivityStatus
  agent?: string
  parentId?: string
  round?: number
  arguments?: unknown
  result?: unknown
  durationMs?: number
}

/** One delegated specialist run and the tool calls it made. */
export interface SpecialistActivity {
  kind: 'specialist'
  id: string
  agent: string
  title: string
  task: string
  status: ActivityStatus
  report?: string
  error?: string
  rounds?: number
  toolsUsed?: string[]
  usage?: AssistantUsage
  durationMs?: number
  children: ToolActivity[]
}

export type Activity = ToolActivity | SpecialistActivity

/** The failure a tool reported, extracted from its safe result envelope. */
export interface ToolFailure {
  code: string
  message: string
}

export function toolFailure(result: unknown): ToolFailure | undefined {
  if (typeof result !== 'object' || result === null) return undefined
  const envelope = result as { ok?: unknown; error?: unknown }
  if (envelope.ok !== false) return undefined
  const error = envelope.error
  if (typeof error !== 'object' || error === null) return undefined
  const { code, message } = error as { code?: unknown; message?: unknown }
  return {
    code: typeof code === 'string' ? code : 'tool_failed',
    message: typeof message === 'string' ? message : 'The analytics tool failed safely.',
  }
}

function replaceById(activities: Activity[], id: string, update: (activity: Activity) => Activity): Activity[] {
  let replaced = false
  const next = activities.map((activity) => {
    if (activity.id === id) {
      replaced = true
      return update(activity)
    }
    if (activity.kind === 'specialist') {
      const children = activity.children.map((child) => {
        if (child.id !== id) return child
        replaced = true
        return update(child) as ToolActivity
      })
      return children === activity.children ? activity : { ...activity, children }
    }
    return activity
  })
  return replaced ? next : activities
}

function appendChild(activities: Activity[], parentId: string, child: ToolActivity): Activity[] | null {
  let attached = false
  const next = activities.map((activity) => {
    if (activity.kind !== 'specialist' || activity.id !== parentId) return activity
    attached = true
    return { ...activity, children: [...activity.children.filter((item) => item.id !== child.id), child] }
  })
  return attached ? next : null
}

/**
 * Folds one progress event into the activity list. Tool calls made by a
 * specialist are nested under the delegation that started them, so live
 * progress reads as the same tree a restored conversation shows.
 */
export function applyStreamEvent(activities: Activity[], event: AssistantStreamEvent): Activity[] {
  switch (event.type) {
    case 'tool_start': {
      const activity: ToolActivity = {
        kind: 'tool',
        id: event.call_id,
        name: event.name,
        status: 'running',
        ...(event.agent !== undefined ? { agent: event.agent } : {}),
        ...(event.parent_call_id !== undefined ? { parentId: event.parent_call_id } : {}),
        ...(event.round !== undefined ? { round: event.round } : {}),
        ...(event.arguments !== undefined ? { arguments: event.arguments } : {}),
      }
      if (event.parent_call_id) {
        const nested = appendChild(activities, event.parent_call_id, activity)
        if (nested) return nested
      }
      return [...activities.filter((item) => item.id !== activity.id), activity]
    }

    case 'tool_finish':
      return replaceById(activities, event.call_id, (activity) => ({
        ...activity,
        status: event.ok ? 'complete' : 'failed',
        ...(event.result !== undefined ? { result: event.result } : {}),
        ...(event.duration_ms !== undefined ? { durationMs: event.duration_ms } : {}),
      }))

    case 'subagent_start': {
      const activity: SpecialistActivity = {
        kind: 'specialist',
        id: event.call_id,
        agent: event.subagent.agent,
        title: event.subagent.title ?? event.subagent.agent,
        task: event.subagent.task ?? '',
        status: 'running',
        children: [],
      }
      return [...activities.filter((item) => item.id !== activity.id), activity]
    }

    case 'subagent_finish':
      return replaceById(activities, event.call_id, (activity) => {
        const previous = activity.kind === 'specialist' ? activity : undefined
        return {
          kind: 'specialist',
          id: event.call_id,
          agent: event.subagent.agent,
          title: event.subagent.title ?? previous?.title ?? event.subagent.agent,
          task: event.subagent.task ?? previous?.task ?? '',
          status: event.ok ? 'complete' : 'failed',
          children: previous?.children ?? [],
          ...(event.subagent.report !== undefined ? { report: event.subagent.report } : {}),
          ...(event.subagent.error !== undefined ? { error: event.subagent.error } : {}),
          ...(event.subagent.rounds !== undefined ? { rounds: event.subagent.rounds } : {}),
          ...(event.subagent.tools_used !== undefined ? { toolsUsed: event.subagent.tools_used } : {}),
          ...(event.subagent.usage !== undefined ? { usage: event.subagent.usage } : {}),
          ...(event.duration_ms !== undefined ? { durationMs: event.duration_ms } : {}),
        }
      })

    default:
      return activities
  }
}

/** Marks anything still running as finished, used when a turn ends. */
export function settleActivities(activities: Activity[]): Activity[] {
  return activities.map((activity) => {
    const settled = activity.status === 'running' ? { ...activity, status: 'complete' as const } : activity
    if (settled.kind !== 'specialist') return settled
    return { ...settled, children: settleActivities(settled.children) as ToolActivity[] }
  })
}

function toolActivityFrom(call: AssistantToolCall): ToolActivity {
  return {
    kind: 'tool',
    id: call.call_id,
    name: call.name,
    status: call.ok ? 'complete' : 'failed',
    ...(call.agent !== undefined ? { agent: call.agent } : {}),
    ...(call.parent_call_id !== undefined ? { parentId: call.parent_call_id } : {}),
    ...(call.round !== undefined ? { round: call.round } : {}),
    ...(call.arguments !== undefined ? { arguments: call.arguments } : {}),
    ...(call.result !== undefined ? { result: call.result } : {}),
    ...(call.duration_ms !== undefined ? { durationMs: call.duration_ms } : {}),
  }
}

function specialistActivityFrom(run: AssistantSubagentRun): SpecialistActivity {
  return {
    kind: 'specialist',
    id: run.call_id,
    agent: run.agent,
    title: run.title ?? run.agent,
    task: run.task ?? '',
    status: run.status === 'failed' || run.error ? 'failed' : 'complete',
    children: [],
    ...(run.report !== undefined ? { report: run.report } : {}),
    ...(run.error !== undefined ? { error: run.error } : {}),
    ...(run.rounds !== undefined ? { rounds: run.rounds } : {}),
    ...(run.tools_used !== undefined ? { toolsUsed: run.tools_used } : {}),
    ...(run.usage !== undefined ? { usage: run.usage } : {}),
    ...(run.duration_ms !== undefined ? { durationMs: run.duration_ms } : {}),
  }
}

/**
 * Builds the canonical activity tree for a finished turn. The server's records
 * are authoritative, so this replaces whatever the live stream assembled and
 * makes a restored conversation render identically.
 */
export function activitiesFromRecords(
  toolCalls: readonly AssistantToolCall[],
  subagents: readonly AssistantSubagentRun[] = [],
): Activity[] {
  const specialists = new Map<string, SpecialistActivity>()
  const ordered: Activity[] = []
  for (const run of subagents) {
    const activity = specialistActivityFrom(run)
    specialists.set(activity.id, activity)
    ordered.push(activity)
  }
  for (const call of toolCalls) {
    const activity = toolActivityFrom(call)
    const parent = activity.parentId ? specialists.get(activity.parentId) : undefined
    if (parent) {
      parent.children.push(activity)
      continue
    }
    ordered.push(activity)
  }
  return ordered
}

/** Rebuilds the activity tree for one restored assistant message. */
export function activitiesFromStoredMessage(message: AssistantChatStoredMessage): Activity[] {
  const toolCalls: AssistantToolCall[] = (message.tool_calls ?? []).map((call) => ({
    call_id: call.call_ref || `${message.id}-tool-${call.index}`,
    name: call.name,
    ok: call.ok,
    ...(call.agent !== undefined ? { agent: call.agent } : {}),
    ...(call.parent_call_ref ? { parent_call_id: call.parent_call_ref } : {}),
    ...(call.round !== undefined ? { round: call.round } : {}),
    ...(call.arguments !== undefined ? { arguments: call.arguments } : {}),
    ...(call.result !== undefined ? { result: call.result } : {}),
    ...(call.duration_ms !== undefined ? { duration_ms: call.duration_ms } : {}),
  }))
  const subagents: AssistantSubagentRun[] = (message.subagents ?? []).map((run) => ({
    call_id: run.call_ref || `${message.id}-specialist-${run.index}`,
    agent: run.agent,
    ...(run.title !== undefined ? { title: run.title } : {}),
    ...(run.task !== undefined ? { task: run.task } : {}),
    ...(run.status !== undefined ? { status: run.status } : {}),
    ...(run.report !== undefined ? { report: run.report } : {}),
    ...(run.error !== undefined ? { error: run.error } : {}),
    ...(run.rounds !== undefined ? { rounds: run.rounds } : {}),
    ...(run.tools_used !== undefined ? { tools_used: run.tools_used } : {}),
    ...(run.usage !== undefined ? { usage: run.usage } : {}),
    ...(run.duration_ms !== undefined ? { duration_ms: run.duration_ms } : {}),
  }))
  return activitiesFromRecords(toolCalls, subagents)
}

/** Counts how many calls the tree holds, including specialists' own calls. */
export function countToolActivities(activities: readonly Activity[]): number {
  return activities.reduce(
    (total, activity) => total + (activity.kind === 'specialist' ? activity.children.length + 1 : 1),
    0,
  )
}

export function formatDurationMs(durationMs?: number): string {
  if (durationMs === undefined || durationMs < 0) return ''
  if (durationMs < 1000) return `${Math.round(durationMs)} ms`
  if (durationMs < 60_000) return `${(durationMs / 1000).toFixed(durationMs < 10_000 ? 1 : 0)} s`
  const minutes = Math.floor(durationMs / 60_000)
  return `${minutes} min ${Math.round((durationMs % 60_000) / 1000)} s`
}

export function formatTokenCount(value: number): string {
  if (!Number.isFinite(value) || value < 0) return '0'
  if (value < 10_000) return value.toLocaleString()
  if (value < 1_000_000) return `${(value / 1000).toFixed(value < 100_000 ? 1 : 0)}k`
  return `${(value / 1_000_000).toFixed(1)}M`
}

export function hasReportedTokens(usage?: AssistantUsage): boolean {
  if (!usage) return false
  return usage.input_tokens > 0 || usage.output_tokens > 0 || usage.total_tokens > 0
}

/**
 * Describes a turn's cost for the message footer. A provider that reported no
 * token counters is shown as "not reported" rather than as a measured zero.
 */
export function usageSummary(usage?: AssistantUsage): string {
  if (!usage) return ''
  if (!hasReportedTokens(usage)) {
    return usage.requests > 0 ? `${usage.requests} model ${usage.requests === 1 ? 'request' : 'requests'}` : ''
  }
  const total = usage.total_tokens || usage.input_tokens + usage.output_tokens
  const parts = [`${formatTokenCount(total)} tokens`]
  if (usage.input_tokens > 0 || usage.output_tokens > 0) {
    parts.push(`${formatTokenCount(usage.input_tokens)} in · ${formatTokenCount(usage.output_tokens)} out`)
  }
  if (usage.cached_input_tokens && usage.cached_input_tokens > 0) {
    parts.push(`${formatTokenCount(usage.cached_input_tokens)} cached`)
  }
  if (usage.reasoning_tokens && usage.reasoning_tokens > 0) {
    parts.push(`${formatTokenCount(usage.reasoning_tokens)} reasoning`)
  }
  return parts.join(' · ')
}

export function humanizeAgentName(value: string): string {
  return value.replace(/[_-]+/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
}

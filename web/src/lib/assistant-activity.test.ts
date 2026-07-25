import assert from 'node:assert/strict'
import test from 'node:test'
import {
  activitiesFromRecords,
  activitiesFromStoredMessage,
  applyStreamEvent,
  countToolActivities,
  formatDurationMs,
  formatTokenCount,
  settleActivities,
  toolFailure,
  usageSummary,
  type Activity,
  type SpecialistActivity,
} from './assistant-activity.ts'
import type { AssistantChatStoredMessage, AssistantStreamEvent } from '../types/assistant.ts'

function fold(events: readonly AssistantStreamEvent[]): Activity[] {
  return events.reduce<Activity[]>((activities, event) => applyStreamEvent(activities, event), [])
}

function specialist(activities: readonly Activity[], id: string): SpecialistActivity {
  const found = activities.find((activity) => activity.id === id)
  assert.ok(found && found.kind === 'specialist', `no specialist ${id}`)
  return found
}

test('folds tool lifecycle events into ordered activities', () => {
  const activities = fold([
    { type: 'tool_start', call_id: 'tool-1', name: 'list_sources', arguments: {} },
    { type: 'tool_start', call_id: 'tool-2', name: 'get_overview', arguments: { source: 'opencode' } },
    { type: 'tool_finish', call_id: 'tool-2', name: 'get_overview', ok: true, result: { ok: true }, duration_ms: 21 },
    { type: 'tool_finish', call_id: 'tool-1', name: 'list_sources', ok: false, result: { ok: false }, duration_ms: 5 },
  ])

  assert.equal(activities.length, 2)
  // Order follows the calls the model made, not the order they finished.
  assert.deepEqual(activities.map((activity) => activity.id), ['tool-1', 'tool-2'])
  assert.equal(activities[0].status, 'failed')
  assert.equal(activities[1].status, 'complete')
  assert.equal(activities[1].kind === 'tool' ? activities[1].durationMs : undefined, 21)
})

test('nests a specialist tool call under the delegation that started it', () => {
  const activities = fold([
    {
      type: 'subagent_start',
      call_id: 'tool-1',
      subagent: { agent: 'trend_analyst', title: 'Trend analyst', task: 'Explain the trend.' },
    },
    {
      type: 'tool_start',
      call_id: 'tool-2',
      name: 'get_daily_usage',
      agent: 'trend_analyst',
      parent_call_id: 'tool-1',
    },
    {
      type: 'tool_finish',
      call_id: 'tool-2',
      name: 'get_daily_usage',
      ok: true,
      agent: 'trend_analyst',
      parent_call_id: 'tool-1',
      duration_ms: 12,
    },
    {
      type: 'subagent_finish',
      call_id: 'tool-1',
      ok: true,
      duration_ms: 340,
      subagent: {
        agent: 'trend_analyst',
        title: 'Trend analyst',
        task: 'Explain the trend.',
        status: 'complete',
        report: 'Tokens rose 40%.',
        rounds: 2,
        tools_used: ['get_daily_usage'],
        usage: { requests: 2, input_tokens: 600, output_tokens: 90, total_tokens: 690 },
      },
    },
  ])

  assert.equal(activities.length, 1, 'a nested call must not appear at the top level')
  const run = specialist(activities, 'tool-1')
  assert.equal(run.status, 'complete')
  assert.equal(run.report, 'Tokens rose 40%.')
  assert.equal(run.usage?.total_tokens, 690)
  assert.equal(run.durationMs, 340)
  assert.equal(run.children.length, 1)
  assert.equal(run.children[0].status, 'complete')
  assert.equal(countToolActivities(activities), 2)
})

test('keeps a specialist tool call visible when its delegation is unknown', () => {
  const activities = fold([
    { type: 'tool_start', call_id: 'tool-9', name: 'get_daily_usage', parent_call_id: 'missing' },
  ])
  assert.equal(activities.length, 1)
  assert.equal(activities[0].id, 'tool-9')
})

test('a failed specialist keeps its failure and drops no prior work', () => {
  const activities = fold([
    { type: 'subagent_start', call_id: 'tool-1', subagent: { agent: 'cost_auditor' } },
    { type: 'tool_start', call_id: 'tool-2', name: 'get_overview', parent_call_id: 'tool-1' },
    { type: 'tool_finish', call_id: 'tool-2', name: 'get_overview', ok: true, parent_call_id: 'tool-1' },
    {
      type: 'subagent_finish',
      call_id: 'tool-1',
      ok: false,
      subagent: { agent: 'cost_auditor', status: 'failed', error: 'The specialist failed before producing a finding.' },
    },
  ])
  const run = specialist(activities, 'tool-1')
  assert.equal(run.status, 'failed')
  assert.equal(run.error, 'The specialist failed before producing a finding.')
  assert.equal(run.children.length, 1)
})

test('settling a turn finishes anything still marked running', () => {
  const activities = settleActivities(fold([
    { type: 'subagent_start', call_id: 'tool-1', subagent: { agent: 'trend_analyst' } },
    { type: 'tool_start', call_id: 'tool-2', name: 'get_daily_usage', parent_call_id: 'tool-1' },
  ]))
  const run = specialist(activities, 'tool-1')
  assert.equal(run.status, 'complete')
  assert.equal(run.children[0].status, 'complete')
})

test('canonical records rebuild the same tree the stream showed', () => {
  const activities = activitiesFromRecords(
    [
      { call_id: 'tool-2', name: 'get_daily_usage', ok: true, agent: 'trend_analyst', parent_call_id: 'tool-1', duration_ms: 12 },
      { call_id: 'tool-3', name: 'list_sources', ok: true, agent: 'analyst' },
    ],
    [{
      call_id: 'tool-1', agent: 'trend_analyst', title: 'Trend analyst', task: 'Explain the trend.',
      status: 'complete', report: 'Tokens rose.', rounds: 2, tools_used: ['get_daily_usage'],
    }],
  )
  assert.deepEqual(activities.map((activity) => activity.id), ['tool-1', 'tool-3'])
  assert.equal(specialist(activities, 'tool-1').children.length, 1)
  assert.equal(countToolActivities(activities), 3)
})

test('a restored message rebuilds specialists, nested calls, and payloads', () => {
  const stored: AssistantChatStoredMessage = {
    id: 7,
    role: 'assistant',
    content: 'Report.',
    created_ms: 1,
    tool_calls: [
      {
        index: 0, name: 'get_daily_usage', call_ref: 'tool-2', parent_call_ref: 'tool-1',
        agent: 'trend_analyst', round: 1, ok: true, duration_ms: 12,
        arguments: { source: 'opencode' }, result: { ok: true, data: {} },
      },
      { index: 1, name: 'list_sources', call_ref: 'tool-3', ok: false, duration_ms: 3, result: { ok: false } },
    ],
    subagents: [{
      index: 0, call_ref: 'tool-1', agent: 'trend_analyst', title: 'Trend analyst',
      task: 'Explain the trend.', status: 'complete', report: 'Tokens rose.', rounds: 2,
      tools_used: ['get_daily_usage'], duration_ms: 340,
      usage: { requests: 2, input_tokens: 600, output_tokens: 90, total_tokens: 690 },
    }],
  }

  const activities = activitiesFromStoredMessage(stored)
  assert.deepEqual(activities.map((activity) => activity.id), ['tool-1', 'tool-3'])
  const run = specialist(activities, 'tool-1')
  assert.equal(run.children.length, 1)
  assert.deepEqual(run.children[0].arguments, { source: 'opencode' })
  assert.equal(run.usage?.total_tokens, 690)
  assert.equal(activities[1].status, 'failed')
})

test('a restored message without call references still renders unique activities', () => {
  const activities = activitiesFromStoredMessage({
    id: 12,
    role: 'assistant',
    content: 'Report.',
    created_ms: 1,
    tool_calls: [
      { index: 0, name: 'list_sources', ok: true, duration_ms: 1 },
      { index: 1, name: 'get_overview', ok: true, duration_ms: 2 },
    ],
  })
  assert.equal(activities.length, 2)
  assert.notEqual(activities[0].id, activities[1].id)
})

test('reads the failure a tool reported and ignores successful envelopes', () => {
  assert.deepEqual(
    toolFailure({ ok: false, error: { code: 'duplicate_call', message: 'Reuse that result.' } }),
    { code: 'duplicate_call', message: 'Reuse that result.' },
  )
  assert.deepEqual(
    toolFailure({ ok: false }),
    undefined,
  )
  assert.equal(toolFailure({ ok: true, data: {} }), undefined)
  assert.equal(toolFailure('nonsense'), undefined)
  assert.equal(toolFailure(null), undefined)
})

test('usage is summarized without inventing counters the provider never reported', () => {
  assert.equal(usageSummary(undefined), '')
  assert.equal(usageSummary({ requests: 2, input_tokens: 0, output_tokens: 0, total_tokens: 0 }), '2 model requests')
  assert.equal(usageSummary({ requests: 1, input_tokens: 0, output_tokens: 0, total_tokens: 0 }), '1 model request')
  assert.equal(
    usageSummary({
      requests: 3, input_tokens: 1200, output_tokens: 300, cached_input_tokens: 900,
      reasoning_tokens: 120, total_tokens: 1500,
    }),
    '1,500 tokens · 1,200 in · 300 out · 900 cached · 120 reasoning',
  )
})

test('durations and token counts stay compact and readable', () => {
  assert.equal(formatDurationMs(undefined), '')
  assert.equal(formatDurationMs(-1), '')
  assert.equal(formatDurationMs(940), '940 ms')
  assert.equal(formatDurationMs(1500), '1.5 s')
  assert.equal(formatDurationMs(42_000), '42 s')
  assert.equal(formatDurationMs(125_000), '2 min 5 s')
  assert.equal(formatTokenCount(950), '950')
  assert.equal(formatTokenCount(12_500), '12.5k')
  assert.equal(formatTokenCount(250_000), '250k')
  assert.equal(formatTokenCount(2_400_000), '2.4M')
})

import assert from 'node:assert/strict'
import test from 'node:test'
import { buildCombinedDailyTotals, buildSourceTrendData, trendMetricValue } from './overview-all.ts'
import type { DayStats, SourceOverview } from '../types/api.ts'

function day(date: string, messages: number, cost: number, sessions = 0): DayStats {
  return {
    date,
    sessions,
    messages,
    cost,
    tokens: { input: messages * 2, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
  }
}

function src(id: string, trend: DayStats[]): SourceOverview {
  return {
    source_id: id as SourceOverview['source_id'],
    label: id,
    overview: {
      sessions: 0,
      messages: 0,
      cost: 0,
      tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
      cost_per_day: 0,
      days: 0,
    },
    message_share: 0,
    token_share: 0,
    messages_per_session: 0,
    tokens_per_message: { input: 0, output: 0, reasoning: 0, cache_read: 0, cache_write: 0 },
    trend,
  }
}

test('trendMetricValue selects the requested metric', () => {
  const d = day('2026-01-01', 10, 1.5)
  assert.equal(trendMetricValue(d, 'messages'), 10)
  assert.equal(trendMetricValue(d, 'cost'), 1.5)
  assert.equal(trendMetricValue(d, 'tokens'), 20) // input = messages * 2
})

test('buildSourceTrendData merges by date with one column per source, ascending', () => {
  const sources = [
    src('opencode', [day('2026-01-02', 4, 0), day('2026-01-01', 2, 0)]),
    src('codex', [day('2026-01-02', 7, 0)]),
  ]
  const rows = buildSourceTrendData(sources, 'messages')

  assert.deepEqual(rows.map((r) => r.date), ['2026-01-01', '2026-01-02'])
  assert.equal(rows[0].opencode, 2)
  assert.equal(rows[0].codex, undefined) // codex had no activity on day 1
  assert.equal(rows[1].opencode, 4)
  assert.equal(rows[1].codex, 7)
})

test('buildSourceTrendData tolerates missing trends', () => {
  const sources = [src('opencode', [])]
  assert.deepEqual(buildSourceTrendData(sources, 'cost'), [])
})

test('buildCombinedDailyTotals sums tokens, sessions, and messages per day, ascending', () => {
  const sources = [
    src('opencode', [day('2026-01-02', 4, 9.9, 1), day('2026-01-01', 2, 9.9, 1)]),
    src('codex', [day('2026-01-02', 7, 9.9, 3)]),
  ]
  const rows = buildCombinedDailyTotals(sources)

  assert.deepEqual(rows, [
    { date: '2026-01-01', tokens: 4, sessions: 1, messages: 2 },
    { date: '2026-01-02', tokens: 22, sessions: 4, messages: 11 },
  ])
})

test('buildCombinedDailyTotals tolerates missing trends', () => {
  assert.deepEqual(buildCombinedDailyTotals([src('opencode', [])]), [])
})

import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildCombinedDailyTotals,
  buildSourceMetricShares,
  buildSourceTrendData,
  overviewMetricValue,
  trendMetricValue,
} from './overview-all.ts'
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

function srcWith(id: string, ov: { tokens?: number; cost?: number; messages?: number }): SourceOverview {
  const base = src(id, [])
  return {
    ...base,
    overview: {
      ...base.overview,
      messages: ov.messages ?? 0,
      cost: ov.cost ?? 0,
      tokens: { input: ov.tokens ?? 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
    },
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

test('overviewMetricValue selects the requested metric from per-source totals', () => {
  const s = srcWith('claude_code', { tokens: 100, cost: 2.5, messages: 7 })
  assert.equal(overviewMetricValue(s.overview, 'tokens'), 100)
  assert.equal(overviewMetricValue(s.overview, 'cost'), 2.5)
  assert.equal(overviewMetricValue(s.overview, 'messages'), 7)
})

test('buildSourceMetricShares computes positive shares that sum to 1', () => {
  const shares = buildSourceMetricShares([srcWith('a', { tokens: 30 }), srcWith('b', { tokens: 10 })], 'tokens')
  assert.equal(shares[0].value, 30)
  assert.equal(shares[1].value, 10)
  assert.ok(Math.abs(shares[0].share - 0.75) < 1e-9)
  assert.ok(Math.abs(shares[1].share - 0.25) < 1e-9)
  assert.ok(Math.abs(shares[0].share + shares[1].share - 1) < 1e-9)
})

test('buildSourceMetricShares returns zero shares (no NaN) when the metric total is zero', () => {
  const shares = buildSourceMetricShares([srcWith('a', { tokens: 0 }), srcWith('b', { tokens: 0 })], 'tokens')
  for (const s of shares) {
    assert.equal(s.value, 0)
    assert.equal(s.share, 0)
    assert.ok(!Number.isNaN(s.share))
  }
})

test('buildSourceMetricShares gives a single source a share of 1', () => {
  const shares = buildSourceMetricShares([srcWith('a', { cost: 5 })], 'cost')
  assert.equal(shares.length, 1)
  assert.equal(shares[0].value, 5)
  assert.equal(shares[0].share, 1)
})

test('buildSourceMetricShares reads cost for the cost metric', () => {
  const shares = buildSourceMetricShares([srcWith('a', { cost: 3, tokens: 999 }), srcWith('b', { cost: 1, tokens: 1 })], 'cost')
  assert.equal(shares[0].value, 3)
  assert.equal(shares[1].value, 1)
  assert.ok(Math.abs(shares[0].share - 0.75) < 1e-9)
})

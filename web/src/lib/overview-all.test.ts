import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildCombinedDailyTotals,
  buildModelMetricBreakdown,
  buildSourceMetricShares,
  buildSourceTrendData,
  dimensionMetricValue,
  modelMetricValue,
  modelUsageKey,
  overviewMetricValue,
  trendMetricValue,
  usageMetricCopy,
} from './overview-all.ts'
import type { DayStats, DimensionDayStats, ModelEntry, SourceID, SourceOverview } from '../types/api.ts'

function day(date: string, messages: number, cost: number, sessions = 0, requests = messages): DayStats {
  return {
    date,
    sessions,
    messages,
    requests,
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
      requests: 0,
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

function srcWith(id: string, ov: { tokens?: number; cost?: number; messages?: number; requests?: number }): SourceOverview {
  const base = src(id, [])
  return {
    ...base,
    overview: {
      ...base.overview,
      messages: ov.messages ?? 0,
      requests: ov.requests ?? 0,
      cost: ov.cost ?? 0,
      tokens: { input: ov.tokens ?? 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
    },
  }
}

function model(sourceId: SourceID, modelId: string, value: number, providerId = 'provider'): ModelEntry {
  return {
    source_id: sourceId,
    model_id: modelId,
    provider_id: providerId,
    sessions: 1,
    messages: value,
    cost: value / 10,
    tokens: { input: value * 2, output: value, reasoning: 0, cache: { read: 0, write: 0 } },
  }
}

function modelDay(sourceId: SourceID, date: string, modelId: string, value: number): DimensionDayStats {
  return {
    source_id: sourceId,
    date,
    dimension_key: modelId,
    sessions: 1,
    messages: value,
    cost: value / 10,
    tokens: { input: value * 2, output: value, reasoning: 0, cache: { read: 0, write: 0 } },
  }
}

test('trendMetricValue selects requests independently from transcript messages', () => {
  const d = day('2026-01-01', 10, 1.5, 1, 6)
  assert.equal(trendMetricValue(d, 'requests'), 6)
  assert.equal(trendMetricValue(d, 'cost'), 1.5)
  assert.equal(trendMetricValue(d, 'tokens'), 20) // input = messages * 2
})

test('buildSourceTrendData merges by date with one column per source, ascending', () => {
  const sources = [
    src('opencode', [day('2026-01-02', 4, 0), day('2026-01-01', 2, 0)]),
    src('codex', [day('2026-01-02', 7, 0)]),
  ]
  const rows = buildSourceTrendData(sources, 'requests')

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

test('buildCombinedDailyTotals sums tokens, sessions, messages, and requests per day, ascending', () => {
  const sources = [
    src('opencode', [day('2026-01-02', 4, 9.9, 1, 3), day('2026-01-01', 2, 9.9, 1, 1)]),
    src('codex', [day('2026-01-02', 7, 9.9, 3, 5)]),
  ]
  const rows = buildCombinedDailyTotals(sources)

  assert.deepEqual(rows, [
    { date: '2026-01-01', tokens: 4, sessions: 1, messages: 2, requests: 1 },
    { date: '2026-01-02', tokens: 22, sessions: 4, messages: 11, requests: 8 },
  ])
})

test('buildCombinedDailyTotals tolerates missing trends', () => {
  assert.deepEqual(buildCombinedDailyTotals([src('opencode', [])]), [])
})

test('overviewMetricValue selects the requested metric from per-source totals', () => {
  const s = srcWith('claude_code', { tokens: 100, cost: 2.5, messages: 7, requests: 4 })
  assert.equal(overviewMetricValue(s.overview, 'tokens'), 100)
  assert.equal(overviewMetricValue(s.overview, 'cost'), 2.5)
  assert.equal(overviewMetricValue(s.overview, 'requests'), 4)
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

test('model and dimension metric selectors expose assistant rows as requests', () => {
  const total = model('opencode', 'gpt-5', 10)
  const daily = modelDay('opencode', '2026-01-01', 'gpt-5', 4)

  assert.equal(modelMetricValue(total, 'requests'), 10)
  assert.equal(modelMetricValue(total, 'cost'), 1)
  assert.equal(modelMetricValue(total, 'tokens'), 30)
  assert.equal(dimensionMetricValue(daily, 'requests'), 4)
  assert.equal(dimensionMetricValue(daily, 'cost'), 0.4)
  assert.equal(dimensionMetricValue(daily, 'tokens'), 12)
})

test('request grouping uses request vocabulary and explains model attribution', () => {
  assert.deepEqual(usageMetricCopy('requests', 'source'), { label: 'Requests', noun: 'requests' })
  assert.deepEqual(usageMetricCopy('requests', 'model'), {
    label: 'Requests',
    noun: 'requests',
    explanation: 'Model Requests include assistant/API request rows that can be attributed to a model. Source Requests include every recorded outbound attempt, including attempts without usage.',
  })
  assert.match(usageMetricCopy('tokens', 'model').explanation ?? '', /additive per-step usage.*message snapshots/i)
  assert.match(usageMetricCopy('cost', 'source').explanation ?? '', /reported spend.*estimated API-equivalent/i)
})

test('model usage identity includes source, provider, and model without delimiter collisions', () => {
  assert.notEqual(
    modelUsageKey('opencode', ['openai'], 'gpt-5'),
    modelUsageKey('codex', ['openai'], 'gpt-5'),
  )
  assert.notEqual(
    modelUsageKey('opencode', ['openai'], 'gpt-5'),
    modelUsageKey('opencode', ['azure'], 'gpt-5'),
  )
})

test('model breakdown keeps the same model source-scoped and merges daily rows by date', () => {
  const breakdown = buildModelMetricBreakdown(
    [model('opencode', 'gpt-5', 30), model('codex', 'gpt-5', 10)],
    [
      modelDay('opencode', '2026-01-02', 'gpt-5', 20),
      modelDay('codex', '2026-01-02', 'gpt-5', 5),
      modelDay('opencode', '2026-01-01', 'gpt-5', 10),
    ],
    'requests',
  )

  assert.equal(breakdown.series.length, 2)
  const open = breakdown.series.find((series) => series.sourceId === 'opencode')
  const codex = breakdown.series.find((series) => series.sourceId === 'codex')
  assert.ok(open)
  assert.ok(codex)
  assert.notEqual(open.id, codex.id)
  assert.equal(open.share, 0.75)
  assert.equal(codex.share, 0.25)
  assert.deepEqual(breakdown.trend.map((row) => row.date), ['2026-01-01', '2026-01-02'])
  assert.equal(breakdown.trend[1][open.id], 20)
  assert.equal(breakdown.trend[1][codex.id], 5)
})

test('model breakdown bounds high cardinality with an Other bucket in totals and trend', () => {
  const models = Array.from({ length: 10 }, (_, index) => model('opencode', `model-${index + 1}`, 10 - index))
  const trend = models.map((entry) => modelDay('opencode', '2026-01-01', entry.model_id, entry.messages))
  const breakdown = buildModelMetricBreakdown(models, trend, 'requests', 4)

  assert.equal(breakdown.series.length, 4)
  assert.deepEqual(breakdown.series.slice(0, 3).map((series) => series.value), [10, 9, 8])
  const other = breakdown.series[3]
  assert.equal(other.isOther, true)
  assert.equal(other.memberCount, 7)
  assert.equal(other.value, 28)
  assert.equal(breakdown.total, 55)
  assert.equal(breakdown.trend[0][other.id], 28)
  assert.ok(Math.abs(breakdown.series.reduce((sum, series) => sum + series.share, 0) - 1) < 1e-9)
})

test('model breakdown falls back to daily totals when model totals are unavailable', () => {
  const breakdown = buildModelMetricBreakdown(
    [],
    [
      modelDay('claude_code', '2026-01-01', 'opus', 3),
      modelDay('claude_code', '2026-01-02', 'opus', 7),
    ],
    'tokens',
  )

  assert.equal(breakdown.series.length, 1)
  assert.equal(breakdown.series[0].value, 30)
  assert.equal(breakdown.series[0].share, 1)
})

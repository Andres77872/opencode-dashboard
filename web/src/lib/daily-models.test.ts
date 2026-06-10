import assert from 'node:assert/strict'
import test from 'node:test'
import { groupModelDaysByDate } from './daily-models.ts'
import type { DimensionDayStats } from '../types/api.ts'

function row(date: string, model: string, messages: number): DimensionDayStats {
  return {
    date,
    dimension_key: model,
    sessions: 1,
    messages,
    cost: 0,
    tokens: { input: 0, output: 0, reasoning: 0, cache: { read: 0, write: 0 } },
  }
}

test('groups rows by date', () => {
  const map = groupModelDaysByDate([
    row('2026-06-07', 'gpt-5.5', 398),
    row('2026-06-06', 'gpt-5.5', 40),
    row('2026-06-07', 'claude-opus-4-8', 12),
  ])
  assert.equal(map.size, 2)
  assert.equal(map.get('2026-06-07')?.length, 2)
  assert.equal(map.get('2026-06-06')?.length, 1)
})

test('sorts each day by message count descending', () => {
  const map = groupModelDaysByDate([
    row('2026-06-07', 'claude-haiku-4-5-20251001', 859),
    row('2026-06-07', '<synthetic>', 2),
    row('2026-06-07', 'claude-opus-4-8', 1397),
  ])
  const models = (map.get('2026-06-07') ?? []).map((r) => r.dimension_key)
  assert.deepEqual(models, ['claude-opus-4-8', 'claude-haiku-4-5-20251001', '<synthetic>'])
})

test('handles undefined and empty input', () => {
  assert.equal(groupModelDaysByDate(undefined).size, 0)
  assert.equal(groupModelDaysByDate([]).size, 0)
})

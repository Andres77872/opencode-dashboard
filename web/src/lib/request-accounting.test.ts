import assert from 'node:assert/strict'
import test from 'node:test'
import {
  isIncompleteRequestAccounting,
  requestAccountingDisclosure,
  traceCoverageLabel,
  usageStatusLabel,
} from './request-accounting.ts'

test('Kimi request accounting discloses unavailable usage as unknown rather than zero', () => {
  const copy = requestAccountingDisclosure({
    usage_recorded: 7,
    usage_recovered: 2,
    usage_unavailable: 3,
    trace_coverage: 'mixed',
  })
  assert.match(copy, /3 requests without persisted usage/i)
  assert.match(copy, /unknown, not zero/i)
})

test('request coverage and usage provenance have stable user-facing labels', () => {
  assert.equal(traceCoverageLabel('successful_only'), 'successful only')
  assert.equal(usageStatusLabel('recorded'), 'Usage recorded')
  assert.equal(usageStatusLabel('recovered'), 'Usage recovered')
  assert.equal(usageStatusLabel('unavailable'), 'Usage unavailable')
  assert.equal(isIncompleteRequestAccounting({ usage_recorded: 1, usage_recovered: 0, usage_unavailable: 0, trace_coverage: 'complete' }), false)
  assert.equal(isIncompleteRequestAccounting({ usage_recorded: 1, usage_recovered: 0, usage_unavailable: 1, trace_coverage: 'complete' }), true)
  assert.equal(isIncompleteRequestAccounting({ usage_recorded: 1, usage_recovered: 0, usage_unavailable: 0, trace_coverage: 'successful_only' }), true)
})

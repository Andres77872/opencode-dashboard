import assert from 'node:assert/strict'
import test from 'node:test'
import {
  ASSISTANT_PREFERENCES_KEY,
  clampAssistantPosition,
  defaultAssistantPosition,
  parseAssistantPreferences,
  readAssistantPreferences,
  writeAssistantPreferences,
  type AssistantStorage,
} from './assistant-position.ts'

test('clamps every edge and respects visual viewport offsets', () => {
  const size = { width: 400, height: 500 }
  const viewport = { width: 1000, height: 800, left: 20, top: 30 }

  assert.deepEqual(clampAssistantPosition({ x: -50, y: -60 }, size, viewport), { x: 32, y: 42 })
  assert.deepEqual(clampAssistantPosition({ x: 9999, y: 9999 }, size, viewport), { x: 608, y: 318 })
  assert.deepEqual(clampAssistantPosition({ x: 200, y: 160 }, size, viewport), { x: 200, y: 160 })
})

test('oversized panels remain anchored to the safe top-left point', () => {
  assert.deepEqual(
    clampAssistantPosition({ x: 100, y: 100 }, { width: 500, height: 700 }, { width: 320, height: 600 }),
    { x: 12, y: 12 },
  )
})

test('default position docks to the viewport-safe lower-right corner', () => {
  assert.deepEqual(
    defaultAssistantPosition({ width: 420, height: 600 }, { width: 1440, height: 900 }),
    { x: 1008, y: 288 },
  )
})

test('preferences parser accepts only the supported UI fields', () => {
  const parsed = parseAssistantPreferences(JSON.stringify({
    open: true,
    minimized: true,
    position: { x: 42, y: 84 },
    privacyAccepted: true,
    messages: [{ role: 'user', content: 'must not persist' }],
  }))

  assert.deepEqual(parsed, {
    open: true,
    minimized: true,
    position: { x: 42, y: 84 },
    privacyAcceptedVersion: null,
  })
  assert.equal('messages' in parsed, false)
})

test('corrupt or non-finite preferences fall back safely', () => {
  assert.deepEqual(parseAssistantPreferences('{broken'), {
    open: false,
    minimized: false,
    position: null,
    privacyAcceptedVersion: null,
  })
  assert.deepEqual(parseAssistantPreferences('{"open":true,"position":{"x":1,"y":null}}'), {
    open: true,
    minimized: false,
    position: null,
    privacyAcceptedVersion: null,
  })
})

test('storage round-trip contains UI preferences and no conversation content', () => {
  let raw: string | null = null
  const storage: AssistantStorage = {
    getItem: (key) => key === ASSISTANT_PREFERENCES_KEY ? raw : null,
    setItem: (key, value) => {
      if (key === ASSISTANT_PREFERENCES_KEY) raw = value
    },
  }

  const preferences = {
    open: true,
    minimized: false,
    position: { x: 123, y: 234 },
    privacyAcceptedVersion: 'analytics-assistant-v1',
  }
  writeAssistantPreferences(preferences, storage)

  assert.deepEqual(readAssistantPreferences(storage), preferences)
  assert.equal(String(raw).includes('message'), false)
  assert.equal(String(raw).includes('content'), false)
})

test('storage failures are non-fatal', () => {
  const storage: AssistantStorage = {
    getItem: () => { throw new Error('blocked') },
    setItem: () => { throw new Error('blocked') },
  }

  assert.doesNotThrow(() => writeAssistantPreferences({
    open: false,
    minimized: false,
    position: null,
    privacyAcceptedVersion: null,
  }, storage))
  assert.deepEqual(readAssistantPreferences(storage), {
    open: false,
    minimized: false,
    position: null,
    privacyAcceptedVersion: null,
  })
})

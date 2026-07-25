import assert from 'node:assert/strict'
import test from 'node:test'
import {
  ASSISTANT_MIN_SIZE,
  ASSISTANT_PREFERENCES_KEY,
  clampAssistantPosition,
  clampAssistantSize,
  defaultAssistantPosition,
  parseAssistantPreferences,
  readAssistantPreferences,
  resizeAssistantFrame,
  writeAssistantPreferences,
  type AssistantFrame,
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
    size: null,
    privacyAcceptedVersion: null,
  })
  assert.equal('messages' in parsed, false)
})

test('corrupt or non-finite preferences fall back safely', () => {
  assert.deepEqual(parseAssistantPreferences('{broken'), {
    open: false,
    minimized: false,
    position: null,
    size: null,
    privacyAcceptedVersion: null,
  })
  assert.deepEqual(parseAssistantPreferences('{"open":true,"position":{"x":1,"y":null}}'), {
    open: true,
    minimized: false,
    position: null,
    size: null,
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
    size: { width: 520, height: 700 },
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
    size: null,
    privacyAcceptedVersion: null,
  }, storage))
  assert.deepEqual(readAssistantPreferences(storage), {
    open: false,
    minimized: false,
    position: null,
    size: null,
    privacyAcceptedVersion: null,
  })
})

test('a stored size survives the round-trip and implausible extents are rejected', () => {
  assert.deepEqual(
    parseAssistantPreferences('{"size":{"width":640,"height":720}}').size,
    { width: 640, height: 720 },
  )
  assert.equal(parseAssistantPreferences('{"size":{"width":0,"height":720}}').size, null)
  assert.equal(parseAssistantPreferences('{"size":{"width":-5,"height":720}}').size, null)
  assert.equal(parseAssistantPreferences('{"size":{"width":640}}').size, null)
  assert.equal(parseAssistantPreferences('{"size":"640x720"}').size, null)
})

test('a size stored on a wide screen is clamped to what the viewport can show', () => {
  assert.deepEqual(
    clampAssistantSize({ width: 900, height: 800 }, { width: 1440, height: 900 }),
    { width: 900, height: 800 },
  )
  assert.deepEqual(
    clampAssistantSize({ width: 900, height: 800 }, { width: 500, height: 600 }),
    { width: 476, height: 576 },
  )
  assert.deepEqual(
    clampAssistantSize({ width: 10, height: 10 }, { width: 1440, height: 900 }),
    ASSISTANT_MIN_SIZE,
  )
})

const FRAME: AssistantFrame = { position: { x: 400, y: 300 }, size: { width: 420, height: 620 } }
const WIDE = { width: 1440, height: 1000 }

test('east and south edges grow the panel without moving its origin', () => {
  assert.deepEqual(resizeAssistantFrame('e', FRAME, { x: 80, y: 0 }, WIDE), {
    position: { x: 400, y: 300 },
    size: { width: 500, height: 620 },
  })
  assert.deepEqual(resizeAssistantFrame('s', FRAME, { x: 0, y: 60 }, WIDE), {
    position: { x: 400, y: 300 },
    size: { width: 420, height: 680 },
  })
})

test('west and north edges pin the opposite edge and move the origin', () => {
  // Left edge moves out by 100, so x drops by 100 and width grows by 100 —
  // the right edge stays at 820 and the bottom stays at 920.
  assert.deepEqual(resizeAssistantFrame('w', FRAME, { x: -100, y: 0 }, WIDE), {
    position: { x: 300, y: 300 },
    size: { width: 520, height: 620 },
  })
  assert.deepEqual(resizeAssistantFrame('n', FRAME, { x: 0, y: -50 }, WIDE), {
    position: { x: 400, y: 250 },
    size: { width: 420, height: 670 },
  })
})

test('corners move both axes at once', () => {
  assert.deepEqual(resizeAssistantFrame('se', FRAME, { x: 40, y: 40 }, WIDE), {
    position: { x: 400, y: 300 },
    size: { width: 460, height: 660 },
  })
  assert.deepEqual(resizeAssistantFrame('nw', FRAME, { x: -40, y: -40 }, WIDE), {
    position: { x: 360, y: 260 },
    size: { width: 460, height: 660 },
  })
  assert.deepEqual(resizeAssistantFrame('ne', FRAME, { x: 40, y: -40 }, WIDE), {
    position: { x: 400, y: 260 },
    size: { width: 460, height: 660 },
  })
  assert.deepEqual(resizeAssistantFrame('sw', FRAME, { x: -40, y: 40 }, WIDE), {
    position: { x: 360, y: 300 },
    size: { width: 460, height: 660 },
  })
})

test('shrinking stops at the minimum extent instead of collapsing', () => {
  // Dragging the east edge far left cannot push it past x + MIN.width.
  assert.deepEqual(resizeAssistantFrame('e', FRAME, { x: -9999, y: 0 }, WIDE), {
    position: { x: 400, y: 300 },
    size: { width: ASSISTANT_MIN_SIZE.width, height: 620 },
  })
  // Dragging the west edge right stops with the right edge still pinned at 820.
  assert.deepEqual(resizeAssistantFrame('w', FRAME, { x: 9999, y: 0 }, WIDE), {
    position: { x: 820 - ASSISTANT_MIN_SIZE.width, y: 300 },
    size: { width: ASSISTANT_MIN_SIZE.width, height: 620 },
  })
  assert.deepEqual(resizeAssistantFrame('n', FRAME, { x: 0, y: 9999 }, WIDE), {
    position: { x: 400, y: 920 - ASSISTANT_MIN_SIZE.height },
    size: { width: 420, height: ASSISTANT_MIN_SIZE.height },
  })
})

test('growing stops at the viewport gap and honours visual viewport offsets', () => {
  assert.deepEqual(resizeAssistantFrame('se', FRAME, { x: 9999, y: 9999 }, WIDE), {
    position: { x: 400, y: 300 },
    size: { width: 1428 - 400, height: 988 - 300 },
  })
  assert.deepEqual(resizeAssistantFrame('nw', FRAME, { x: -9999, y: -9999 }, WIDE), {
    position: { x: 12, y: 12 },
    size: { width: 820 - 12, height: 920 - 12 },
  })
  // With the visual viewport shifted, the safe bounds shift with it.
  assert.deepEqual(
    resizeAssistantFrame('nw', FRAME, { x: -9999, y: -9999 }, { ...WIDE, left: 100, top: 50 }),
    { position: { x: 112, y: 62 }, size: { width: 820 - 112, height: 920 - 62 } },
  )
})

export interface AssistantPosition {
  x: number
  y: number
}

export interface AssistantSize {
  width: number
  height: number
}

export interface AssistantViewport {
  width: number
  height: number
  left?: number
  top?: number
}

/** A panel frame is its origin plus its extent; resizing moves both together. */
export interface AssistantFrame {
  position: AssistantPosition
  size: AssistantSize
}

/** Which edge or corner a resize gesture grabbed. */
export type AssistantResizeEdge = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw'

export interface AssistantPreferences {
  open: boolean
  minimized: boolean
  position: AssistantPosition | null
  /** null means "use the stylesheet default"; a number pair overrides it. */
  size: AssistantSize | null
  privacyAcceptedVersion: string | null
}

export interface AssistantStorage {
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
}

export const ASSISTANT_EDGE_GAP = 12
export const ASSISTANT_PREFERENCES_KEY = 'ocd:assistant-ui:v2'

// Below this the composer, status line, and a single message stop coexisting.
// Clamping to it is also what makes a "reset size" control unnecessary: the
// panel can never be dragged down to something unusable.
export const ASSISTANT_MIN_SIZE: AssistantSize = { width: 320, height: 360 }

export const DEFAULT_ASSISTANT_PREFERENCES: AssistantPreferences = {
  open: false,
  minimized: false,
  position: null,
  size: null,
  privacyAcceptedVersion: null,
}

function finiteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function clamp(min: number, value: number, max: number): number {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

/** Keeps a fixed-position assistant surface fully inside the visual viewport. */
export function clampAssistantPosition(
  position: AssistantPosition,
  size: AssistantSize,
  viewport: AssistantViewport,
  gap = ASSISTANT_EDGE_GAP,
): AssistantPosition {
  const left = viewport.left ?? 0
  const top = viewport.top ?? 0
  const minX = left + gap
  const minY = top + gap
  const maxX = Math.max(minX, left + viewport.width - size.width - gap)
  const maxY = Math.max(minY, top + viewport.height - size.height - gap)

  return {
    x: Math.min(maxX, Math.max(minX, position.x)),
    y: Math.min(maxY, Math.max(minY, position.y)),
  }
}

/** Default dock is the lower-right corner with the same viewport-safe gap. */
export function defaultAssistantPosition(
  size: AssistantSize,
  viewport: AssistantViewport,
  gap = ASSISTANT_EDGE_GAP,
): AssistantPosition {
  const left = viewport.left ?? 0
  const top = viewport.top ?? 0
  return clampAssistantPosition(
    {
      x: left + viewport.width - size.width - gap,
      y: top + viewport.height - size.height - gap,
    },
    size,
    viewport,
    gap,
  )
}

/**
 * Keeps a user-chosen size between the minimum that stays usable and what the
 * current viewport can actually show. A size stored on a wide monitor has to
 * survive being reopened on a laptop.
 */
export function clampAssistantSize(
  size: AssistantSize,
  viewport: AssistantViewport,
  gap = ASSISTANT_EDGE_GAP,
): AssistantSize {
  return {
    width: clamp(ASSISTANT_MIN_SIZE.width, size.width, viewport.width - gap * 2),
    height: clamp(ASSISTANT_MIN_SIZE.height, size.height, viewport.height - gap * 2),
  }
}

/**
 * Applies a resize gesture by moving the grabbed edges rather than by adjusting
 * width and height. Dragging a west or north edge then pins the opposite edge
 * and shifts the origin for free, and every clamp — minimum extent and viewport
 * bounds alike — is expressed once, as a bound on a single edge coordinate.
 */
export function resizeAssistantFrame(
  edge: AssistantResizeEdge,
  origin: AssistantFrame,
  delta: AssistantPosition,
  viewport: AssistantViewport,
  gap = ASSISTANT_EDGE_GAP,
): AssistantFrame {
  const minX = (viewport.left ?? 0) + gap
  const minY = (viewport.top ?? 0) + gap
  const maxX = (viewport.left ?? 0) + viewport.width - gap
  const maxY = (viewport.top ?? 0) + viewport.height - gap

  let left = origin.position.x
  let top = origin.position.y
  let right = left + origin.size.width
  let bottom = top + origin.size.height

  if (edge.includes('w')) left = clamp(minX, left + delta.x, right - ASSISTANT_MIN_SIZE.width)
  if (edge.includes('e')) right = clamp(left + ASSISTANT_MIN_SIZE.width, right + delta.x, maxX)
  if (edge.includes('n')) top = clamp(minY, top + delta.y, bottom - ASSISTANT_MIN_SIZE.height)
  if (edge.includes('s')) bottom = clamp(top + ASSISTANT_MIN_SIZE.height, bottom + delta.y, maxY)

  return {
    position: { x: left, y: top },
    size: { width: right - left, height: bottom - top },
  }
}

export function parseAssistantPreferences(raw: string | null): AssistantPreferences {
  if (!raw) return { ...DEFAULT_ASSISTANT_PREFERENCES }

  let value: unknown
  try {
    value = JSON.parse(raw)
  } catch {
    return { ...DEFAULT_ASSISTANT_PREFERENCES }
  }

  if (!isRecord(value)) return { ...DEFAULT_ASSISTANT_PREFERENCES }

  const rawPosition = value.position
  const position = isRecord(rawPosition) && finiteNumber(rawPosition.x) && finiteNumber(rawPosition.y)
    ? { x: rawPosition.x, y: rawPosition.y }
    : null

  // A zero or negative extent would render an invisible panel with no way back,
  // so an implausible stored size falls back to the stylesheet default.
  const rawSize = value.size
  const size = isRecord(rawSize)
    && finiteNumber(rawSize.width) && rawSize.width > 0
    && finiteNumber(rawSize.height) && rawSize.height > 0
    ? { width: rawSize.width, height: rawSize.height }
    : null

  return {
    open: value.open === true,
    minimized: value.minimized === true,
    position,
    size,
    privacyAcceptedVersion: typeof value.privacyAcceptedVersion === 'string'
      ? value.privacyAcceptedVersion.slice(0, 64)
      : null,
  }
}

function browserStorage(): AssistantStorage | null {
  try {
    return typeof localStorage === 'undefined' ? null : localStorage
  } catch {
    return null
  }
}

export function readAssistantPreferences(storage: AssistantStorage | null = browserStorage()): AssistantPreferences {
  if (!storage) return { ...DEFAULT_ASSISTANT_PREFERENCES }
  try {
    return parseAssistantPreferences(storage.getItem(ASSISTANT_PREFERENCES_KEY))
  } catch {
    return { ...DEFAULT_ASSISTANT_PREFERENCES }
  }
}

export function writeAssistantPreferences(
  preferences: AssistantPreferences,
  storage: AssistantStorage | null = browserStorage(),
): void {
  if (!storage) return
  try {
    storage.setItem(ASSISTANT_PREFERENCES_KEY, JSON.stringify(preferences))
  } catch {
    // UI persistence is best-effort when storage is blocked or full.
  }
}

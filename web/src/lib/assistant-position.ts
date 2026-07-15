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

export interface AssistantPreferences {
  open: boolean
  minimized: boolean
  position: AssistantPosition | null
  privacyAcceptedVersion: string | null
}

export interface AssistantStorage {
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
}

export const ASSISTANT_EDGE_GAP = 12
export const ASSISTANT_PREFERENCES_KEY = 'ocd:assistant-ui:v2'

export const DEFAULT_ASSISTANT_PREFERENCES: AssistantPreferences = {
  open: false,
  minimized: false,
  position: null,
  privacyAcceptedVersion: null,
}

function finiteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
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

  return {
    open: value.open === true,
    minimized: value.minimized === true,
    position,
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

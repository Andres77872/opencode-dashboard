import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  type RefObject,
} from 'react'
import { createPortal } from 'react-dom'
import { useLocation } from 'react-router-dom'
import { Icon } from '../vael/icon'
import { Markdown } from './markdown'
import { useDashboardContext } from '../layout/dashboard-context'
import {
  deleteAssistantSession,
  getAssistantSession,
  getAssistantSessions,
  getAssistantStatus,
  streamAssistantChat,
} from '../../lib/api'
import {
  clampAssistantPosition,
  clampAssistantSize,
  defaultAssistantPosition,
  readAssistantPreferences,
  resizeAssistantFrame,
  writeAssistantPreferences,
  type AssistantFrame,
  type AssistantPosition,
  type AssistantPreferences,
  type AssistantResizeEdge,
  type AssistantViewport,
} from '../../lib/assistant-position'
import { usePeriodState } from '../../lib/use-period-state'
import { boundAssistantHistory, dropAbandonedTurns } from '../../lib/assistant-history'
import { conversationToMarkdown } from '../../lib/assistant-transcript'
import { CopyButton } from './copy-button'
import { ActivityTrail } from './activity'
import {
  activitiesFromRecords,
  activitiesFromStoredMessage,
  applyStreamEvent,
  countToolActivities,
  formatDurationMs,
  humanizeAgentName,
  settleActivities,
  usageSummary,
  type Activity,
} from '../../lib/assistant-activity'
import type {
  AssistantChatSessionSummary,
  AssistantMessage,
  AssistantRequestContext,
  AssistantStatusResponse,
  AssistantStreamEvent,
  AssistantUsage,
} from '../../types/assistant'

const PANEL_ID = 'analytics-assistant-panel'
const MAX_PROMPT_LENGTH = 4_000
const STATUS_RETRY_DELAYS_MS = [0, 10_000, 30_000] as const
const ASSISTANT_ROUTES = new Set([
  '/overview', '/daily', '/models', '/tools', '/projects', '/sessions', '/config',
])

const RESIZE_EDGES: readonly AssistantResizeEdge[] = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw']
const COMPOSER_MAX_HEIGHT = 150

const QUICK_PROMPTS = [
  'Summarize my usage for this period.',
  'What changed most recently?',
  'Which models and projects use the most tokens?',
  'Which tools fail most often?',
] as const

interface DisplayMessage extends AssistantMessage {
  id: number
  streaming?: boolean
  /**
   * Set when a turn ended without a complete, signed answer. The partial text
   * stays readable, but the turn is excluded from what is replayed to the model.
   */
  stopped?: 'stopped' | 'failed'
  activities?: Activity[]
  usage?: AssistantUsage
  rounds?: number
  durationMs?: number
}

interface DragState {
  pointerId: number
  startX: number
  startY: number
  origin: AssistantPosition
}

interface ResizeState {
  pointerId: number
  edge: AssistantResizeEdge
  startX: number
  startY: number
  origin: AssistantFrame
}

function currentViewport(): AssistantViewport {
  const visual = window.visualViewport
  return visual
    ? {
        width: visual.width,
        height: visual.height,
        left: visual.offsetLeft,
        top: visual.offsetTop,
      }
    : { width: window.innerWidth, height: window.innerHeight }
}

function periodHint(state: ReturnType<typeof usePeriodState>): string {
  if (state.mode === 'custom' && state.customRange?.from) {
    return `${state.customRange.from} to ${state.customRange.to ?? 'now'}`
  }
  return state.preset
}

function safeTimezone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    return undefined
  }
}

function routeHint(pathname: string): string | undefined {
  const normalized = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname
  return ASSISTANT_ROUTES.has(normalized) ? normalized : undefined
}

function samePosition(left: AssistantPosition | null, right: AssistantPosition): boolean {
  return left?.x === right.x && left.y === right.y
}

function providerLabel(value: string): string {
  return value.trim().toLowerCase() === 'minimax' ? 'MiniMax' : value
}

function formatSessionTime(updatedMs: number): string {
  const elapsed = Date.now() - updatedMs
  if (elapsed < 60_000) return 'just now'
  if (elapsed < 3_600_000) return `${Math.floor(elapsed / 60_000)} min ago`
  if (elapsed < 86_400_000) return `${Math.floor(elapsed / 3_600_000)} h ago`
  return new Date(updatedMs).toLocaleDateString()
}

/** Describes a saved conversation without needing to open it. */
function sessionMetaLine(session: AssistantChatSessionSummary): string {
  const turns = session.turn_count ?? Math.ceil(session.message_count / 2)
  const parts = [
    formatSessionTime(session.updated_ms),
    `${turns} ${turns === 1 ? 'question' : 'questions'}`,
  ]
  if (session.subagent_count) {
    parts.push(`${session.subagent_count} ${session.subagent_count === 1 ? 'specialist' : 'specialists'}`)
  }
  const usage = usageSummary(session.usage)
  if (usage) parts.push(usage)
  return parts.join(' · ')
}

function AssistantGlyph({ size = 20 }: { size?: number }) {
  return (
    <span className="analytics-assistant-glyph" aria-hidden="true">
      <Icon name="message-square" size={size} />
      <span className="analytics-assistant-glyph-spark" />
    </span>
  )
}

function HeaderButton({
  label,
  icon,
  active,
  onClick,
}: {
  label: string
  icon: 'refresh' | 'chevron-down' | 'x' | 'clock' | 'copy' | 'check'
  active?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      className={`analytics-assistant-header-button${active ? ' active' : ''}`}
      aria-label={label}
      aria-pressed={active}
      title={label}
      onClick={onClick}
    >
      <Icon name={icon} size={15} />
    </button>
  )
}

function PrivacyDisclosure({
  status,
  onAccept,
  onCancel,
  continueRef,
}: {
  status: AssistantStatusResponse
  onAccept: () => void
  onCancel: () => void
  continueRef: RefObject<HTMLButtonElement | null>
}) {
  return (
    <div className="analytics-assistant-disclosure">
      <div className="analytics-assistant-disclosure-icon">
        <Icon name="info" size={22} />
      </div>
      <div>
        <h3>Before you start</h3>
        <p>
          Your prompts and the aggregate usage metrics requested to answer them are sent to MiniMax,
          including the model, provider, and tool names behind those numbers and project names
          without their directories. Integrity audits can also send normalized source scan counts,
          request-accounting evidence, and sanitized cache-health flags.
          Transcripts, prompts, session titles, file paths, raw diagnostics or errors, timestamps,
          request/session identifiers, raw configuration, and secrets are not included.
        </p>
        {status.privacy_notice && <p className="analytics-assistant-privacy-note">{status.privacy_notice}</p>}
      </div>
      {status.capabilities.length > 0 && (
        <div className="analytics-assistant-capabilities" aria-label="Assistant capabilities">
          {status.capabilities.map((capability) => <span key={capability}>{capability}</span>)}
        </div>
      )}
      <div className="analytics-assistant-disclosure-actions">
        <button type="button" className="analytics-assistant-button secondary" onClick={onCancel}>Not now</button>
        <button ref={continueRef} type="button" className="analytics-assistant-button primary" onClick={onAccept}>Continue</button>
      </div>
    </div>
  )
}

/** Reports what one answer cost once the turn is complete. */
function MessageFooter({ message }: { message: DisplayMessage }) {
  const activities = message.activities ?? []
  const calls = countToolActivities(activities)
  const specialists = activities.filter((activity) => activity.kind === 'specialist').length
  const parts = [
    message.rounds ? `${message.rounds} ${message.rounds === 1 ? 'round' : 'rounds'}` : '',
    calls ? `${calls} ${calls === 1 ? 'call' : 'calls'}` : '',
    specialists ? `${specialists} ${specialists === 1 ? 'specialist' : 'specialists'}` : '',
    formatDurationMs(message.durationMs),
    usageSummary(message.usage),
  ].filter(Boolean)
  if (parts.length === 0) return null
  return <div className="analytics-assistant-message-footer">{parts.join(' · ')}</div>
}

function MessageRow({ message }: { message: DisplayMessage }) {
  const assistant = message.role === 'assistant'
  const hasContent = message.content.length > 0
  const activities = assistant ? message.activities ?? [] : []
  // The copy target is message.content itself — the Markdown source the model
  // produced, which the renderer only ever reads. Copying never touches the DOM.
  const copyable = hasContent && !message.streaming
  return (
    <article
      className={`analytics-assistant-message ${assistant ? 'assistant' : 'user'}${message.streaming ? ' streaming' : ''}`}
      aria-busy={message.streaming || undefined}
    >
      <div className="analytics-assistant-message-meta">
        <span>{assistant ? 'Analytics assistant' : 'You'}</span>
        {message.streaming && <span className="analytics-assistant-stream-label">Live</span>}
        {message.stopped && (
          <span className="analytics-assistant-stopped-label">
            {message.stopped === 'stopped' ? 'Stopped' : 'Incomplete'}
          </span>
        )}
      </div>
      <ActivityTrail activities={activities} />
      {hasContent && (
        <div className={`analytics-assistant-message-content${assistant ? ' markdown' : ''}`}>
          {assistant ? (
            <Markdown content={message.content} streaming={message.streaming} />
          ) : (
            message.content
          )}
        </div>
      )}
      {message.streaming && !hasContent && activities.length === 0 && (
        <div className="analytics-assistant-thinking compact" role="status">
          <AssistantGlyph size={15} />
          <span>Starting the report</span>
          <i /><i /><i />
        </div>
      )}
      {copyable && (
        <div className="analytics-assistant-message-trailer">
          {assistant && <MessageFooter message={message} />}
          <div className="analytics-assistant-message-actions">
            <CopyButton value={message.content} label="Copy markdown" copiedLabel="Copied markdown" />
          </div>
        </div>
      )}
    </article>
  )
}

function Welcome({ status, onPrompt }: { status: AssistantStatusResponse; onPrompt: (prompt: string) => void }) {
  const specialists = status.specialists ?? []
  return (
    <div className="analytics-assistant-welcome">
      <AssistantGlyph size={22} />
      <div>
        <h3>Ask about your agent usage</h3>
        <p>I can build reports and surface patterns across the analytics sources available to this dashboard.</p>
      </div>
      <div className="analytics-assistant-quick-prompts" aria-label="Suggested questions">
        {QUICK_PROMPTS.map((prompt) => (
          <button key={prompt} type="button" onClick={() => onPrompt(prompt)}>{prompt}</button>
        ))}
      </div>
      {specialists.length > 0 && (
        <div className="analytics-assistant-specialist-roster">
          <span>Deeper questions are handed to specialists</span>
          <ul>
            {specialists.map((specialist) => (
              <li key={specialist.id}>
                <strong>{specialist.title || humanizeAgentName(specialist.id)}</strong>
                <span>{specialist.purpose}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

function SessionHistory({
  sessions,
  loading,
  error,
  activeSessionId,
  onSelect,
  onDelete,
  onClose,
}: {
  sessions: AssistantChatSessionSummary[]
  loading: boolean
  error: string | null
  activeSessionId: string | null
  onSelect: (id: string) => void
  onDelete: (id: string) => void
  onClose: () => void
}) {
  return (
    <div className="analytics-assistant-history" aria-label="Saved conversations">
      <div className="analytics-assistant-history-header">
        <Icon name="clock" size={14} />
        <strong>Saved conversations</strong>
        <button type="button" className="analytics-assistant-history-close" onClick={onClose}>Back to chat</button>
      </div>
      {loading && <p className="analytics-assistant-history-empty">Loading saved conversations…</p>}
      {!loading && error && <p className="analytics-assistant-history-empty error">{error}</p>}
      {!loading && !error && sessions.length === 0 && (
        <p className="analytics-assistant-history-empty">No saved conversations yet. Completed chats are saved automatically.</p>
      )}
      {!loading && !error && sessions.length > 0 && (
        <ul className="analytics-assistant-history-list">
          {sessions.map((session) => (
            <li key={session.id} className={session.id === activeSessionId ? 'active' : undefined}>
              <button
                type="button"
                className="analytics-assistant-history-item"
                onClick={() => onSelect(session.id)}
                title={session.title}
              >
                <span className="analytics-assistant-history-title">{session.title}</span>
                <span className="analytics-assistant-history-meta">{sessionMetaLine(session)}</span>
              </button>
              <button
                type="button"
                className="analytics-assistant-history-delete"
                aria-label={`Delete conversation ${session.title}`}
                title="Delete conversation"
                onClick={() => onDelete(session.id)}
              >
                <Icon name="x" size={13} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function AnalyticsAssistant() {
  const location = useLocation()
  const periodState = usePeriodState()
  const { selectedSourceId } = useDashboardContext()
  const [status, setStatus] = useState<AssistantStatusResponse | null>(null)
  const [preferences, setPreferences] = useState<AssistantPreferences>(() => readAssistantPreferences())
  const [messages, setMessages] = useState<DisplayMessage[]>([])
  const [draft, setDraft] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [failedPrompt, setFailedPrompt] = useState<string | null>(null)
  const [liveMessage, setLiveMessage] = useState('')
  const [completedAnnouncement, setCompletedAnnouncement] = useState('')
  const [dragging, setDragging] = useState(false)
  const [resizing, setResizing] = useState(false)
  const [atBottom, setAtBottom] = useState(true)
  const [sessionId, setSessionId] = useState<string | null>(null)
  const [sessionTitle, setSessionTitle] = useState<string | null>(null)
  const [sessionUsage, setSessionUsage] = useState<AssistantUsage | undefined>(undefined)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [historySessions, setHistorySessions] = useState<AssistantChatSessionSummary[]>([])
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const launcherRef = useRef<HTMLButtonElement>(null)
  const privacyContinueRef = useRef<HTMLButtonElement>(null)
  const restoreButtonRef = useRef<HTMLButtonElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const followStreamRef = useRef(true)
  const dragRef = useRef<DragState | null>(null)
  const resizeRef = useRef<ResizeState | null>(null)
  // Stop and error recovery run from callbacks that must not be rebuilt on every
  // keystroke, but still need to know whether the composer holds unsent text and
  // whether the abandoned answer produced anything worth keeping.
  const draftRef = useRef('')
  const messagesRef = useRef<DisplayMessage[]>([])
  const abortRef = useRef<AbortController | null>(null)
  const historyAbortRef = useRef<AbortController | null>(null)
  const pendingPromptRef = useRef<{ id: number; responseID: number; content: string } | null>(null)
  const requestIDRef = useRef(0)
  const messageIDRef = useRef(1)
  const restoreLauncherFocusRef = useRef(false)

  useEffect(() => {
    let stopped = false
    let controller: AbortController | null = null
    let timer: number | undefined

    const scheduleProbe = (attempt: number) => {
      if (stopped || attempt >= STATUS_RETRY_DELAYS_MS.length) return
      const probe = () => {
        if (stopped) return
        controller = new AbortController()
        getAssistantStatus(controller.signal)
          .then((next) => {
            if (stopped || controller?.signal.aborted) return
            setStatus(next)
            if (!next.available) scheduleProbe(attempt + 1)
          })
          .catch(() => {
            if (!stopped && !controller?.signal.aborted) scheduleProbe(attempt + 1)
          })
      }
      const delay = STATUS_RETRY_DELAYS_MS[attempt]
      if (delay === 0) probe()
      else timer = window.setTimeout(probe, delay)
    }

    scheduleProbe(0)
    return () => {
      stopped = true
      controller?.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [])

  useEffect(() => {
    const timer = window.setTimeout(() => writeAssistantPreferences(preferences), 120)
    return () => window.clearTimeout(timer)
  }, [preferences])

  useEffect(() => () => {
    requestIDRef.current += 1
    abortRef.current?.abort()
    historyAbortRef.current?.abort()
  }, [])

  const context = useMemo<AssistantRequestContext>(() => {
    const route = routeHint(location.pathname)
    return {
      route,
      source: route === '/overview' ? undefined : selectedSourceId,
      period: periodHint(periodState),
      timezone: safeTimezone(),
    }
  }, [location.pathname, periodState, selectedSourceId])
  const consentAccepted = preferences.privacyAcceptedVersion === status?.consent_version
  const sessionsPersisted = status?.sessions_persisted === true

  const clampToPanel = useCallback((position: AssistantPosition): AssistantPosition => {
    const element = panelRef.current
    if (!element) return position
    const rect = element.getBoundingClientRect()
    return clampAssistantPosition(position, { width: rect.width, height: rect.height }, currentViewport())
  }, [])

  // The panel only exists once status confirms availability, so placement must
  // also re-run then. Without that dependency a position saved on a wider
  // screen is never re-clamped and the panel stays parked off-viewport.
  const panelVisible = status?.available === true && preferences.open
  useLayoutEffect(() => {
    if (!panelVisible) return
    const element = panelRef.current
    if (!element) return

    const place = () => {
      // A live resize already produces a clamped frame, and the ResizeObserver
      // below fires on every one of its frames. Re-clamping from here would
      // fight the gesture it is observing.
      if (resizeRef.current) return
      const rect = element.getBoundingClientRect()
      const viewport = currentViewport()
      setPreferences((current) => {
        const desired = current.position ?? defaultAssistantPosition(
          { width: rect.width, height: rect.height },
          viewport,
        )
        const next = clampAssistantPosition(desired, { width: rect.width, height: rect.height }, viewport)
        const size = current.size ? clampAssistantSize(current.size, viewport) : null
        const sizeChanged = size !== null && current.size !== null
          && (size.width !== current.size.width || size.height !== current.size.height)
        if (samePosition(current.position, next) && !sizeChanged) return current
        return { ...current, position: next, size }
      })
    }

    const frame = window.requestAnimationFrame(place)
    const resizeObserver = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(place)
    resizeObserver?.observe(element)
    window.addEventListener('resize', place)
    window.visualViewport?.addEventListener('resize', place)
    window.visualViewport?.addEventListener('scroll', place)

    return () => {
      window.cancelAnimationFrame(frame)
      resizeObserver?.disconnect()
      window.removeEventListener('resize', place)
      window.visualViewport?.removeEventListener('resize', place)
      window.visualViewport?.removeEventListener('scroll', place)
    }
  }, [panelVisible, preferences.minimized, consentAccepted])

  useEffect(() => {
    if (!preferences.open) return
    const frame = window.requestAnimationFrame(() => {
      if (preferences.minimized) {
        restoreButtonRef.current?.focus()
      } else if (consentAccepted) {
        inputRef.current?.focus()
      } else {
        privacyContinueRef.current?.focus()
      }
    })
    return () => window.cancelAnimationFrame(frame)
  }, [consentAccepted, preferences.open, preferences.minimized])

  useEffect(() => {
    if (preferences.open || !restoreLauncherFocusRef.current) return
    restoreLauncherFocusRef.current = false
    const frame = window.requestAnimationFrame(() => launcherRef.current?.focus())
    return () => window.cancelAnimationFrame(frame)
  }, [preferences.open])

  useEffect(() => {
    draftRef.current = draft
  }, [draft])

  useEffect(() => {
    messagesRef.current = messages
  }, [messages])

  // Grow the composer with its content instead of parking it at a fixed height
  // with an inner scrollbar. Resetting to auto first lets it shrink again when
  // the draft is cleared on send.
  useLayoutEffect(() => {
    const element = inputRef.current
    if (!element) return
    element.style.height = 'auto'
    element.style.height = `${Math.min(element.scrollHeight, COMPOSER_MAX_HEIGHT)}px`
  }, [draft, preferences.open, preferences.minimized, consentAccepted, historyOpen])

  useEffect(() => {
    const list = messageListRef.current
    if (list && followStreamRef.current) list.scrollTop = list.scrollHeight
  }, [messages, preferences.minimized, preferences.open, sending])

  const updateMessageScrollFollow = useCallback(() => {
    const list = messageListRef.current
    if (!list) return
    const following = list.scrollHeight - list.scrollTop - list.clientHeight < 40
    followStreamRef.current = following
    setAtBottom(following)
  }, [])

  const jumpToLatest = useCallback(() => {
    const list = messageListRef.current
    if (!list) return
    followStreamRef.current = true
    setAtBottom(true)
    list.scrollTo({ top: list.scrollHeight, behavior: 'smooth' })
  }, [])

  useEffect(() => {
    if (!preferences.open || preferences.minimized) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) {
        setPreferences((current) => ({ ...current, minimized: true }))
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [preferences.open, preferences.minimized])

  const beginDrag = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || (event.target as Element).closest('button')) return
    const element = panelRef.current
    if (!element) return
    const rect = element.getBoundingClientRect()
    dragRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      origin: { x: rect.left, y: rect.top },
    }
    event.currentTarget.setPointerCapture(event.pointerId)
    event.preventDefault()
    setDragging(true)
  }, [])

  const moveDrag = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== event.pointerId) return
    const next = clampToPanel({
      x: drag.origin.x + event.clientX - drag.startX,
      y: drag.origin.y + event.clientY - drag.startY,
    })
    setPreferences((current) => samePosition(current.position, next) ? current : { ...current, position: next })
  }, [clampToPanel])

  const endDrag = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current
    if (!drag || drag.pointerId !== event.pointerId) return
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    dragRef.current = null
    setDragging(false)
  }, [])

  // The origin frame is read from the live rect rather than from preferences, so
  // the first resize works while the size is still the stylesheet default.
  const beginResize = useCallback((edge: AssistantResizeEdge) => (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return
    const element = panelRef.current
    if (!element) return
    const rect = element.getBoundingClientRect()
    resizeRef.current = {
      pointerId: event.pointerId,
      edge,
      startX: event.clientX,
      startY: event.clientY,
      origin: {
        position: { x: rect.left, y: rect.top },
        size: { width: rect.width, height: rect.height },
      },
    }
    event.currentTarget.setPointerCapture(event.pointerId)
    event.preventDefault()
    event.stopPropagation()
    setResizing(true)
  }, [])

  const moveResize = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const resize = resizeRef.current
    if (!resize || resize.pointerId !== event.pointerId) return
    const frame = resizeAssistantFrame(
      resize.edge,
      resize.origin,
      { x: event.clientX - resize.startX, y: event.clientY - resize.startY },
      currentViewport(),
    )
    setPreferences((current) => ({ ...current, position: frame.position, size: frame.size }))
  }, [])

  const endResize = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    const resize = resizeRef.current
    if (!resize || resize.pointerId !== event.pointerId) return
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
    resizeRef.current = null
    setResizing(false)
  }, [])

  const moveWithKeyboard = useCallback((event: ReactKeyboardEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget) return
    const directions: Record<string, AssistantPosition> = {
      ArrowLeft: { x: -1, y: 0 },
      ArrowRight: { x: 1, y: 0 },
      ArrowUp: { x: 0, y: -1 },
      ArrowDown: { x: 0, y: 1 },
    }
    const direction = directions[event.key]
    if (!direction) return
    const rect = panelRef.current?.getBoundingClientRect()
    if (!rect) return
    const distance = event.shiftKey ? 4 : 16
    const next = clampToPanel({
      x: rect.left + direction.x * distance,
      y: rect.top + direction.y * distance,
    })
    event.preventDefault()
    setPreferences((current) => ({ ...current, position: next }))
  }, [clampToPanel])

  /**
   * Ends the in-flight turn. Whatever streamed in is left on screen — an answer
   * cut short is still worth reading — and the composer is never overwritten,
   * because by now it may hold a follow-up typed during the response.
   */
  const stopRequest = useCallback((message = 'Response stopped.') => {
    requestIDRef.current += 1
    abortRef.current?.abort()
    abortRef.current = null
    const pending = pendingPromptRef.current
    pendingPromptRef.current = null
    if (pending) {
      const partial = messagesRef.current.find((item) => item.id === pending.responseID)
      if (partial && partial.content.trim() !== '') {
        setMessages((current) => current.map((item) => (item.id === pending.responseID
          ? {
              ...item,
              streaming: false,
              stopped: 'stopped' as const,
              activities: settleActivities(item.activities ?? []),
            }
          : item)))
      } else {
        // Nothing was produced, so there is no turn to preserve. Offer the
        // prompt back through the composer only when it is empty; otherwise the
        // Retry button carries it instead of clobbering what was typed.
        setMessages((current) => current.filter((item) => item.id !== pending.id && item.id !== pending.responseID))
        if (draftRef.current.trim()) setFailedPrompt(pending.content)
        else setDraft(pending.content)
      }
    }
    setSending(false)
    setLiveMessage(message)
  }, [])

  const closeAssistant = useCallback(() => {
    if (abortRef.current) stopRequest('')
    restoreLauncherFocusRef.current = true
    setPreferences((current) => ({ ...current, open: false, minimized: false }))
  }, [stopRequest])

  const resetConversation = useCallback(() => {
    if (abortRef.current) stopRequest('')
    setMessages([])
    setDraft('')
    setError(null)
    setFailedPrompt(null)
    setCompletedAnnouncement('')
    setSessionId(null)
    setSessionTitle(null)
    setSessionUsage(undefined)
    setHistoryOpen(false)
    followStreamRef.current = true
    setLiveMessage('New conversation started.')
  }, [stopRequest])

  const resetPosition = useCallback(() => {
    const element = panelRef.current
    if (!element) return
    const rect = element.getBoundingClientRect()
    const next = defaultAssistantPosition(
      { width: rect.width, height: rect.height },
      currentViewport(),
    )
    setPreferences((current) => ({ ...current, position: next }))
  }, [])

  const refreshHistory = useCallback(() => {
    historyAbortRef.current?.abort()
    const controller = new AbortController()
    historyAbortRef.current = controller
    setHistoryLoading(true)
    setHistoryError(null)
    getAssistantSessions(controller.signal)
      .then((response) => {
        if (controller.signal.aborted) return
        setHistorySessions(response.sessions)
        setHistoryLoading(false)
      })
      .catch((caught) => {
        if (controller.signal.aborted) return
        setHistoryError(caught instanceof Error ? caught.message : 'Saved conversations are unavailable.')
        setHistoryLoading(false)
      })
  }, [])

  const toggleHistory = useCallback(() => {
    setHistoryOpen((open) => {
      if (!open) refreshHistory()
      return !open
    })
  }, [refreshHistory])

  const loadSession = useCallback((id: string) => {
    if (abortRef.current) stopRequest('')
    historyAbortRef.current?.abort()
    const controller = new AbortController()
    historyAbortRef.current = controller
    setHistoryLoading(true)
    setHistoryError(null)
    getAssistantSession(id, controller.signal)
      .then((detail) => {
        if (controller.signal.aborted) return
        // A restored conversation must show exactly what the live turn showed:
        // its evidence trail, its specialist findings, and what it cost.
        const restored: DisplayMessage[] = detail.messages.map((message) => ({
          id: messageIDRef.current++,
          role: message.role,
          content: message.content,
          signature: message.signature,
          ...(message.role === 'assistant' ? {
            activities: activitiesFromStoredMessage(message),
            usage: message.usage,
            rounds: message.rounds,
            durationMs: message.duration_ms,
          } : {}),
        }))
        setMessages(restored)
        setSessionId(detail.session.id)
        setSessionTitle(detail.session.title)
        setSessionUsage(detail.session.usage)
        setError(null)
        setHistoryOpen(false)
        setHistoryLoading(false)
        followStreamRef.current = true
        setLiveMessage(`Loaded conversation: ${detail.session.title}`)
      })
      .catch((caught) => {
        if (controller.signal.aborted) return
        setHistoryError(caught instanceof Error ? caught.message : 'The conversation could not be loaded.')
        setHistoryLoading(false)
      })
  }, [stopRequest])

  const removeSession = useCallback((id: string) => {
    historyAbortRef.current?.abort()
    const controller = new AbortController()
    historyAbortRef.current = controller
    deleteAssistantSession(id, controller.signal)
      .then(() => {
        if (controller.signal.aborted) return
        setHistorySessions((current) => current.filter((session) => session.id !== id))
        setSessionId((current) => {
          if (current !== id) return current
          setMessages([])
          setSessionTitle(null)
          setSessionUsage(undefined)
          return null
        })
        setLiveMessage('Conversation deleted.')
      })
      .catch((caught) => {
        if (controller.signal.aborted) return
        setHistoryError(caught instanceof Error ? caught.message : 'The conversation could not be deleted.')
      })
  }, [])

  const submitPrompt = useCallback(async (rawPrompt: string) => {
    const prompt = rawPrompt.trim()
    if (!prompt || sending || abortRef.current || !consentAccepted || !status) return

    const userMessage: DisplayMessage = {
      id: messageIDRef.current++,
      role: 'user',
      content: prompt,
    }
    const assistantMessage: DisplayMessage = {
      id: messageIDRef.current++,
      role: 'assistant',
      content: '',
      streaming: true,
      activities: [],
    }
    // Abandoned turns are dropped as whole pairs before bounding: their answers
    // carry no signature (which the backend rejects) and removing only the
    // answer would leave two adjacent user turns, which boundAssistantHistory
    // treats as the end of the replayable suffix.
    const requestMessages = boundAssistantHistory(
      dropAbandonedTurns([...messages, userMessage])
        .map(({ role, content, signature }) => ({ role, content, signature })),
    )
    const requestSessionID = sessionId ?? undefined
    const controller = new AbortController()
    const requestID = ++requestIDRef.current
    abortRef.current = controller
    pendingPromptRef.current = { id: userMessage.id, responseID: assistantMessage.id, content: prompt }
    followStreamRef.current = true
    setHistoryOpen(false)
    setMessages((current) => [...current, userMessage, assistantMessage])
    setDraft('')
    setError(null)
    setFailedPrompt(null)
    setCompletedAnnouncement('')
    setLiveMessage('Analyzing usage data…')
    setSending(true)

    try {
      const onStreamEvent = (event: AssistantStreamEvent) => {
        if (controller.signal.aborted || requestID !== requestIDRef.current) return

        const updateResponse = (update: (message: DisplayMessage) => DisplayMessage) => {
          setMessages((current) => current.map((message) => (
            message.id === assistantMessage.id ? update(message) : message
          )))
        }

        const foldActivity = () => {
          updateResponse((message) => ({
            ...message,
            activities: applyStreamEvent(message.activities ?? [], event),
          }))
        }

        switch (event.type) {
          case 'start':
            setLiveMessage(`Connected to ${event.model}.`)
            break
          case 'round_start':
            if (event.round > 1 && !event.parent_call_id) {
              setLiveMessage(`Reasoning over the evidence (round ${event.round})…`)
            }
            break
          case 'content_delta':
            if (event.delta !== '') {
              updateResponse((message) => ({ ...message, content: message.content + event.delta }))
              setLiveMessage('Streaming the report…')
            }
            break
          case 'content_reset':
            updateResponse((message) => ({ ...message, content: '' }))
            setLiveMessage('Gathering analytics evidence…')
            break
          case 'tool_start':
            foldActivity()
            setLiveMessage(`Running ${humanizeAgentName(event.name)}…`)
            break
          case 'tool_finish':
            foldActivity()
            setLiveMessage(`${humanizeAgentName(event.name)} ${event.ok ? 'completed.' : 'failed safely.'}`)
            break
          case 'subagent_start':
            foldActivity()
            setLiveMessage(`${event.subagent.title || humanizeAgentName(event.subagent.agent)} is investigating…`)
            break
          case 'subagent_finish':
            foldActivity()
            setLiveMessage(
              `${event.subagent.title || humanizeAgentName(event.subagent.agent)} ${event.ok ? 'reported a finding.' : 'could not finish.'}`,
            )
            break
          case 'complete':
            break
          case 'error':
            setLiveMessage('')
            break
        }
      }

      const response = await streamAssistantChat({
        messages: requestMessages,
        context,
        consent_version: status.consent_version,
        session_id: requestSessionID,
      }, onStreamEvent, controller.signal)
      if (controller.signal.aborted || requestID !== requestIDRef.current) return
      const content = response.message.content
      if (!content.trim()) throw new Error('The analytics assistant returned an empty response.')
      pendingPromptRef.current = null
      setMessages((current) => current.map((message) => {
        if (message.id !== assistantMessage.id) return message
        // Canonical records replace live activities so restored and live
        // conversations render identically; fall back to the streamed ones.
        const activities = response.tool_calls.length > 0 || response.subagents.length > 0
          ? activitiesFromRecords(response.tool_calls, response.subagents)
          : settleActivities(message.activities ?? [])
        return {
          id: assistantMessage.id,
          role: 'assistant',
          content,
          signature: response.message.signature,
          activities,
          usage: response.usage,
          rounds: response.rounds,
          durationMs: response.duration_ms,
        }
      }))
      if (response.session_id) setSessionId(response.session_id)
      if (response.session_title) setSessionTitle(response.session_title)
      if (response.session_usage) setSessionUsage(response.session_usage)
      setCompletedAnnouncement(`Analytics assistant: ${content}`)
      setLiveMessage(`Response complete with ${response.model || status?.model || 'the configured model'}.`)
    } catch (caught) {
      if (controller.signal.aborted || requestID !== requestIDRef.current) return
      pendingPromptRef.current = null
      const partial = messagesRef.current.find((item) => item.id === assistantMessage.id)
      if (partial && partial.content.trim() !== '') {
        // A stream that died mid-answer still delivered analysis; keep it
        // readable alongside the error rather than erasing the turn.
        setMessages((current) => current.map((item) => (item.id === assistantMessage.id
          ? {
              ...item,
              streaming: false,
              stopped: 'failed' as const,
              activities: settleActivities(item.activities ?? []),
            }
          : item)))
      } else {
        setMessages((current) => current.filter((item) => item.id !== userMessage.id && item.id !== assistantMessage.id))
        if (!draftRef.current.trim()) setDraft(prompt)
      }
      // The prompt is kept verbatim so a transient failure costs one click,
      // not a retyped question.
      setFailedPrompt(prompt)
      setError(caught instanceof Error ? caught.message : 'The analytics request failed.')
      setLiveMessage('')
    } finally {
      if (requestID === requestIDRef.current) {
        abortRef.current = null
        setSending(false)
      }
    }
  }, [consentAccepted, context, messages, sending, sessionId, status])

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    void submitPrompt(draft)
  }

  const openAssistant = () => {
    setPreferences((current) => ({ ...current, open: true, minimized: false }))
  }

  if (!status?.available || typeof document === 'undefined') return null

  const positionedStyle = preferences.position
    ? { left: preferences.position.x, top: preferences.position.y }
    : { right: 12, bottom: 12 }

  if (!preferences.open) {
    return createPortal(
      <button
        ref={launcherRef}
        type="button"
        className="analytics-assistant-launcher"
        aria-label="Open analytics assistant"
        aria-controls={PANEL_ID}
        aria-expanded="false"
        onClick={openAssistant}
      >
        <AssistantGlyph size={21} />
        <span>Ask analytics</span>
      </button>,
      document.body,
    )
  }

  if (preferences.minimized) {
    return createPortal(
      <div
        ref={panelRef}
        id={PANEL_ID}
        className={`analytics-assistant-minimized${dragging ? ' dragging' : ''}`}
        style={positionedStyle}
        role="region"
        aria-label="Minimized analytics assistant"
      >
        <div
          className="analytics-assistant-drag-handle minimized"
          tabIndex={0}
          aria-label="Move minimized analytics assistant with the arrow keys"
          onPointerDown={beginDrag}
          onPointerMove={moveDrag}
          onPointerUp={endDrag}
          onPointerCancel={endDrag}
          onKeyDown={moveWithKeyboard}
        >
          <AssistantGlyph size={18} />
          <span>{sending ? 'Analyzing…' : 'Analytics assistant'}</span>
          {sending && <span className="analytics-assistant-running-dot" aria-hidden="true" />}
          <button
            ref={restoreButtonRef}
            type="button"
            className="analytics-assistant-restore"
            onClick={() => setPreferences((current) => ({ ...current, minimized: false }))}
          >
            Restore
          </button>
          <HeaderButton label="Close analytics assistant" icon="x" onClick={closeAssistant} />
        </div>
      </div>,
      document.body,
    )
  }

  return createPortal(
    <section
      ref={panelRef}
      id={PANEL_ID}
      className={`analytics-assistant-panel${dragging ? ' dragging' : ''}${resizing ? ' resizing' : ''}`}
      style={{
        ...positionedStyle,
        // Undefined falls through to the stylesheet default until the panel has
        // actually been resized.
        width: preferences.size?.width,
        height: preferences.size?.height,
      }}
      role="dialog"
      aria-modal="false"
      aria-labelledby={`${PANEL_ID}-title`}
    >
      {RESIZE_EDGES.map((edge) => (
        <div
          key={edge}
          className={`analytics-assistant-resize ${edge}`}
          aria-hidden="true"
          onPointerDown={beginResize(edge)}
          onPointerMove={moveResize}
          onPointerUp={endResize}
          onPointerCancel={endResize}
        />
      ))}
      <div
        className="analytics-assistant-drag-handle"
        tabIndex={0}
        aria-label="Move analytics assistant with the arrow keys"
        onPointerDown={beginDrag}
        onPointerMove={moveDrag}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onKeyDown={moveWithKeyboard}
      >
        <AssistantGlyph size={19} />
        <div className="analytics-assistant-title-block">
          <strong id={`${PANEL_ID}-title`} title={sessionTitle ?? undefined}>
            {sessionTitle ?? 'Analytics assistant'}
          </strong>
          <span>
            {providerLabel(status.provider)} · {status.model}
            {sessionUsage && usageSummary(sessionUsage) ? ` · ${usageSummary(sessionUsage)}` : ''}
          </span>
        </div>
        <div className="analytics-assistant-header-actions">
          {messages.length > 0 && (
            <button type="button" className="analytics-assistant-new-chat" onClick={resetConversation}>New chat</button>
          )}
          {messages.length > 0 && (
            <CopyButton
              value={() => conversationToMarkdown(messages)}
              label="Copy conversation as Markdown"
              copiedLabel="Conversation copied"
              iconSize={15}
              showLabel={false}
              className="analytics-assistant-header-button"
            />
          )}
          {consentAccepted && sessionsPersisted && (
            <HeaderButton
              label={historyOpen ? 'Hide saved conversations' : 'Show saved conversations'}
              icon="clock"
              active={historyOpen}
              onClick={toggleHistory}
            />
          )}
          <HeaderButton label="Reset assistant position" icon="refresh" onClick={resetPosition} />
          <HeaderButton
            label="Minimize analytics assistant"
            icon="chevron-down"
            onClick={() => setPreferences((current) => ({ ...current, minimized: true }))}
          />
          <HeaderButton label="Close analytics assistant" icon="x" onClick={closeAssistant} />
        </div>
      </div>

      {!consentAccepted ? (
        <PrivacyDisclosure
          status={status}
          onAccept={() => setPreferences((current) => ({ ...current, privacyAcceptedVersion: status.consent_version }))}
          onCancel={() => setPreferences((current) => ({ ...current, minimized: true }))}
          continueRef={privacyContinueRef}
        />
      ) : historyOpen ? (
        <SessionHistory
          sessions={historySessions}
          loading={historyLoading}
          error={historyError}
          activeSessionId={sessionId}
          onSelect={loadSession}
          onDelete={removeSession}
          onClose={() => setHistoryOpen(false)}
        />
      ) : (
        <>
          <div
            ref={messageListRef}
            className="analytics-assistant-messages"
            role="log"
            aria-label="Conversation"
            aria-live="off"
            aria-busy={sending}
            tabIndex={0}
            onScroll={updateMessageScrollFollow}
          >
            {messages.length === 0 && <Welcome status={status} onPrompt={(prompt) => void submitPrompt(prompt)} />}
            {messages.map((message) => <MessageRow key={message.id} message={message} />)}
            {!atBottom && messages.length > 0 && (
              <button
                type="button"
                className="analytics-assistant-jump-latest"
                onClick={jumpToLatest}
              >
                <Icon name="arrow-down" size={13} />
                Jump to latest
              </button>
            )}
          </div>

          <div className="analytics-assistant-sr-only" role="status" aria-live="polite" aria-atomic="true">
            {completedAnnouncement}
          </div>

          <div
            className="analytics-assistant-status"
            aria-live={completedAnnouncement ? 'off' : 'polite'}
            aria-atomic="true"
          >
            {error ? (
              <>
                <span className="error" role="alert">{error}</span>
                {failedPrompt && (
                  <button
                    type="button"
                    className="analytics-assistant-retry"
                    onClick={() => void submitPrompt(failedPrompt)}
                  >
                    Retry
                  </button>
                )}
              </>
            ) : (
              <span>{liveMessage}</span>
            )}
          </div>

          <form className="analytics-assistant-composer" onSubmit={handleSubmit}>
            <label htmlFor="analytics-assistant-input" className="analytics-assistant-sr-only">Ask about usage analytics</label>
            <textarea
              ref={inputRef}
              id="analytics-assistant-input"
              value={draft}
              maxLength={MAX_PROMPT_LENGTH}
              rows={1}
              placeholder="Ask for a report or usage insight…"
              onChange={(event) => setDraft(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && !event.shiftKey) {
                  event.preventDefault()
                  if (!sending && draft.trim()) void submitPrompt(draft)
                }
              }}
            />
            <div className="analytics-assistant-composer-actions">
              <span>{draft.length.toLocaleString()} / {MAX_PROMPT_LENGTH.toLocaleString()}</span>
              {/* Only sending is gated while a response streams — the composer
                  itself stays live so a follow-up can be drafted meanwhile. Both
                  buttons are shown so the disabled Ask explains the gate. */}
              {sending && (
                <button type="button" className="analytics-assistant-button secondary" onClick={() => stopRequest()}>Stop</button>
              )}
              <button
                type="submit"
                className="analytics-assistant-button primary"
                disabled={sending || !draft.trim()}
                title={sending ? 'Waiting for the current answer' : undefined}
              >
                Ask
              </button>
            </div>
          </form>
        </>
      )}
    </section>,
    document.body,
  )
}

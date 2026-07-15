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
import { useDashboardContext } from '../layout/dashboard-context'
import { getAssistantStatus, streamAssistantChat } from '../../lib/api'
import {
  clampAssistantPosition,
  defaultAssistantPosition,
  readAssistantPreferences,
  writeAssistantPreferences,
  type AssistantPosition,
  type AssistantPreferences,
  type AssistantViewport,
} from '../../lib/assistant-position'
import { usePeriodState } from '../../lib/use-period-state'
import { boundAssistantHistory } from '../../lib/assistant-history'
import type {
  AssistantMessage,
  AssistantRequestContext,
  AssistantStatusResponse,
  AssistantStreamEvent,
} from '../../types/assistant'

const PANEL_ID = 'analytics-assistant-panel'
const MAX_PROMPT_LENGTH = 4_000
const STATUS_RETRY_DELAYS_MS = [0, 10_000, 30_000] as const
const ASSISTANT_ROUTES = new Set([
  '/overview', '/daily', '/models', '/tools', '/projects', '/sessions', '/config',
])

const QUICK_PROMPTS = [
  'Summarize my usage for this period.',
  'What changed most recently?',
  'Which models and projects use the most tokens?',
  'Which tools fail most often?',
] as const

interface DisplayMessage extends AssistantMessage {
  id: number
  toolsUsed?: string[]
  streaming?: boolean
  toolActivities?: StreamToolActivity[]
}

interface StreamToolActivity {
  id: string
  name: string
  status: 'running' | 'complete' | 'failed'
}

interface DragState {
  pointerId: number
  startX: number
  startY: number
  origin: AssistantPosition
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

function humanizeToolName(value: string): string {
  return value.replace(/[_-]+/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
}

function providerLabel(value: string): string {
  return value.trim().toLowerCase() === 'minimax' ? 'MiniMax' : value
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
  onClick,
}: {
  label: string
  icon: 'refresh' | 'chevron-down' | 'x'
  onClick: () => void
}) {
  return (
    <button
      type="button"
      className="analytics-assistant-header-button"
      aria-label={label}
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
          Your prompts and the aggregate usage metrics requested to answer them are sent to MiniMax.
          Transcripts, source files, raw configuration, and secrets are not included.
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

function MessageRow({ message }: { message: DisplayMessage }) {
  const assistant = message.role === 'assistant'
  const hasContent = message.content.length > 0
  return (
    <article
      className={`analytics-assistant-message ${assistant ? 'assistant' : 'user'}${message.streaming ? ' streaming' : ''}`}
      aria-busy={message.streaming || undefined}
    >
      <div className="analytics-assistant-message-meta">
        <span>{assistant ? 'Analytics assistant' : 'You'}</span>
        {message.streaming && <span className="analytics-assistant-stream-label">Live</span>}
      </div>
      {hasContent && (
        <div className="analytics-assistant-message-content">
          {message.content}
          {message.streaming && <span className="analytics-assistant-stream-cursor" aria-hidden="true" />}
        </div>
      )}
      {assistant && message.toolActivities && message.toolActivities.length > 0 && (
        <div className="analytics-assistant-tool-activity" aria-label="Live analytics tool activity">
          {message.toolActivities.map((tool) => (
            <div key={tool.id} className={`analytics-assistant-tool-call ${tool.status}`}>
              <span className="analytics-assistant-tool-state" aria-hidden="true">
                {tool.status === 'running'
                  ? <i />
                  : <Icon name={tool.status === 'complete' ? 'check' : 'x'} size={11} />}
              </span>
              <Icon name="wrench" size={12} />
              <span>{humanizeToolName(tool.name)}</span>
              <small>{tool.status === 'running' ? 'Running' : tool.status === 'complete' ? 'Complete' : 'Failed'}</small>
            </div>
          ))}
        </div>
      )}
      {message.streaming && !hasContent && (!message.toolActivities || message.toolActivities.length === 0) && (
        <div className="analytics-assistant-thinking compact" role="status">
          <AssistantGlyph size={15} />
          <span>Starting the report</span>
          <i /><i /><i />
        </div>
      )}
      {assistant && message.toolsUsed && message.toolsUsed.length > 0 && (
        <div className="analytics-assistant-tools" aria-label="Analytics tools used">
          <Icon name="wrench" size={12} />
          {message.toolsUsed.map((tool) => <span key={tool}>{humanizeToolName(tool)}</span>)}
        </div>
      )}
    </article>
  )
}

function Welcome({ onPrompt }: { onPrompt: (prompt: string) => void }) {
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
  const [liveMessage, setLiveMessage] = useState('')
  const [completedAnnouncement, setCompletedAnnouncement] = useState('')
  const [dragging, setDragging] = useState(false)
  const panelRef = useRef<HTMLDivElement>(null)
  const launcherRef = useRef<HTMLButtonElement>(null)
  const privacyContinueRef = useRef<HTMLButtonElement>(null)
  const restoreButtonRef = useRef<HTMLButtonElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const messageListRef = useRef<HTMLDivElement>(null)
  const followStreamRef = useRef(true)
  const dragRef = useRef<DragState | null>(null)
  const abortRef = useRef<AbortController | null>(null)
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

  const clampToPanel = useCallback((position: AssistantPosition): AssistantPosition => {
    const element = panelRef.current
    if (!element) return position
    const rect = element.getBoundingClientRect()
    return clampAssistantPosition(position, { width: rect.width, height: rect.height }, currentViewport())
  }, [])

  useLayoutEffect(() => {
    if (!preferences.open) return
    const element = panelRef.current
    if (!element) return

    const place = () => {
      const rect = element.getBoundingClientRect()
      const viewport = currentViewport()
      setPreferences((current) => {
        const desired = current.position ?? defaultAssistantPosition(
          { width: rect.width, height: rect.height },
          viewport,
        )
        const next = clampAssistantPosition(desired, { width: rect.width, height: rect.height }, viewport)
        return samePosition(current.position, next) ? current : { ...current, position: next }
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
  }, [preferences.open, preferences.minimized])

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
    const list = messageListRef.current
    if (list && followStreamRef.current) list.scrollTop = list.scrollHeight
  }, [messages, preferences.minimized, preferences.open, sending])

  const updateMessageScrollFollow = useCallback(() => {
    const list = messageListRef.current
    if (!list) return
    followStreamRef.current = list.scrollHeight - list.scrollTop - list.clientHeight < 40
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

  const stopRequest = useCallback((message = 'Request stopped.') => {
    requestIDRef.current += 1
    abortRef.current?.abort()
    abortRef.current = null
    const pending = pendingPromptRef.current
    pendingPromptRef.current = null
    if (pending) {
      setMessages((current) => current.filter((item) => item.id !== pending.id && item.id !== pending.responseID))
      setDraft(pending.content)
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
    setCompletedAnnouncement('')
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
      toolActivities: [],
    }
    const requestMessages = boundAssistantHistory(
      [...messages, userMessage].map(({ role, content, signature }) => ({ role, content, signature })),
    )
    const controller = new AbortController()
    const requestID = ++requestIDRef.current
    abortRef.current = controller
    pendingPromptRef.current = { id: userMessage.id, responseID: assistantMessage.id, content: prompt }
    followStreamRef.current = true
    setMessages((current) => [...current, userMessage, assistantMessage])
    setDraft('')
    setError(null)
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

        switch (event.type) {
          case 'start':
            setLiveMessage(`Connected to ${event.model}.`)
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
            updateResponse((message) => ({
              ...message,
              toolActivities: [
                ...(message.toolActivities ?? []).filter((tool) => tool.id !== event.call_id),
                { id: event.call_id, name: event.name, status: 'running' },
              ],
            }))
            setLiveMessage(`Running ${humanizeToolName(event.name)}…`)
            break
          case 'tool_finish':
            updateResponse((message) => ({
              ...message,
              toolActivities: (message.toolActivities ?? []).map((tool) => (
                tool.id === event.call_id
                  ? { ...tool, status: event.ok ? 'complete' : 'failed' }
                  : tool
              )),
            }))
            setLiveMessage(`${humanizeToolName(event.name)} ${event.ok ? 'completed.' : 'failed safely.'}`)
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
      }, onStreamEvent, controller.signal)
      if (controller.signal.aborted || requestID !== requestIDRef.current) return
      const content = response.message.content
      if (!content.trim()) throw new Error('The analytics assistant returned an empty response.')
      pendingPromptRef.current = null
      setMessages((current) => current.map((message) => message.id === assistantMessage.id ? {
        id: assistantMessage.id,
        role: 'assistant',
        content,
        signature: response.message.signature,
        toolsUsed: response.tools_used.filter((tool) => tool.trim() !== ''),
      } : message))
      setCompletedAnnouncement(`Analytics assistant: ${content}`)
      setLiveMessage(`Response complete with ${response.model || status?.model || 'the configured model'}.`)
    } catch (caught) {
      if (controller.signal.aborted || requestID !== requestIDRef.current) return
      pendingPromptRef.current = null
      setMessages((current) => current.filter((item) => item.id !== userMessage.id && item.id !== assistantMessage.id))
      setDraft(prompt)
      setError(caught instanceof Error ? caught.message : 'The analytics request failed.')
      setLiveMessage('')
    } finally {
      if (requestID === requestIDRef.current) {
        abortRef.current = null
        setSending(false)
      }
    }
  }, [consentAccepted, context, messages, sending, status])

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
      className={`analytics-assistant-panel${dragging ? ' dragging' : ''}`}
      style={positionedStyle}
      role="dialog"
      aria-modal="false"
      aria-labelledby={`${PANEL_ID}-title`}
    >
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
          <strong id={`${PANEL_ID}-title`}>Analytics assistant</strong>
          <span>{providerLabel(status.provider)} · {status.model}</span>
        </div>
        <div className="analytics-assistant-header-actions">
          {messages.length > 0 && (
            <button type="button" className="analytics-assistant-new-chat" onClick={resetConversation}>New chat</button>
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
            {messages.length === 0 && <Welcome onPrompt={(prompt) => void submitPrompt(prompt)} />}
            {messages.map((message) => <MessageRow key={message.id} message={message} />)}
          </div>

          <div className="analytics-assistant-sr-only" role="status" aria-live="polite" aria-atomic="true">
            {completedAnnouncement}
          </div>

          <div
            className="analytics-assistant-status"
            aria-live={completedAnnouncement ? 'off' : 'polite'}
            aria-atomic="true"
          >
            {error ? <span className="error" role="alert">{error}</span> : <span>{liveMessage}</span>}
          </div>

          <form className="analytics-assistant-composer" onSubmit={handleSubmit}>
            <label htmlFor="analytics-assistant-input" className="analytics-assistant-sr-only">Ask about usage analytics</label>
            <textarea
              ref={inputRef}
              id="analytics-assistant-input"
              value={draft}
              maxLength={MAX_PROMPT_LENGTH}
              rows={2}
              placeholder="Ask for a report or usage insight…"
              disabled={sending}
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
              {sending ? (
                <button type="button" className="analytics-assistant-button secondary" onClick={() => stopRequest()}>Stop</button>
              ) : (
                <button type="submit" className="analytics-assistant-button primary" disabled={!draft.trim()}>Ask</button>
              )}
            </div>
          </form>
        </>
      )}
    </section>,
    document.body,
  )
}

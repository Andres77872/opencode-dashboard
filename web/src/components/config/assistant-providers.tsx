/*
  Assistant providers — which model answers the analytics assistant, and the
  endpoints it can be served from.

  Three cards: the active model, the configured providers (the built-in ones
  and any OpenAI-compatible endpoint the user added), and the form to add one.
  The catalog is the shared store in lib/assistant-providers-store, so the
  docked assistant panel and this section never disagree about the selection.
*/
import { useCallback, useContext, useEffect, useId, useRef, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Badge, Button, Card, ConfirmDialog, EmptyState, Field, Notice, Skeleton, TextField } from '../vael'
import type { NoticeTone } from '../vael'
import { DashboardContext } from '../layout/dashboard-context'
import { AssistantModelPicker } from './assistant-model-picker'
import {
  MAX_CONTEXT_LIMIT,
  MIN_CONTEXT_LIMIT,
  catalogLabel,
  checkProviderBaseURL,
  checkProviderName,
  describeModel,
  formatContextLimit,
  parseContextLimit,
} from './assistant-provider-form'
import {
  createAssistantProvider,
  deleteAssistantProvider,
  putAssistantProviderModel,
  putAssistantSelection,
  refreshAssistantProviderModels,
  updateAssistantProvider,
} from '../../lib/api'
import type { AssistantProviderMutation } from '../../lib/api'
import {
  applyAssistantSelection,
  notifyAssistantSelectionChanged,
  reloadAssistantProviders,
  useAssistantProviders,
} from '../../lib/assistant-providers-store'
import { formatInteger } from '../../lib/format'
import type { AssistantProvider } from '../../types/assistant'

const INSECURE_ACK_LABEL = 'I understand this loopback/private-LAN connection sends prompts and credentials without TLS.'
const MODEL_PREVIEW_COUNT = 12
const SUCCESS_VISIBLE_MS = 6_000

interface Feedback {
  tone: NoticeTone
  title: string
  message?: string
}

type RunAction = (options: {
  /** Identifies the in-flight action, so only its own button shows progress. */
  key: string
  /** Where the outcome is reported: 'selection', 'create', or a provider id. */
  scope: string
  work: () => Promise<unknown>
  success: string
  failure: string
  /** False when `work` already updated the shared catalog itself. */
  reload?: boolean
}) => Promise<boolean>

/** Outcome messages keyed by where they belong; a success clears itself, an error stays. */
function useScopedFeedback() {
  const [items, setItems] = useState<Record<string, Feedback>>({})
  const timersRef = useRef(new Map<string, number>())

  useEffect(() => {
    const timers = timersRef.current
    return () => {
      for (const timer of timers.values()) window.clearTimeout(timer)
      timers.clear()
    }
  }, [])

  const clear = useCallback((scope: string) => {
    setItems((current) => {
      if (!(scope in current)) return current
      const next = { ...current }
      delete next[scope]
      return next
    })
  }, [])

  const setFeedback = useCallback((scope: string, feedback: Feedback | null) => {
    const timers = timersRef.current
    window.clearTimeout(timers.get(scope))
    timers.delete(scope)
    if (!feedback) {
      clear(scope)
      return
    }
    setItems((current) => ({ ...current, [scope]: feedback }))
    if (feedback.tone === 'success') {
      timers.set(scope, window.setTimeout(() => {
        timers.delete(scope)
        clear(scope)
      }, SUCCESS_VISIBLE_MS))
    }
  }, [clear])

  return [items, setFeedback] as const
}

function FeedbackNotice({ feedback }: { feedback: Feedback | undefined }) {
  if (!feedback) return null
  return <Notice tone={feedback.tone} title={feedback.title}>{feedback.message}</Notice>
}

export function AssistantProviders() {
  const { data, loading, error } = useAssistantProviders()
  const [busy, setBusy] = useState<ReadonlySet<string>>(() => new Set())
  const [feedback, setFeedback] = useScopedFeedback()

  // The dashboard's Refresh re-reads this section too. Read without the
  // throwing hook so the section also renders outside the dashboard shell.
  const refreshNonce = useContext(DashboardContext)?.refreshNonce ?? 0
  const handledNonceRef = useRef(refreshNonce)
  useEffect(() => {
    if (handledNonceRef.current === refreshNonce) return
    handledNonceRef.current = refreshNonce
    void reloadAssistantProviders().catch(() => undefined)
  }, [refreshNonce])

  const run = useCallback<RunAction>(async ({ key, scope, work, success, failure, reload = true }) => {
    setBusy((current) => new Set([...current, key]))
    setFeedback(scope, null)
    try {
      await work()
      // The mutation is done; a failed re-read is reported by the store, not
      // as a failure of the action that just succeeded.
      if (reload) await reloadAssistantProviders().catch(() => undefined)
      // The assistant's own status (availability, consent text) follows the
      // provider setup, so tell it to look again.
      notifyAssistantSelectionChanged()
      setFeedback(scope, { tone: 'success', title: success })
      return true
    } catch (caught) {
      setFeedback(scope, {
        tone: 'danger',
        title: failure,
        message: caught instanceof Error ? caught.message : 'The request failed.',
      })
      return false
    } finally {
      setBusy((current) => {
        const next = new Set(current)
        next.delete(key)
        return next
      })
    }
  }, [setFeedback])

  const providers = data?.providers ?? []
  const builtIn = providers.filter((provider) => provider.built_in)
  const custom = providers.filter((provider) => !provider.built_in)
  const selection = data?.selection ?? null
  const selectedProvider = providers.find((provider) => provider.id === selection?.provider_id) ?? null
  const selectedModel = selectedProvider?.models.find((model) => model.id === selection?.model_id) ?? null
  const anySelectable = providers.some((provider) => provider.models.some((model) => model.selectable))

  // An empty pair is the server's "no selection": the assistant is turned off.
  const select = (providerId: string, modelId: string) => {
    void run({
      key: 'selection',
      scope: 'selection',
      reload: false,
      work: async () => applyAssistantSelection(await putAssistantSelection(providerId, modelId)),
      success: modelId ? `The assistant now uses ${modelId}.` : 'The assistant is off until you choose a model.',
      failure: 'The model could not be changed',
    })
  }

  if (!data) {
    return (
      <Card title="Assistant providers" subtitle="The model that answers the analytics assistant, and where it is served from.">
        {loading ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }} aria-busy="true" aria-label="Loading assistant providers">
            <Skeleton width="min(420px, 100%)" height={36} radius="var(--radius-md)" />
            <Skeleton width="100%" height={120} radius="var(--radius-lg)" />
          </div>
        ) : (
          <Notice
            tone="danger"
            title="Assistant providers could not be loaded"
            action={<Button size="sm" variant="secondary" onClick={() => void reloadAssistantProviders().catch(() => undefined)}>Retry</Button>}
          >
            {error}
          </Notice>
        )}
      </Card>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {error && (
        <Notice
          tone="warning"
          title="Refresh failed — showing the last loaded providers"
          action={<Button size="sm" variant="secondary" onClick={() => void reloadAssistantProviders().catch(() => undefined)}>Retry</Button>}
        >
          {error}
        </Notice>
      )}

      <Card
        title="Assistant model"
        subtitle="One provider and model answers every assistant question. A change applies to the next question in every tab and conversation; an answer already in progress keeps the model it started with."
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Field label="Active provider and model" hint={anySelectable ? undefined : 'No provider has a usable model yet. Set one up below.'}>
            <div style={{ maxWidth: 480 }}>
              <AssistantModelPicker
                providers={providers}
                selection={selection?.provider_id ? selection : null}
                onSelect={select}
                onClear={() => select('', '')}
                busy={busy.has('selection')}
                label="Active assistant provider and model"
              />
            </div>
          </Field>
          {selectedProvider && selectedModel && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>
              <span>Questions are sent to</span>
              <span style={{ fontFamily: 'var(--font-mono)', color: 'var(--fg-secondary)', overflowWrap: 'anywhere' }}>{selectedProvider.destination_label}</span>
              {selectedModel.context_limit > 0 && <Badge>{formatContextLimit(selectedModel.context_limit)} context</Badge>}
              {!selectedModel.verified && (
                <span title="The context window was entered by hand, not reported by the provider.">
                  <Badge tone="warning">Unverified context</Badge>
                </span>
              )}
              {!selectedProvider.available && <Badge tone="warning" dot>Provider unavailable</Badge>}
            </div>
          )}
          {selection?.provider_id && !selectedModel && (
            <Notice tone="warning" title="The selected model is no longer available">
              {selection.model_id} is not in any provider’s catalog. Choose another model to use the assistant.
            </Notice>
          )}
          <FeedbackNotice feedback={feedback.selection} />
        </div>
      </Card>

      <Card
        title="Providers"
        subtitle="Built-in providers use credentials already on this machine. Custom providers are OpenAI-compatible endpoints you add."
        action={(
          <Button
            size="sm"
            variant="ghost"
            iconLeft="refresh"
            busy={loading}
            onClick={() => void reloadAssistantProviders().catch(() => undefined)}
            title="Re-read the provider list. This does not contact the providers."
          >
            Reload
          </Button>
        )}
      >
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <FeedbackNotice feedback={feedback.providers} />
          {providers.length === 0 && (
            <EmptyState icon="cpu" title="No providers yet" description="Add an OpenAI-compatible endpoint below to use the assistant." />
          )}
          {builtIn.length > 0 && (
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(260px, 100%), 1fr))', gap: 12 }}>
              {builtIn.map((provider) => (
                <BuiltInProviderCard key={provider.id} provider={provider} busy={busy} run={run} feedback={feedback[provider.id]} />
              ))}
            </div>
          )}
          {custom.map((provider) => (
            <CustomProviderCard key={provider.id} provider={provider} busy={busy} run={run} feedback={feedback[provider.id]} />
          ))}
        </div>
      </Card>

      <AddProviderCard busy={busy.has('create')} run={run} feedback={feedback.create} />
    </div>
  )
}

/* ---------- shared pieces ---------- */

function ProviderPanel({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section
      aria-label={label}
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 12,
        minWidth: 0,
        padding: 14,
        background: 'var(--ink-750)',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-lg)',
      }}
    >
      {children}
    </section>
  )
}

function ProviderHeading({ provider, action }: { provider: AssistantProvider; action?: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 10, rowGap: 8, flexWrap: 'wrap' }}>
      <div style={{ minWidth: 0, flex: '1 1 200px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, rowGap: 4, flexWrap: 'wrap' }}>
          <span style={{ font: '600 14px/1.3 var(--font-ui)', color: 'var(--fg-primary)', overflowWrap: 'anywhere' }}>{provider.name}</span>
          <Badge tone={provider.available ? 'success' : 'warning'} dot>{provider.available ? 'Available' : 'Needs setup'}</Badge>
          {!provider.built_in && <Badge>{provider.api_key_configured ? 'Key saved' : 'No key'}</Badge>}
          {provider.insecure_transport_ack && provider.base_url?.toLowerCase().startsWith('http://') && (
            <span title="This endpoint is reached over plain HTTP, without TLS.">
              <Badge tone="warning">HTTP</Badge>
            </span>
          )}
        </div>
        <div style={{ font: '400 11.5px/1.45 var(--font-mono)', color: 'var(--fg-muted)', marginTop: 4, overflowWrap: 'anywhere' }}>
          {provider.destination_label}
        </div>
        <div style={{ font: '400 12px/1.45 var(--font-ui)', color: provider.catalog.status === 'error' ? 'var(--warning)' : 'var(--fg-muted)', marginTop: 3, overflowWrap: 'anywhere' }}>
          {catalogLabel(provider)}
        </div>
      </div>
      {action && <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>{action}</div>}
    </div>
  )
}

function ModelList({ provider, onPick }: { provider: AssistantProvider; onPick?: (modelId: string) => void }) {
  const [showAll, setShowAll] = useState(false)
  if (provider.models.length === 0) return null
  const visible = showAll ? provider.models : provider.models.slice(0, MODEL_PREVIEW_COUNT)
  const hidden = provider.models.length - visible.length
  const chipStyle = (selectable: boolean) => ({
    display: 'inline-flex',
    alignItems: 'center',
    maxWidth: '100%',
    padding: '3px 8px',
    border: '1px solid var(--border-default)',
    borderRadius: 'var(--radius-pill)',
    background: 'var(--ink-800)',
    color: selectable ? 'var(--fg-secondary)' : 'var(--warning)',
    font: '500 11.5px/1.4 var(--font-mono)',
    overflowWrap: 'anywhere' as const,
  })
  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, alignItems: 'center' }}>
      {visible.map((model) =>
        !model.selectable && onPick ? (
          <button
            key={model.id}
            type="button"
            onClick={() => onPick(model.id)}
            title="This model reports no context window. Select it to enter one."
            style={{ ...chipStyle(false), cursor: 'pointer' }}
          >
            {describeModel(model)}
          </button>
        ) : (
          <span key={model.id} style={chipStyle(model.selectable)}>{describeModel(model)}</span>
        ),
      )}
      {(hidden > 0 || showAll) && provider.models.length > MODEL_PREVIEW_COUNT && (
        <Button size="sm" variant="ghost" onClick={() => setShowAll((value) => !value)}>
          {showAll ? 'Show fewer' : `Show ${formatInteger(hidden)} more`}
        </Button>
      )}
    </div>
  )
}

/* ---------- built-in providers ---------- */

function BuiltInProviderCard({ provider, busy, run, feedback }: { provider: AssistantProvider; busy: ReadonlySet<string>; run: RunAction; feedback: Feedback | undefined }) {
  const refreshKey = `${provider.id}:refresh`
  return (
    <ProviderPanel label={provider.name}>
      <ProviderHeading provider={provider} />
      {!provider.api_key_configured && (
        <div style={{ font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)' }}>
          {provider.kind === 'kimi' ? (
            <>Set <code style={{ font: '500 11.5px/1.4 var(--font-mono)', color: 'var(--fg-secondary)' }}>OPENCODE_DASHBOARD_KIMI_API_KEY</code>, or sign in with Kimi Code.</>
          ) : (
            <>Set <code style={{ font: '500 11.5px/1.4 var(--font-mono)', color: 'var(--fg-secondary)' }}>OPENCODE_DASHBOARD_MINIMAX_API_KEY</code>, or configure the OpenCode MiniMax login.</>
          )}
        </div>
      )}
      <ModelList provider={provider} />
      <FeedbackNotice feedback={feedback} />
      <div>
        <Button
          size="sm"
          iconLeft="refresh"
          busy={busy.has(refreshKey)}
          onClick={() => void run({
            key: refreshKey,
            scope: provider.id,
            work: () => refreshAssistantProviderModels(provider.id),
            success: `${provider.name} models refreshed.`,
            failure: `${provider.name} models could not be refreshed`,
          })}
        >
          Refresh models
        </Button>
      </div>
    </ProviderPanel>
  )
}

/* ---------- custom providers ---------- */

function SecretField({ label, value, onChange, placeholder, hint, error, disabled }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string; hint?: ReactNode; error?: ReactNode; disabled?: boolean }) {
  const id = useId()
  const [revealed, setRevealed] = useState(false)
  return (
    <Field label={label} htmlFor={id} hint={hint} error={error}>
      <div style={{ position: 'relative' }}>
        <input
          id={id}
          className="vael-input"
          type={revealed ? 'text' : 'password'}
          value={value}
          placeholder={placeholder}
          disabled={disabled}
          // Not a login: keep password managers from offering to save or fill it.
          autoComplete="off"
          data-1p-ignore
          data-lpignore="true"
          spellCheck={false}
          aria-invalid={error ? true : undefined}
          onChange={(event) => onChange(event.target.value)}
          style={{ fontFamily: 'var(--font-mono)', fontSize: 12, paddingRight: 64 }}
        />
        <button
          type="button"
          onClick={() => setRevealed((current) => !current)}
          aria-pressed={revealed}
          aria-label={revealed ? `Hide ${label}` : `Show ${label}`}
          disabled={disabled || value === ''}
          style={{
            position: 'absolute',
            right: 4,
            top: 4,
            height: 26,
            padding: '0 9px',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            background: 'transparent',
            color: value === '' ? 'var(--fg-faint)' : 'var(--fg-secondary)',
            font: '600 11.5px/1 var(--font-ui)',
            cursor: value === '' ? 'default' : 'pointer',
          }}
        >
          {revealed ? 'Hide' : 'Show'}
        </button>
      </div>
    </Field>
  )
}

function InsecureAcknowledgement({ checked, onChange, error }: { checked: boolean; onChange: (checked: boolean) => void; error?: boolean }) {
  return (
    <label
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 9,
        padding: '9px 11px',
        borderRadius: 'var(--radius-md)',
        background: 'var(--warning-soft)',
        border: `1px solid ${error ? 'var(--warning)' : 'var(--border-subtle)'}`,
        font: '400 12px/1.45 var(--font-ui)',
        color: 'var(--fg-secondary)',
        cursor: 'pointer',
      }}
    >
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        style={{ marginTop: 2, accentColor: 'var(--warning)', flexShrink: 0 }}
      />
      {INSECURE_ACK_LABEL}
    </label>
  )
}

function CustomProviderCard({ provider, busy, run, feedback }: { provider: AssistantProvider; busy: ReadonlySet<string>; run: RunAction; feedback: Feedback | undefined }) {
  const savedURL = provider.base_url ?? ''
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(provider.name)
  const [baseURL, setBaseURL] = useState(savedURL)
  const [apiKey, setAPIKey] = useState('')
  const [insecureAck, setInsecureAck] = useState(provider.insecure_transport_ack === true)
  const [manualModel, setManualModel] = useState('')
  const [contextLimit, setContextLimit] = useState('')
  const [confirm, setConfirm] = useState<'delete' | 'clear-key' | null>(null)
  const contextRef = useRef<HTMLDivElement>(null)

  const keys = {
    save: `${provider.id}:save`,
    refresh: `${provider.id}:refresh`,
    remove: `${provider.id}:delete`,
    clearKey: `${provider.id}:clear-key`,
    model: `${provider.id}:model`,
  }
  const anyBusy = Object.values(keys).some((key) => busy.has(key))

  const urlCheck = checkProviderBaseURL(baseURL)
  const savedOrigin = checkProviderBaseURL(savedURL).origin
  const nameError = checkProviderName(name)
  const key = apiKey.trim()
  const hostChanged = urlCheck.origin !== null && savedOrigin !== null && urlCheck.origin !== savedOrigin
  // The saved key is never sent to a different host: moving the endpoint
  // means entering the key for the new one.
  const keyError = hostChanged && provider.api_key_configured && !key
    ? 'Enter the API key again. A saved key is never sent to a different host.'
    : null
  const ackMissing = urlCheck.insecure && !insecureAck

  // Only what actually changed is sent; an untouched field is left alone.
  const changes: AssistantProviderMutation = {}
  if (name.trim() !== provider.name) changes.name = name.trim()
  if (baseURL.trim() !== savedURL) changes.base_url = baseURL.trim()
  if (key) changes.api_key = key
  const desiredAck = urlCheck.insecure && insecureAck
  if (desiredAck !== (provider.insecure_transport_ack === true)) changes.insecure_transport_ack = desiredAck
  const dirty = Object.keys(changes).length > 0
  const valid = !nameError && !urlCheck.error && !keyError && !ackMissing
  const saveBlocked = !dirty
    ? 'Nothing has changed.'
    : !valid
      ? 'Fix the highlighted fields first.'
      : undefined

  const resetForm = () => {
    setName(provider.name)
    setBaseURL(savedURL)
    setAPIKey('')
    setInsecureAck(provider.insecure_transport_ack === true)
  }

  const save = (event: FormEvent) => {
    event.preventDefault()
    if (!dirty || !valid) return
    void run({
      key: keys.save,
      scope: provider.id,
      work: async () => {
        const saved = await updateAssistantProvider(provider.id, changes)
        // The server normalizes what it stores (a trailing slash, a default
        // /v1 path); adopt that so the form is not left looking unsaved.
        setName(saved.name)
        setBaseURL(saved.base_url ?? '')
        setInsecureAck(saved.insecure_transport_ack === true)
        setAPIKey('')
      },
      success: `${name.trim()} saved.`,
      failure: `${provider.name} could not be saved`,
    }).then((ok) => {
      if (ok) setEditing(false)
    })
  }

  const limit = parseContextLimit(contextLimit)
  const modelId = manualModel.trim()
  const existingModel = provider.models.find((model) => model.id === modelId)
  const contextError = contextLimit.trim() !== '' && limit === null
    ? `Enter a whole number of tokens between ${formatInteger(MIN_CONTEXT_LIMIT)} and ${formatInteger(MAX_CONTEXT_LIMIT)}.`
    : undefined

  const addModel = (event: FormEvent) => {
    event.preventDefault()
    if (!modelId || limit === null) return
    void run({
      key: keys.model,
      scope: provider.id,
      work: () => putAssistantProviderModel(provider.id, modelId, limit),
      success: `${modelId} ${existingModel ? 'updated' : 'added'} with a ${formatContextLimit(limit)} context window.`,
      failure: `${modelId} could not be saved`,
    }).then((ok) => {
      if (!ok) return
      setManualModel('')
      setContextLimit('')
    })
  }

  return (
    <ProviderPanel label={provider.name}>
      <ProviderHeading
        provider={provider}
        action={(
          <>
            <Button
              size="sm"
              iconLeft="refresh"
              busy={busy.has(keys.refresh)}
              disabled={anyBusy && !busy.has(keys.refresh)}
              title={`Ask ${provider.destination_label} for its model list.`}
              onClick={() => void run({
                key: keys.refresh,
                scope: provider.id,
                work: () => refreshAssistantProviderModels(provider.id),
                success: `${provider.name} models refreshed.`,
                failure: `${provider.name} models could not be refreshed`,
              })}
            >
              Refresh models
            </Button>
            <Button
              size="sm"
              variant={editing ? 'secondary' : 'ghost'}
              iconLeft="pencil"
              onClick={() => {
                // Opening starts from what is saved now; closing discards the draft.
                resetForm()
                setEditing((current) => !current)
              }}
            >
              {editing ? 'Cancel edit' : 'Edit'}
            </Button>
            <Button size="sm" variant="danger" disabled={anyBusy} onClick={() => setConfirm('delete')}>Delete</Button>
          </>
        )}
      />

      <FeedbackNotice feedback={feedback} />

      {editing && (
        <form onSubmit={save} aria-label={`Edit ${provider.name}`} noValidate style={{ display: 'flex', flexDirection: 'column', gap: 12, paddingTop: 12, borderTop: '1px solid var(--border-subtle)' }}>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(220px, 100%), 1fr))', gap: 12 }}>
            <TextField label="Name" value={name} onChange={setName} error={nameError ?? undefined} required />
            <TextField label="Base URL" value={baseURL} onChange={setBaseURL} error={urlCheck.error ?? undefined} mono inputMode="url" required />
          </div>
          <SecretField
            label={provider.api_key_configured ? 'Replace API key' : 'API key'}
            value={apiKey}
            onChange={setAPIKey}
            placeholder={provider.api_key_configured ? 'Leave blank to keep the saved key' : 'Optional for local servers'}
            error={keyError ?? undefined}
            hint={provider.api_key_configured ? 'A key is saved. It is never shown again; enter a new one to replace it.' : 'No key is saved for this provider.'}
          />
          {urlCheck.insecure && <InsecureAcknowledgement checked={insecureAck} onChange={setInsecureAck} error={ackMissing} />}
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
            <Button type="submit" variant="primary" size="sm" busy={busy.has(keys.save)} disabled={!dirty || !valid} title={saveBlocked}>Save changes</Button>
            {provider.api_key_configured && (
              <Button size="sm" variant="danger" disabled={anyBusy} onClick={() => setConfirm('clear-key')}>Clear saved key</Button>
            )}
            {ackMissing && <span style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--warning)' }}>Acknowledge the plain-HTTP warning to save.</span>}
          </div>
        </form>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 10, paddingTop: 12, borderTop: '1px solid var(--border-subtle)' }}>
        <div style={{ font: '600 12px/1.2 var(--font-ui)', color: 'var(--fg-secondary)' }}>Models</div>
        {provider.models.length === 0 ? (
          <div style={{ font: '400 12px/1.5 var(--font-ui)', color: 'var(--fg-muted)' }}>
            No models yet. Refresh the list, or add one by hand below.
          </div>
        ) : (
          <ModelList
            provider={provider}
            onPick={(id) => {
              setManualModel(id)
              contextRef.current?.querySelector('input')?.focus()
            }}
          />
        )}
        <form
          onSubmit={addModel}
          aria-label={`Add a model to ${provider.name}`}
          noValidate
          style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(190px, 100%), 1fr))', gap: 12, alignItems: 'start' }}
        >
          <TextField
            label="Model ID"
            value={manualModel}
            onChange={setManualModel}
            placeholder="Exact ID the endpoint expects"
            hint="For a model the endpoint does not list, or one without a context window."
            mono
          />
          <div ref={contextRef}>
            <TextField
              label="Context window (tokens)"
              value={contextLimit}
              onChange={setContextLimit}
              placeholder="For example 131072"
              inputMode="numeric"
              error={contextError}
              mono
            />
          </div>
          <div style={{ paddingTop: 20 }}>
            <Button
              type="submit"
              size="sm"
              iconLeft="plus"
              busy={busy.has(keys.model)}
              disabled={!modelId || limit === null}
              title={!modelId ? 'Enter a model ID.' : limit === null ? 'Enter the model’s context window.' : undefined}
            >
              {existingModel ? 'Update model' : 'Add model'}
            </Button>
          </div>
        </form>
      </div>

      <ConfirmDialog
        open={confirm === 'delete'}
        title={`Delete ${provider.name}?`}
        confirmLabel="Delete provider"
        danger
        busy={busy.has(keys.remove)}
        onCancel={() => setConfirm(null)}
        onConfirm={() => void run({
          key: keys.remove,
          scope: 'providers',
          work: () => deleteAssistantProvider(provider.id),
          success: `${provider.name} deleted.`,
          failure: `${provider.name} could not be deleted`,
        }).then(() => setConfirm(null))}
      >
        Its saved API key and model list are removed. If it serves the active model, the assistant is unavailable until you choose another one.
      </ConfirmDialog>
      <ConfirmDialog
        open={confirm === 'clear-key'}
        title={`Clear the saved key for ${provider.name}?`}
        confirmLabel="Clear key"
        danger
        busy={busy.has(keys.clearKey)}
        onCancel={() => setConfirm(null)}
        onConfirm={() => void run({
          key: keys.clearKey,
          scope: provider.id,
          work: () => updateAssistantProvider(provider.id, { clear_api_key: true }),
          success: `The saved key for ${provider.name} was cleared.`,
          failure: 'The saved key could not be cleared',
        }).then(() => setConfirm(null))}
      >
        Requests to <span style={{ fontFamily: 'var(--font-mono)' }}>{provider.destination_label}</span> are sent without a key until you enter a new one.
      </ConfirmDialog>
    </ProviderPanel>
  )
}

/* ---------- add provider ---------- */

function AddProviderCard({ busy, run, feedback }: { busy: boolean; run: RunAction; feedback: Feedback | undefined }) {
  const [name, setName] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [apiKey, setAPIKey] = useState('')
  const [insecureAck, setInsecureAck] = useState(false)
  // Errors appear once a field has been used, not on an untouched form.
  const [touched, setTouched] = useState<{ name?: boolean; url?: boolean }>({})

  const urlCheck = checkProviderBaseURL(baseURL)
  const nameError = checkProviderName(name)
  const ackMissing = urlCheck.insecure && !insecureAck
  const valid = !nameError && !urlCheck.error && !ackMissing
  const blocked = nameError
    ? 'Enter a provider name.'
    : urlCheck.error
      ? 'Enter a valid endpoint URL.'
      : ackMissing
        ? 'Acknowledge the plain-HTTP warning first.'
        : undefined
  // No complaint while the scheme itself is still being typed.
  const typed = baseURL.trim().toLowerCase()
  const typingScheme = 'https://'.startsWith(typed) || 'http://'.startsWith(typed)
  const urlError = touched.url && !typingScheme ? urlCheck.error : null

  const create = (event: FormEvent) => {
    event.preventDefault()
    if (!valid) return
    const providerName = name.trim()
    void run({
      key: 'create',
      scope: 'create',
      work: () => createAssistantProvider({
        name: providerName,
        base_url: baseURL.trim(),
        api_key: apiKey.trim(),
        insecure_transport_ack: urlCheck.insecure && insecureAck,
      }),
      success: `${providerName} added. Its models are listed under Providers.`,
      failure: 'The provider could not be added',
    }).then((ok) => {
      if (!ok) return
      setName('')
      setBaseURL('')
      setAPIKey('')
      setInsecureAck(false)
      setTouched({})
    })
  }

  return (
    <Card
      title="Add a provider"
      subtitle="Any OpenAI-compatible endpoint: a hosted API, or a model server on this machine or your network."
    >
      <form onSubmit={create} aria-label="Add a provider" noValidate style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(min(220px, 100%), 1fr))', gap: 12 }}>
          <TextField
            label="Name"
            value={name}
            onChange={(value) => {
              setName(value)
              setTouched((current) => ({ ...current, name: true }))
            }}
            placeholder="For example Local Ollama"
            error={touched.name && nameError ? nameError : undefined}
            required
          />
          <TextField
            label="Base URL"
            value={baseURL}
            onChange={(value) => {
              setBaseURL(value)
              setTouched((current) => ({ ...current, url: true }))
            }}
            placeholder="https://host.example/v1"
            hint="The address that serves /chat/completions and /models."
            error={urlError ?? undefined}
            mono
            inputMode="url"
            required
          />
        </div>
        <SecretField
          label="API key"
          value={apiKey}
          onChange={setAPIKey}
          placeholder="Optional for local servers"
          hint="Custom API keys are stored as plaintext in the local settings database (file mode 0600)."
        />
        {urlCheck.insecure && <InsecureAcknowledgement checked={insecureAck} onChange={setInsecureAck} />}
        <FeedbackNotice feedback={feedback} />
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
          <Button type="submit" variant="primary" iconLeft="plus" busy={busy} disabled={!valid} title={blocked}>Add provider</Button>
          <span style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>
            Adding a provider asks the endpoint for its model list.
          </span>
        </div>
      </form>
    </Card>
  )
}

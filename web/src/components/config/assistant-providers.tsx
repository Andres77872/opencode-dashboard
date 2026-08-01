import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Card, Notice } from '../vael'
import {
  createAssistantProvider,
  deleteAssistantProvider,
  getAssistantProviders,
  putAssistantProviderModel,
  putAssistantSelection,
  refreshAssistantProviderModels,
  updateAssistantProvider,
} from '../../lib/api'
import type { AssistantProvider, AssistantProvidersResponse } from '../../types/assistant'

export const ASSISTANT_SELECTION_EVENT = 'opencode-dashboard:assistant-selection-changed'

const fieldStyle = {
  width: '100%', minWidth: 0, padding: '9px 10px', color: 'var(--fg-primary)',
  background: 'var(--ink-850)', border: '1px solid var(--border-default)', borderRadius: 'var(--radius-md)',
} as const

function providerModelValue(providerId: string, modelId: string) {
  return `${providerId}\u0000${modelId}`
}

function splitProviderModel(value: string) {
  const [providerId = '', modelId = ''] = value.split('\u0000')
  return { providerId, modelId }
}

function catalogLabel(provider: AssistantProvider) {
  if (provider.catalog.status === 'ready') return `${provider.models.length} models discovered`
  if (provider.catalog.status === 'error') return provider.catalog.error || 'Discovery failed; last successful catalog retained'
  if (provider.catalog.status === 'stale') return 'Configuration changed; refresh required'
  return provider.reason || 'Not checked yet'
}

export function AssistantProviders() {
  const [data, setData] = useState<AssistantProvidersResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [name, setName] = useState('')
  const [baseURL, setBaseURL] = useState('https://')
  const [apiKey, setAPIKey] = useState('')
  const [insecureAck, setInsecureAck] = useState(false)

  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await getAssistantProviders(signal)
      setData(response)
      setError(null)
    } catch (caught) {
      if (!signal?.aborted) setError(caught instanceof Error ? caught.message : 'Assistant providers could not be loaded.')
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  useEffect(() => {
    if (window.location.hash !== '#assistant-providers') return
    const frame = window.requestAnimationFrame(() => document.getElementById('assistant-providers')?.scrollIntoView({ block: 'start' }))
    return () => window.cancelAnimationFrame(frame)
  }, [])

  const selectable = useMemo(() => {
    const query = search.trim().toLowerCase()
    return (data?.providers ?? []).flatMap((provider) => provider.models
      .filter((model) => model.selectable && (!query || `${provider.name} ${model.id}`.toLowerCase().includes(query)))
      .map((model) => ({ provider, model })))
  }, [data, search])

  const selectedValue = data?.selection.provider_id
    ? providerModelValue(data.selection.provider_id, data.selection.model_id)
    : ''

  const mutate = async (key: string, work: () => Promise<unknown>) => {
    setBusy(key)
    setError(null)
    try {
      await work()
      await load()
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : 'Assistant provider update failed.')
    } finally {
      setBusy(null)
    }
  }

  const select = (value: string) => {
    const { providerId, modelId } = splitProviderModel(value)
    void mutate('selection', async () => {
      await putAssistantSelection(providerId, modelId)
      window.dispatchEvent(new CustomEvent(ASSISTANT_SELECTION_EVENT))
    })
  }

  const create = (event: FormEvent) => {
    event.preventDefault()
    void mutate('create', async () => {
      await createAssistantProvider({
        name: name.trim(), base_url: baseURL.trim(), api_key: apiKey,
        insecure_transport_ack: insecureAck,
      })
      setName('')
      setBaseURL('https://')
      setAPIKey('')
      setInsecureAck(false)
    })
  }

  return (
    <section id="assistant-providers" style={{ scrollMarginTop: 20 }}>
      <Card>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div>
            <h2 style={{ margin: 0, fontSize: 17 }}>Assistant providers</h2>
            <p style={{ margin: '6px 0 0', color: 'var(--fg-muted)', fontSize: 13 }}>
              One provider and model is active globally. Changes apply to the next question in every tab and conversation; in-flight answers keep their original provider.
            </p>
          </div>

          {error && <Notice tone="warning" title="Provider settings">{error}</Notice>}

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 10 }}>
            <label style={{ display: 'grid', gap: 6, fontSize: 12 }}>
              Active provider and model
              <select
                aria-label="Active assistant provider and model"
                style={fieldStyle}
                value={selectedValue}
                disabled={loading || busy === 'selection'}
                onChange={(event) => select(event.target.value)}
              >
                <option value="">No selection</option>
                {selectable.map(({ provider, model }) => (
                  <option key={providerModelValue(provider.id, model.id)} value={providerModelValue(provider.id, model.id)}>
                    {provider.name} · {model.id} ({Math.round(model.context_limit / 1024)}k context{model.verified ? '' : ', unverified'})
                  </option>
                ))}
              </select>
            </label>
            <label style={{ display: 'grid', gap: 6, fontSize: 12 }}>
              Search models
              <input style={fieldStyle} value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Model ID" />
            </label>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(250px, 1fr))', gap: 10 }}>
            {(data?.providers ?? []).filter((provider) => provider.built_in).map((provider) => (
              <div key={provider.id} style={{ padding: 13, border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)', background: 'var(--ink-825)' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 10 }}>
                  <strong>{provider.name}</strong>
                  <span style={{ color: provider.available ? 'var(--success)' : 'var(--fg-muted)', fontSize: 11 }}>
                    {provider.available ? 'Available' : 'Needs setup'}
                  </span>
                </div>
                <p style={{ margin: '7px 0 10px', color: 'var(--fg-muted)', fontSize: 12 }}>{catalogLabel(provider)}</p>
                <button type="button" disabled={busy === provider.id} onClick={() => void mutate(provider.id, () => refreshAssistantProviderModels(provider.id))}>
                  {busy === provider.id ? 'Refreshing…' : 'Refresh models'}
                </button>
                {!provider.api_key_configured && (
                  <p style={{ margin: '10px 0 0', color: 'var(--fg-muted)', fontSize: 11 }}>
                    {provider.kind === 'kimi'
                      ? 'Set OPENCODE_DASHBOARD_KIMI_API_KEY, or sign in with Kimi Code.'
                      : 'Set OPENCODE_DASHBOARD_MINIMAX_API_KEY, or configure the existing OpenCode MiniMax login.'}
                  </p>
                )}
              </div>
            ))}
          </div>

          {(data?.providers ?? []).filter((provider) => !provider.built_in).map((provider) => (
            <CustomProviderCard key={provider.id} provider={provider} busy={busy === provider.id} mutate={(work) => mutate(provider.id, work)} />
          ))}

          <form onSubmit={create} style={{ display: 'grid', gap: 10, paddingTop: 4 }}>
            <strong>Add OpenAI-compatible provider</strong>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 10 }}>
              <input aria-label="Provider name" required maxLength={120} style={fieldStyle} value={name} onChange={(event) => setName(event.target.value)} placeholder="Provider name" />
              <input aria-label="Provider base URL" required style={fieldStyle} value={baseURL} onChange={(event) => setBaseURL(event.target.value)} placeholder="https://host.example/v1" />
            </div>
            <input aria-label="Provider API key" type="password" autoComplete="new-password" style={fieldStyle} value={apiKey} onChange={(event) => setAPIKey(event.target.value)} placeholder="API key (optional for local servers)" />
            {baseURL.trim().toLowerCase().startsWith('http://') && (
              <label style={{ display: 'flex', gap: 8, alignItems: 'flex-start', color: 'var(--warning)', fontSize: 12 }}>
                <input type="checkbox" checked={insecureAck} onChange={(event) => setInsecureAck(event.target.checked)} />
                I understand this loopback/private-LAN connection sends prompts and credentials without TLS.
              </label>
            )}
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12 }}>
              <span style={{ color: 'var(--fg-muted)', fontSize: 11 }}>Custom API keys are stored as plaintext in the local settings database (file mode 0600).</span>
              <button type="submit" disabled={busy === 'create'}>{busy === 'create' ? 'Adding…' : 'Add provider'}</button>
            </div>
          </form>
        </div>
      </Card>
    </section>
  )
}

function CustomProviderCard({ provider, busy, mutate }: { provider: AssistantProvider; busy: boolean; mutate: (work: () => Promise<unknown>) => void }) {
  const [name, setName] = useState(provider.name)
  const [baseURL, setBaseURL] = useState(provider.base_url ?? '')
  const [apiKey, setAPIKey] = useState('')
  const [insecureAck, setInsecureAck] = useState(provider.insecure_transport_ack === true)
  const [manualModel, setManualModel] = useState('')
  const [contextLimit, setContextLimit] = useState('')

  const save = () => mutate(async () => {
    await updateAssistantProvider(provider.id, {
      name: name.trim(), base_url: baseURL.trim(), insecure_transport_ack: insecureAck,
      ...(apiKey ? { api_key: apiKey } : {}),
    })
    setAPIKey('')
  })

  return (
    <div style={{ display: 'grid', gap: 10, padding: 14, border: '1px solid var(--border-default)', borderRadius: 'var(--radius-lg)', background: 'var(--ink-825)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12 }}>
        <div><strong>{provider.name}</strong><div style={{ color: 'var(--fg-muted)', fontSize: 11 }}>{provider.destination_label} · {catalogLabel(provider)}</div></div>
        <button type="button" disabled={busy} onClick={() => {
          if (window.confirm(`Delete ${provider.name}?`)) mutate(() => deleteAssistantProvider(provider.id))
        }}>Delete</button>
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 8 }}>
        <input aria-label={`${provider.name} name`} style={fieldStyle} value={name} onChange={(event) => setName(event.target.value)} />
        <input aria-label={`${provider.name} base URL`} style={fieldStyle} value={baseURL} onChange={(event) => setBaseURL(event.target.value)} />
      </div>
      <input aria-label={`${provider.name} replacement API key`} type="password" autoComplete="new-password" style={fieldStyle} value={apiKey} onChange={(event) => setAPIKey(event.target.value)} placeholder={provider.api_key_configured ? 'Replacement API key (leave blank to preserve)' : 'API key (optional)'} />
      {baseURL.trim().toLowerCase().startsWith('http://') && (
        <label style={{ display: 'flex', gap: 8, fontSize: 12, color: 'var(--warning)' }}>
          <input type="checkbox" checked={insecureAck} onChange={(event) => setInsecureAck(event.target.checked)} /> Acknowledge insecure loopback/private-LAN HTTP
        </label>
      )}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" disabled={busy} onClick={save}>Save</button>
        <button type="button" disabled={busy} onClick={() => mutate(() => refreshAssistantProviderModels(provider.id))}>Refresh models</button>
        {provider.api_key_configured && <button type="button" disabled={busy} onClick={() => mutate(() => updateAssistantProvider(provider.id, { clear_api_key: true }))}>Clear key</button>}
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 8, alignItems: 'center' }}>
        <input aria-label={`${provider.name} manual model ID`} style={fieldStyle} value={manualModel} onChange={(event) => setManualModel(event.target.value)} placeholder="Exact manual model ID" />
        <input aria-label={`${provider.name} context limit`} type="number" min={1024} max={16000000} style={fieldStyle} value={contextLimit} onChange={(event) => setContextLimit(event.target.value)} placeholder="Context tokens" />
        <button type="button" disabled={busy || !manualModel.trim() || Number(contextLimit) < 1024} onClick={() => mutate(() => putAssistantProviderModel(provider.id, manualModel.trim(), Number(contextLimit)))}>Add model</button>
      </div>
      {provider.models.length > 0 && (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
          {provider.models.slice(0, 30).map((model) => (
            <span key={model.id} style={{ padding: '4px 7px', border: '1px solid var(--border-default)', borderRadius: 99, color: model.selectable ? 'var(--fg-secondary)' : 'var(--warning)', fontSize: 11 }}>
              {model.id} · {model.context_limit ? `${Math.round(model.context_limit / 1024)}k` : 'context required'}{model.verified ? '' : ' · unverified'}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

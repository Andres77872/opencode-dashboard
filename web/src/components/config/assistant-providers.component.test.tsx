import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AssistantProviders } from './assistant-providers'
import type { AssistantProvidersResponse } from '../../types/assistant'

function responseBody(selectionRevision = 4): AssistantProvidersResponse {
  return {
    selection: { provider_id: 'minimax', model_id: 'MiniMax-M3', revision: selectionRevision, updated_ms: 1 },
    providers: [
      {
        id: 'minimax', kind: 'minimax', name: 'MiniMax', built_in: true,
        base_url: 'https://api.minimax.io/v1', destination_label: 'https://api.minimax.io',
        available: true, api_key_configured: true, catalog: { status: 'ready' },
        models: [{ id: 'MiniMax-M3', context_limit: 262144, verified: true, selectable: true }],
      },
      {
        id: 'custom_local', kind: 'custom', name: 'Local model', built_in: false,
        base_url: 'http://127.0.0.1:8080/v1', destination_label: 'http://127.0.0.1:8080',
        available: true, api_key_configured: false, insecure_transport_ack: true,
        catalog: { status: 'error', error: 'model discovery failed; the last successful catalog was kept' },
        models: [{ id: 'manual-model', context_limit: 32768, verified: false, selectable: true }],
      },
    ],
  }
}

describe('AssistantProviders', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('switches the global next-turn selection and labels manual models unverified', async () => {
    let data = responseBody()
    const requests: Array<{ path: string; method: string; body: string }> = []
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      const method = init?.method ?? 'GET'
      const body = String(init?.body ?? '')
      requests.push({ path, method, body })
      if (path.endsWith('/api/v1/assistant/selection') && method === 'PUT') {
        data = { ...data, selection: { provider_id: 'custom_local', model_id: 'manual-model', revision: 5, updated_ms: 2 } }
        return new Response(JSON.stringify(data.selection), { status: 200, headers: { 'Content-Type': 'application/json' } })
      }
      return new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }))

    render(<AssistantProviders />)
    const selector = await screen.findByLabelText('Active assistant provider and model') as HTMLSelectElement
    expect(screen.getByText(/plaintext in the local settings database/i)).toBeTruthy()
    expect(screen.getByText(/manual-model · 32k · unverified/i)).toBeTruthy()

    fireEvent.change(selector, { target: { value: 'custom_local\u0000manual-model' } })
    await waitFor(() => expect(selector.value).toBe('custom_local\u0000manual-model'))
    const update = requests.find((request) => request.path.endsWith('/assistant/selection') && request.method === 'PUT')
    expect(JSON.parse(update?.body ?? '{}')).toEqual({ provider_id: 'custom_local', model_id: 'manual-model' })
  })

  it('requires an explicit warning acknowledgement for an HTTP custom endpoint', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(responseBody()), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    render(<AssistantProviders />)
    const baseURL = await screen.findByLabelText('Provider base URL')
    fireEvent.change(baseURL, { target: { value: 'http://192.168.1.10:11434/v1' } })
    const acknowledgement = screen.getByLabelText(/I understand this loopback\/private-LAN connection/i) as HTMLInputElement
    expect(acknowledgement.checked).toBe(false)
    fireEvent.click(acknowledgement)
    expect(acknowledgement.checked).toBe(true)
  })

  it('treats legacy null model catalogs as empty arrays', async () => {
    const data = responseBody()
    // Older binaries encoded an empty built-in slice as null even though the
    // public TypeScript contract has always described an array.
    ;(data.providers[0] as unknown as { models: null }).models = null
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(data), { status: 200, headers: { 'Content-Type': 'application/json' } })))

    render(<AssistantProviders />)

    await screen.findByLabelText('Active assistant provider and model')
    expect(screen.getByText('0 models discovered')).toBeTruthy()
  })
})

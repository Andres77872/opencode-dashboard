import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AssistantProviders } from './assistant-providers'
import { resetAssistantProvidersStore } from '../../lib/assistant-providers-store'
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
        available: true, api_key_configured: true, insecure_transport_ack: true,
        catalog: { status: 'error', error: 'model discovery failed; the last successful catalog was kept' },
        models: [{ id: 'manual-model', context_limit: 32768, verified: false, selectable: true }],
      },
    ],
  }
}

interface RecordedRequest {
  path: string
  method: string
  body: string
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

/** Serves the provider catalog and records every request; `handle` may answer a mutation. */
function stubApi(data: AssistantProvidersResponse, handle?: (request: RecordedRequest) => Response | undefined) {
  const requests: RecordedRequest[] = []
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = { path: String(input), method: init?.method ?? 'GET', body: String(init?.body ?? '') }
    requests.push(request)
    return handle?.(request) ?? jsonResponse(data)
  }))
  return requests
}

const addForm = () => within(screen.getByRole('form', { name: 'Add a provider' }))

describe('AssistantProviders', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    // The catalog is a shared store; each test must load its own.
    resetAssistantProvidersStore()
  })

  it('switches the global next-turn selection and labels manual models unverified', async () => {
    let data = responseBody()
    const requests = stubApi(data, (request) => {
      if (request.path.endsWith('/api/v1/assistant/selection') && request.method === 'PUT') {
        data = { ...data, selection: { provider_id: 'custom_local', model_id: 'manual-model', revision: 5, updated_ms: 2 } }
        return jsonResponse(data.selection)
      }
      return undefined
    })

    render(<AssistantProviders />)
    const picker = await screen.findByLabelText('Active assistant provider and model')
    expect(picker.textContent).toContain('MiniMax-M3')
    expect(screen.getByText(/plaintext in the local settings database/i)).toBeTruthy()
    expect(screen.getByText(/manual-model · 32k · unverified/i)).toBeTruthy()

    fireEvent.click(picker)
    // Searching narrows the list without losing the current choice on the trigger.
    fireEvent.change(await screen.findByLabelText('Search providers and models'), { target: { value: 'manual' } })
    expect(picker.textContent).toContain('MiniMax-M3')
    fireEvent.click(screen.getByRole('option', { name: /manual-model/ }))

    await waitFor(() => expect(picker.textContent).toContain('manual-model'))
    const update = requests.find((request) => request.path.endsWith('/assistant/selection') && request.method === 'PUT')
    expect(JSON.parse(update?.body ?? '{}')).toEqual({ provider_id: 'custom_local', model_id: 'manual-model' })
    // The confirmed selection is applied in place: the catalog is not fetched again.
    expect(requests.filter((request) => request.method === 'GET')).toHaveLength(1)
  })

  it('requires an explicit warning acknowledgement for an HTTP custom endpoint', async () => {
    stubApi(responseBody())
    render(<AssistantProviders />)
    await screen.findByLabelText('Active assistant provider and model')

    const form = addForm()
    const submit = form.getByRole('button', { name: 'Add provider' }) as HTMLButtonElement
    expect(submit.disabled).toBe(true)

    fireEvent.change(form.getByLabelText(/^Name/), { target: { value: 'LAN box' } })
    fireEvent.change(form.getByLabelText(/^Base URL/), { target: { value: 'http://192.168.1.10:11434/v1' } })
    const acknowledgement = form.getByLabelText(/I understand this loopback\/private-LAN connection/i) as HTMLInputElement
    expect(acknowledgement.checked).toBe(false)
    expect(submit.disabled).toBe(true)

    fireEvent.click(acknowledgement)
    expect(acknowledgement.checked).toBe(true)
    expect(submit.disabled).toBe(false)
  })

  it('rejects endpoints the server would refuse before sending anything', async () => {
    const requests = stubApi(responseBody())
    render(<AssistantProviders />)
    await screen.findByLabelText('Active assistant provider and model')

    const form = addForm()
    const submit = form.getByRole('button', { name: 'Add provider' }) as HTMLButtonElement
    const url = form.getByLabelText(/^Base URL/)
    fireEvent.change(form.getByLabelText(/^Name/), { target: { value: 'Somewhere' } })

    // Plain HTTP to a public host is never acceptable, acknowledged or not.
    fireEvent.change(url, { target: { value: 'http://example.com/v1' } })
    expect(form.getByRole('alert').textContent).toMatch(/Plain HTTP is accepted only for localhost/)
    expect(submit.disabled).toBe(true)

    // A bare scheme used to satisfy `required`.
    fireEvent.change(url, { target: { value: 'https://' } })
    expect(submit.disabled).toBe(true)

    fireEvent.change(url, { target: { value: 'https://user:secret@example.com/v1' } })
    expect(form.getByRole('alert').textContent).toMatch(/Remove the credentials/)

    fireEvent.change(url, { target: { value: '  https://example.com/v1  ' } })
    fireEvent.change(form.getByLabelText(/^API key/), { target: { value: '  sk-test  ' } })
    expect(submit.disabled).toBe(false)
    fireEvent.click(submit)

    await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true))
    const created = requests.find((request) => request.method === 'POST')
    expect(JSON.parse(created?.body ?? '{}')).toEqual({
      name: 'Somewhere', base_url: 'https://example.com/v1', api_key: 'sk-test', insecure_transport_ack: false,
    })
    await screen.findByText(/Somewhere added/)
  })

  it('saves only what changed and asks for the key again when the host moves', async () => {
    const requests = stubApi(responseBody(), (request) => {
      if (request.method === 'PATCH') return jsonResponse({ ...responseBody().providers[1], name: 'Renamed' })
      return undefined
    })
    render(<AssistantProviders />)
    const panel = within(await screen.findByRole('region', { name: 'Local model' }))
    fireEvent.click(panel.getByRole('button', { name: 'Edit' }))

    const save = panel.getByRole('button', { name: 'Save changes' }) as HTMLButtonElement
    expect(save.disabled).toBe(true)

    // Moving to another host without re-entering the key is blocked.
    fireEvent.change(panel.getByLabelText(/^Base URL/), { target: { value: 'http://127.0.0.1:9090/v1' } })
    expect(panel.getByRole('alert').textContent).toMatch(/never sent to a different host/)
    expect(save.disabled).toBe(true)

    fireEvent.change(panel.getByLabelText(/^Base URL/), { target: { value: 'http://127.0.0.1:8080/v1' } })
    fireEvent.change(panel.getByLabelText(/^Name/), { target: { value: 'Renamed ' } })
    expect(save.disabled).toBe(false)
    fireEvent.click(save)

    await waitFor(() => expect(requests.some((request) => request.method === 'PATCH')).toBe(true))
    const patch = requests.find((request) => request.method === 'PATCH')
    expect(JSON.parse(patch?.body ?? '{}')).toEqual({ name: 'Renamed' })
  })

  it('confirms before deleting a provider', async () => {
    const requests = stubApi(responseBody(), (request) => (request.method === 'DELETE' ? new Response(null, { status: 204 }) : undefined))
    render(<AssistantProviders />)
    const panel = within(await screen.findByRole('region', { name: 'Local model' }))

    fireEvent.click(panel.getByRole('button', { name: 'Delete' }))
    const dialog = within(await screen.findByRole('alertdialog'))
    expect(requests.some((request) => request.method === 'DELETE')).toBe(false)

    fireEvent.click(dialog.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
    expect(requests.some((request) => request.method === 'DELETE')).toBe(false)

    fireEvent.click(panel.getByRole('button', { name: 'Delete' }))
    fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Delete provider' }))
    await waitFor(() => expect(requests.some((request) => request.method === 'DELETE' && request.path.endsWith('/assistant/providers/custom_local'))).toBe(true))
  })

  it('treats legacy null model catalogs as empty arrays', async () => {
    const data = responseBody()
    // Older binaries encoded an empty built-in slice as null even though the
    // public TypeScript contract has always described an array.
    ;(data.providers[0] as unknown as { models: null }).models = null
    stubApi(data)

    render(<AssistantProviders />)

    await screen.findByLabelText('Active assistant provider and model')
    expect(screen.getByText('0 models discovered')).toBeTruthy()
  })
})

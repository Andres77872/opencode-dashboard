/* Pure helpers for the assistant provider forms: the same checks the server
   applies to a provider endpoint, run before submitting so a mistake is
   explained next to the field instead of coming back as a failed request. */
import { asSentence } from './text'
import type { AssistantProvider, AssistantProviderModel } from '../../types/assistant'

export const MIN_CONTEXT_LIMIT = 1024
export const MAX_CONTEXT_LIMIT = 16_000_000
const MAX_PROVIDER_NAME_LENGTH = 120

export interface BaseURLCheck {
  /** Why the URL cannot be used, or null when it can. */
  error: string | null
  /** True for an accepted plain-HTTP endpoint, which needs an explicit acknowledgement. */
  insecure: boolean
  /** scheme://host[:port], used to tell whether an edit moves to another host. */
  origin: string | null
}

function isLoopbackOrPrivateHost(hostname: string): boolean {
  const host = hostname.toLowerCase().replace(/^\[|\]$/g, '')
  if (host === 'localhost') return true
  const v4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host)
  if (v4) {
    const [a, b] = [Number(v4[1]), Number(v4[2])]
    if ([a, b, Number(v4[3]), Number(v4[4])].some((part) => part > 255)) return false
    return a === 127 || a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)
  }
  // IPv6 loopback and unique-local (fc00::/7).
  return host === '::1' || /^f[cd][0-9a-f]{2}:/.test(host)
}

/**
 * Validate a provider base URL the way the server does: an absolute HTTP(S)
 * URL without credentials, query or fragment, where plain HTTP is accepted
 * only for localhost or a private-network IP address.
 */
export function checkProviderBaseURL(raw: string): BaseURLCheck {
  const value = raw.trim()
  const invalid = (error: string): BaseURLCheck => ({ error, insecure: false, origin: null })
  if (!value) return invalid('Enter the endpoint URL, for example https://host.example/v1.')

  let parsed: URL
  try {
    parsed = new URL(value)
  } catch {
    return invalid('Enter a full URL that starts with https:// or http://.')
  }
  if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return invalid('The URL must start with https:// or http://.')
  if (!parsed.hostname) return invalid('The URL needs a host, for example https://host.example/v1.')
  if (parsed.username || parsed.password) return invalid('Remove the credentials from the URL — the API key has its own field.')
  if (parsed.search || parsed.hash) return invalid('The URL cannot contain a query string or a fragment.')
  if (parsed.protocol === 'http:' && !isLoopbackOrPrivateHost(parsed.hostname)) {
    return invalid('Plain HTTP is accepted only for localhost or a private-network IP address. Use https:// for this host.')
  }
  return { error: null, insecure: parsed.protocol === 'http:', origin: parsed.origin }
}

export function checkProviderName(raw: string): string | null {
  const value = raw.trim()
  if (!value) return 'Give the provider a name.'
  if (value.length > MAX_PROVIDER_NAME_LENGTH) return `Use ${MAX_PROVIDER_NAME_LENGTH} characters or fewer.`
  return null
}

/** Parse a context-window entry; null unless it is a whole number of tokens in range. */
export function parseContextLimit(raw: string): number | null {
  const value = raw.trim().replace(/[,_\s]/g, '')
  if (!/^\d+$/.test(value)) return null
  const limit = Number(value)
  return limit >= MIN_CONTEXT_LIMIT && limit <= MAX_CONTEXT_LIMIT ? limit : null
}

/** "32k" / "256k" / "1M" — a context window at a glance. */
export function formatContextLimit(limit: number): string {
  const k = Math.round(limit / 1024)
  if (k >= 1000) return `${Number((k / 1024).toFixed(1))}M`
  return `${k}k`
}

/** One line describing a model in a list: id, context window, and whether the window was verified. */
export function describeModel(model: AssistantProviderModel): string {
  const context = model.context_limit ? formatContextLimit(model.context_limit) : 'context required'
  return `${model.id} · ${context}${model.verified ? '' : ' · unverified'}`
}

/** What the provider's model catalog currently is, in one sentence. */
export function catalogLabel(provider: AssistantProvider): string {
  const count = provider.models.length
  if (provider.catalog.status === 'ready') return `${count} ${count === 1 ? 'model' : 'models'} discovered`
  if (provider.catalog.status === 'error') return asSentence(provider.catalog.error || 'Discovery failed; the last successful catalog was kept')
  if (provider.catalog.status === 'stale') return 'Configuration changed — refresh the models.'
  return asSentence(provider.reason || 'Not checked yet')
}

/** Stable value for one provider/model pair (ids never contain NUL). */
export function providerModelValue(providerId: string, modelId: string) {
  return `${providerId}\u0000${modelId}`
}

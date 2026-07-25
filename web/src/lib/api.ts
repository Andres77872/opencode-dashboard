import type {
  AllSourcesModelUsage,
  AllSourcesOverview,
  ApiErrorResponse,
  CacheStatusResponse,
  CacheSyncMode,
  ConfigStats,
  DailyDimensionStats,
  DailyStats,
  MessageDetail,
  MessageList,
  ModelStats,
  OverviewStats,
  ProjectDetail,
  ProjectStats,
  QuotasResponse,
  SessionDetail,
  SessionList,
  SessionSortMode,
  SourceID,
  SourceListResponse,
  ToolStats,
} from '../types/api'
import type {
  AssistantChatRequest,
  AssistantChatResponse,
  AssistantChatSessionDetail,
  AssistantChatSessionListResponse,
  AssistantStatusResponse,
  AssistantStreamCompleteEvent,
  AssistantStreamEvent,
} from '../types/assistant'
import { AssistantStreamProtocolError, readAssistantStream } from './assistant-stream.ts'

const DEFAULT_API_BASE_URL = import.meta.env?.VITE_API_BASE_URL?.trim() ?? ''

/**
 * Module-level flag for HTTP cache bypass, scoped by `withBypassCache`.
 */
let _bypassCache = false

/**
 * Run `fn`, making every request it *initiates synchronously* bypass the
 * browser HTTP cache (`cache: 'no-cache'`). The flag is read before fetch()
 * is called and restored before returning, so in single-threaded JS the scope
 * is deterministic: parallel refresh-triggered fetches all get the bypass and
 * nothing started outside the callback does. (The old one-shot flag was
 * consumed by whichever concurrent request ran first, letting a refresh
 * replay a stale cached body for the rest.)
 */
export function withBypassCache<T>(fn: () => T): T {
  _bypassCache = true
  try {
    return fn()
  } finally {
    _bypassCache = false
  }
}

export class ApiClientError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiClientError'
    this.status = status
  }
}

function resolveUrl(path: string) {
  if (!DEFAULT_API_BASE_URL) {
    return path
  }

  return `${DEFAULT_API_BASE_URL}${path}`
}

async function parseError(response: Response) {
  try {
    const payload = (await response.json()) as ApiErrorResponse
    return payload.message || payload.error || `Request failed with ${response.status}`
  } catch {
    return `Request failed with ${response.status}`
  }
}

async function request<T>(path: string, init?: RequestInit) {
  const fetchInit: RequestInit = {
    ...init,
    headers: {
      Accept: 'application/json',
      ...init?.headers,
    },
  }

  // When a user-initiated refresh triggered this request, bypass HTTP cache.
  // Read-only: the flag is scoped by withBypassCache, never consumed here.
  if (_bypassCache) {
    fetchInit.cache = 'no-cache'
  }

  const response = await fetch(resolveUrl(path), fetchInit)

  if (!response.ok) {
    throw new ApiClientError(await parseError(response), response.status)
  }

  return (await response.json()) as T
}

/**
 * Builds a URL with the correct query parameters for a given period/custom range key.
 *
 * If the key starts with "from_", it is a serialized custom range key:
 *   "from_2026-04-01_to_2026-04-15" → ?from=2026-04-01&to=2026-04-15
 *   "from_2026-04-01_to__now__"     → ?from=2026-04-01
 *   "from_2026-04-01_to_"           → ?from=2026-04-01
 *
 * Otherwise, it is a preset period key:
 *   "7d" → ?period=7d
 */
function addSourceParam(params: URLSearchParams, sourceId?: SourceID) {
  if (sourceId && sourceId !== 'opencode') {
    params.set('source', sourceId)
  }
}

function buildUrl(basePath: string, period: string, extraParams?: Record<string, string>, sourceId?: SourceID): string {
  const params = new URLSearchParams(extraParams)
  addSourceParam(params, sourceId)

  if (period.startsWith('from_')) {
    // Parse custom range: "from_YYYY-MM-DD_to_YYYY-MM-DD" or "from_YYYY-MM-DD_to__now__"
    const parts = period.replace('from_', '').split('_to_')
    params.set('from', parts[0])
    if (parts[1] && parts[1] !== '__now__' && parts[1] !== '') {
      params.set('to', parts[1])
    }
  } else {
    params.set('period', period)
  }

  return `${basePath}?${params.toString()}`
}

function buildDetailUrl(basePath: string, sourceId?: SourceID): string {
  const params = new URLSearchParams()
  addSourceParam(params, sourceId)

  const query = params.toString()
  return query ? `${basePath}?${query}` : basePath
}

export function getSources(signal?: AbortSignal) {
  return request<SourceListResponse>('/api/v1/sources', { signal })
}

export function getCacheStatus(signal?: AbortSignal) {
  return request<CacheStatusResponse>('/api/v1/cache', { signal })
}

export function syncCache(sourceId?: SourceID, mode: CacheSyncMode = 'incremental', signal?: AbortSignal) {
  const params = new URLSearchParams()
  if (sourceId) {
    params.set('source', sourceId)
  }
  params.set('mode', mode)
  const query = params.toString()
  return request<CacheStatusResponse>(`/api/v1/cache/sync${query ? `?${query}` : ''}`, {
    method: 'POST',
    signal,
  })
}

/**
 * Optional model/provider filter for overview, daily, and messages requests.
 * Only cache-backed reads honor it; filtered token totals are model-attributed.
 */
export interface ModelFilterParams {
  model?: string
  provider?: string
}

function withModelFilter(
  extra: Record<string, string> | undefined,
  filter?: ModelFilterParams,
): Record<string, string> | undefined {
  if (!filter?.model && !filter?.provider) return extra
  const params = { ...(extra ?? {}) }
  if (filter.model) params.model = filter.model
  if (filter.provider) params.provider = filter.provider
  return params
}

export function getOverview(period: string, signal?: AbortSignal, sourceId?: SourceID, filter?: ModelFilterParams) {
  return request<OverviewStats>(buildUrl('/api/v1/overview', period, withModelFilter(undefined, filter), sourceId), { signal })
}

// getOverviewAll fetches the cross-source aggregate. It deliberately takes no
// sourceId — the Overview view spans every source.
export function getOverviewAll(period: string, signal?: AbortSignal) {
  return request<AllSourcesOverview>(
    buildUrl('/api/v1/overview/all', period, { trend: 'true', top: '10' }),
    { signal },
  )
}

/**
 * Fetch the lean, model-dimension payload used by the Overview usage switch.
 * This is deliberately separate from getOverviewAll: the base overview renders
 * immediately and model totals/trends are requested only when the user asks for
 * them. Consumers must use only model_usage/model_trend and partial errors from
 * this response; the server may omit the normal overview roll-ups.
 */
export function getOverviewAllModels(period: string, signal?: AbortSignal) {
  return request<AllSourcesModelUsage>(
    buildUrl('/api/v1/overview/all', period, { trend: 'true', dimension: 'model' }),
    { signal },
  )
}

export function getDaily(period: string, signal?: AbortSignal, sourceId?: SourceID, filter?: ModelFilterParams) {
  return request<DailyStats>(buildUrl('/api/v1/daily', period, withModelFilter(undefined, filter), sourceId), { signal })
}

export function getModels(period: string, signal?: AbortSignal, sourceId?: SourceID) {
  return request<ModelStats>(buildUrl('/api/v1/models', period, undefined, sourceId), { signal })
}

export function getTools(period: string, signal?: AbortSignal, sourceId?: SourceID) {
  return request<ToolStats>(buildUrl('/api/v1/tools', period, undefined, sourceId), { signal })
}

export function getProjects(period: string, signal?: AbortSignal, sourceId?: SourceID) {
  return request<ProjectStats>(buildUrl('/api/v1/projects', period, undefined, sourceId), { signal })
}

export function getConfig(signal?: AbortSignal, sourceId?: SourceID) {
  return request<ConfigStats>(buildDetailUrl('/api/v1/config', sourceId), { signal })
}

export function getSessions(
  page: number,
  limit: number,
  period: string,
  signal?: AbortSignal,
  sourceId?: SourceID,
) {
  return request<SessionList>(
    buildUrl('/api/v1/sessions', period, { page: String(page), limit: String(limit) }, sourceId),
    { signal },
  )
}

export function getSessionsWithFilter(
  page: number,
  limit: number,
  period: string,
  filter?: string,
  projectId?: string,
  signal?: AbortSignal,
  sourceId?: SourceID,
  sort?: SessionSortMode,
) {
  const extraParams: Record<string, string> = {
    page: String(page),
    limit: String(limit),
  }

  if (filter) {
    extraParams.filter = filter
  }

  if (projectId) {
    extraParams.project_id = projectId
  }

  // Sorting is server-side: the endpoint pages the result set, so ordering only
  // the current page's rows would be wrong.
  if (sort) {
    extraParams.sort = sort
  }

  return request<SessionList>(buildUrl('/api/v1/sessions', period, extraParams, sourceId), { signal })
}

export function getDailyDimension(dimension: string, period: string, signal?: AbortSignal, sourceId?: SourceID) {
  return request<DailyDimensionStats>(buildUrl('/api/v1/daily', period, { dimension }, sourceId), { signal })
}

export function getProjectDetail(id: string, period: string, page?: number, limit?: number, signal?: AbortSignal, sourceId?: SourceID) {
  const extraParams: Record<string, string> = {}

  if (page !== undefined) {
    extraParams.page = String(page)
  }

  if (limit !== undefined) {
    extraParams.limit = String(limit)
  }

  return request<ProjectDetail>(
    buildUrl(`/api/v1/projects/${encodeURIComponent(id)}`, period, extraParams, sourceId),
    { signal },
  )
}

export function getSessionDetail(id: string, signal?: AbortSignal, sourceId?: SourceID) {
  return request<SessionDetail>(buildDetailUrl(`/api/v1/sessions/${encodeURIComponent(id)}`, sourceId), { signal })
}

export function getMessages(period: string, page: number, limit: number, sort?: string, signal?: AbortSignal, sourceId?: SourceID, filter?: ModelFilterParams) {
  const extraParams: Record<string, string> = {
    page: String(page),
    limit: String(limit),
  }

  if (sort) {
    extraParams.sort = sort
  }

  return request<MessageList>(buildUrl('/api/v1/messages', period, withModelFilter(extraParams, filter), sourceId), { signal })
}

export function getMessageDetail(id: string, signal?: AbortSignal, sourceId?: SourceID) {
  return request<MessageDetail>(buildDetailUrl(`/api/v1/messages/${encodeURIComponent(id)}`, sourceId), { signal })
}

export function getQuotas(signal?: AbortSignal) {
  return request<QuotasResponse>('/api/v1/quotas', { signal })
}

export function getAssistantStatus(signal?: AbortSignal) {
  return request<AssistantStatusResponse>('/api/v1/assistant/status', { signal })
}

export function getAssistantSessions(signal?: AbortSignal) {
  return request<AssistantChatSessionListResponse>('/api/v1/assistant/sessions', { signal })
}

export function getAssistantSession(id: string, signal?: AbortSignal) {
  return request<AssistantChatSessionDetail>(`/api/v1/assistant/sessions/${encodeURIComponent(id)}`, { signal })
}

export async function deleteAssistantSession(id: string, signal?: AbortSignal) {
  const response = await fetch(resolveUrl(`/api/v1/assistant/sessions/${encodeURIComponent(id)}`), {
    method: 'DELETE',
    signal,
  })
  if (!response.ok) {
    throw new ApiClientError(await parseError(response), response.status)
  }
}

export function sendAssistantChat(payload: AssistantChatRequest, signal?: AbortSignal) {
  return request<AssistantChatResponse>('/api/v1/assistant/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
    signal,
  })
}

export async function streamAssistantChat(
  payload: AssistantChatRequest,
  onEvent: (event: AssistantStreamEvent) => void,
  signal?: AbortSignal,
): Promise<AssistantStreamCompleteEvent> {
  const response = await fetch(resolveUrl('/api/v1/assistant/chat/stream'), {
    method: 'POST',
    headers: {
      Accept: 'application/x-ndjson',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
    cache: 'no-store',
    signal,
  })

  if (!response.ok) {
    throw new ApiClientError(await parseError(response), response.status)
  }
  if (!response.body) {
    throw new AssistantStreamProtocolError('Assistant stream response has no body')
  }

  return readAssistantStream(response.body, onEvent)
}

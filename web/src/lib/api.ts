import type {
  ApiErrorResponse,
  ConfigStats,
  DailyDimensionStats,
  DailyPeriod,
  DailyStats,
  MessageDetail,
  MessageList,
  ModelStats,
  OverviewStats,
  ProjectDetail,
  ProjectStats,
  SessionDetail,
  SessionList,
  ToolStats,
} from '../types/api'

const DEFAULT_API_BASE_URL = import.meta.env.VITE_API_BASE_URL?.trim() ?? ''

/**
 * Module-level flag for HTTP cache bypass.
 * Set to true before a refresh-triggered fetch to make the next `request()` call
 * pass `cache: 'no-cache'` to fetch(). Reset to false after each request.
 */
let _bypassCache = false

/**
 * Enable HTTP cache bypass for the next request made via `request()`.
 * Used by usePeriodResource when refreshNonce triggers a re-fetch.
 */
export function setBypassCache(value: boolean) {
  _bypassCache = value
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

  // When the user-initiated refresh has triggered this request, bypass HTTP cache
  if (_bypassCache) {
    fetchInit.cache = 'no-cache'
    _bypassCache = false
  }

  const response = await fetch(resolveUrl(path), fetchInit)

  if (!response.ok) {
    throw new ApiClientError(await parseError(response), response.status)
  }

  return (await response.json()) as T
}

export function getOverview(period: DailyPeriod, signal?: AbortSignal) {
  const params = new URLSearchParams({ period })
  return request<OverviewStats>(`/api/v1/overview?${params.toString()}`, { signal })
}

export function getDaily(period: DailyPeriod, signal?: AbortSignal) {
  const params = new URLSearchParams({ period })
  return request<DailyStats>(`/api/v1/daily?${params.toString()}`, { signal })
}

export function getModels(period: DailyPeriod, signal?: AbortSignal) {
  const params = new URLSearchParams({ period })
  return request<ModelStats>(`/api/v1/models?${params.toString()}`, { signal })
}

export function getTools(period: DailyPeriod, signal?: AbortSignal) {
  const params = new URLSearchParams({ period })
  return request<ToolStats>(`/api/v1/tools?${params.toString()}`, { signal })
}

export function getProjects(period: DailyPeriod, signal?: AbortSignal) {
  const params = new URLSearchParams({ period })
  return request<ProjectStats>(`/api/v1/projects?${params.toString()}`, { signal })
}

export function getConfig(signal?: AbortSignal) {
  return request<ConfigStats>('/api/v1/config', { signal })
}

export function getSessions(
  page: number,
  limit: number,
  period: DailyPeriod,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({
    page: String(page),
    limit: String(limit),
    period,
  })

  return request<SessionList>(`/api/v1/sessions?${params.toString()}`, { signal })
}

export function getSessionsWithFilter(
  page: number,
  limit: number,
  period: DailyPeriod,
  filter?: string,
  projectId?: string,
  signal?: AbortSignal,
) {
  const params = new URLSearchParams({
    page: String(page),
    limit: String(limit),
    period,
  })

  if (filter) {
    params.set('filter', filter)
  }

  if (projectId) {
    params.set('project_id', projectId)
  }

  return request<SessionList>(`/api/v1/sessions?${params.toString()}`, { signal })
}

export function getDailyDimension(dimension: string, period: DailyPeriod, signal?: AbortSignal) {
  const params = new URLSearchParams({ dimension, period })
  return request<DailyDimensionStats>(`/api/v1/daily?${params.toString()}`, { signal })
}

export function getProjectDetail(id: string, period: DailyPeriod, page?: number, limit?: number, signal?: AbortSignal) {
  const params = new URLSearchParams({ period })

  if (page !== undefined) {
    params.set('page', String(page))
  }

  if (limit !== undefined) {
    params.set('limit', String(limit))
  }

  return request<ProjectDetail>(`/api/v1/projects/${encodeURIComponent(id)}?${params.toString()}`, { signal })
}

export function getSessionDetail(id: string, signal?: AbortSignal) {
  return request<SessionDetail>(`/api/v1/sessions/${encodeURIComponent(id)}`, { signal })
}

export function getMessages(period: DailyPeriod, page: number, limit: number, sort?: string, signal?: AbortSignal) {
  const params = new URLSearchParams({
    period,
    page: String(page),
    limit: String(limit),
  })

  if (sort) {
    params.set('sort', sort)
  }

  return request<MessageList>(`/api/v1/messages?${params.toString()}`, { signal })
}

export function getMessageDetail(id: string, signal?: AbortSignal) {
  return request<MessageDetail>(`/api/v1/messages/${encodeURIComponent(id)}`, { signal })
}

import type {
  ApiErrorResponse,
  ConfigStats,
  DailyDimensionStats,
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
function buildUrl(basePath: string, period: string, extraParams?: Record<string, string>): string {
  const params = new URLSearchParams(extraParams)

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

export function getOverview(period: string, signal?: AbortSignal) {
  return request<OverviewStats>(buildUrl('/api/v1/overview', period), { signal })
}

export function getDaily(period: string, signal?: AbortSignal) {
  return request<DailyStats>(buildUrl('/api/v1/daily', period), { signal })
}

export function getModels(period: string, signal?: AbortSignal) {
  return request<ModelStats>(buildUrl('/api/v1/models', period), { signal })
}

export function getTools(period: string, signal?: AbortSignal) {
  return request<ToolStats>(buildUrl('/api/v1/tools', period), { signal })
}

export function getProjects(period: string, signal?: AbortSignal) {
  return request<ProjectStats>(buildUrl('/api/v1/projects', period), { signal })
}

export function getConfig(signal?: AbortSignal) {
  return request<ConfigStats>('/api/v1/config', { signal })
}

export function getSessions(
  page: number,
  limit: number,
  period: string,
  signal?: AbortSignal,
) {
  return request<SessionList>(
    buildUrl('/api/v1/sessions', period, { page: String(page), limit: String(limit) }),
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

  return request<SessionList>(buildUrl('/api/v1/sessions', period, extraParams), { signal })
}

export function getDailyDimension(dimension: string, period: string, signal?: AbortSignal) {
  return request<DailyDimensionStats>(buildUrl('/api/v1/daily', period, { dimension }), { signal })
}

export function getProjectDetail(id: string, period: string, page?: number, limit?: number, signal?: AbortSignal) {
  const extraParams: Record<string, string> = {}

  if (page !== undefined) {
    extraParams.page = String(page)
  }

  if (limit !== undefined) {
    extraParams.limit = String(limit)
  }

  return request<ProjectDetail>(
    buildUrl(`/api/v1/projects/${encodeURIComponent(id)}`, period, extraParams),
    { signal },
  )
}

export function getSessionDetail(id: string, signal?: AbortSignal) {
  return request<SessionDetail>(`/api/v1/sessions/${encodeURIComponent(id)}`, { signal })
}

export function getMessages(period: string, page: number, limit: number, sort?: string, signal?: AbortSignal) {
  const extraParams: Record<string, string> = {
    page: String(page),
    limit: String(limit),
  }

  if (sort) {
    extraParams.sort = sort
  }

  return request<MessageList>(buildUrl('/api/v1/messages', period, extraParams), { signal })
}

export function getMessageDetail(id: string, signal?: AbortSignal) {
  return request<MessageDetail>(`/api/v1/messages/${encodeURIComponent(id)}`, { signal })
}

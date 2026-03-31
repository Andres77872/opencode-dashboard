import type {
  ApiErrorResponse,
  ConfigStats,
  DailyPeriod,
  DailyStats,
  ModelStats,
  OverviewStats,
  ProjectStats,
  SessionDetail,
  SessionList,
  ToolStats,
} from '../types/api'

const DEFAULT_API_BASE_URL = import.meta.env.VITE_API_BASE_URL?.trim() ?? ''

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
  const response = await fetch(resolveUrl(path), {
    ...init,
    headers: {
      Accept: 'application/json',
      ...init?.headers,
    },
  })

  if (!response.ok) {
    throw new ApiClientError(await parseError(response), response.status)
  }

  return (await response.json()) as T
}

export function getOverview(signal?: AbortSignal) {
  return request<OverviewStats>('/api/v1/overview', { signal })
}

export function getDaily(period: DailyPeriod, signal?: AbortSignal) {
  const params = new URLSearchParams({ period })
  return request<DailyStats>(`/api/v1/daily?${params.toString()}`, { signal })
}

export function getModels(signal?: AbortSignal) {
  return request<ModelStats>('/api/v1/models', { signal })
}

export function getTools(signal?: AbortSignal) {
  return request<ToolStats>('/api/v1/tools', { signal })
}

export function getProjects(signal?: AbortSignal) {
  return request<ProjectStats>('/api/v1/projects', { signal })
}

export function getConfig(signal?: AbortSignal) {
  return request<ConfigStats>('/api/v1/config', { signal })
}

export function getSessions(page: number, limit: number, signal?: AbortSignal) {
  const params = new URLSearchParams({
    page: String(page),
    limit: String(limit),
  })

  return request<SessionList>(`/api/v1/sessions?${params.toString()}`, { signal })
}

export function getSessionDetail(id: string, signal?: AbortSignal) {
  return request<SessionDetail>(`/api/v1/sessions/${encodeURIComponent(id)}`, { signal })
}

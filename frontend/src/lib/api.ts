import { getCsrf } from './csrf'

export type ApiError = {
  error: {
    code: string
    message: string
    requestId: string
    fields?: Record<string, string[]>
    retryable: boolean
    retryAfterSeconds?: number | null
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = { ...(init?.headers as Record<string, string> | undefined) }
  const method = (init?.method ?? 'GET').toUpperCase()
  if (method !== 'GET' && method !== 'HEAD') {
    headers['Content-Type'] = 'application/json'
    const csrf = getCsrf()
    if (csrf) headers['X-CSRF-Token'] = csrf
  }
  const res = await fetch(path, { credentials: 'same-origin', ...init, headers })
  if (!res.ok) {
    let body: ApiError | null = null
    try {
      const text = await res.text()
      body = text ? (JSON.parse(text) as ApiError) : null
    } catch {
      body = null
    }
    const err = new Error(body?.error.message ?? `Request failed (${res.status})`) as Error & {
      status: number
      body: ApiError | null
    }
    err.status = res.status
    err.body = body
    throw err
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  if (!text) return undefined as T
  return JSON.parse(text) as T
}

export const api = {
  get: <T>(path: string, headers?: Record<string, string>) => request<T>(path, { headers }),
  post: <T>(path: string, body?: unknown, headers?: Record<string, string>) =>
    request<T>(path, { method: 'POST', body: body === undefined ? undefined : JSON.stringify(body), headers }),
  patch: <T>(path: string, body: unknown, headers?: Record<string, string>) =>
    request<T>(path, { method: 'PATCH', body: JSON.stringify(body), headers }),
  del: <T>(path: string, headers?: Record<string, string>) => request<T>(path, { method: 'DELETE', headers }),
}

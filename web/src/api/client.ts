import createClient from 'openapi-fetch'
import type { components, paths } from './schema'

export type Version = components['schemas']['Version']
export type Session = components['schemas']['Session']
type ErrorBody = components['schemas']['Error']

export const api = createClient<paths>({
  // Same-origin API: the Go server serves both the app and /api, and the Vite
  // dev server proxies /api to it.
  baseUrl: window.location.origin,
  credentials: 'same-origin',
  // Resolve fetch per call so tests can stub it.
  fetch: (request) => globalThis.fetch(request),
})

/** A failed API call, with the HTTP status and the server's error code. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  /** Seconds from a Retry-After header, when present. */
  readonly retryAfter?: number

  constructor(status: number, code: string, message: string, retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.retryAfter = retryAfter
  }
}

export function isUnauthenticated(err: unknown): boolean {
  return err instanceof ApiError && err.status === 401 && err.code === 'unauthenticated'
}

/** Returns data for a successful openapi-fetch call, otherwise throws ApiError. */
export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  const { response } = result
  if (response.ok) return result.data as T
  const body = result.error as Partial<ErrorBody> | undefined
  const retry = Number(response.headers.get('Retry-After'))
  throw new ApiError(
    response.status,
    body?.error?.code ?? 'unknown',
    body?.error?.message ?? `Request failed: ${response.status}`,
    Number.isFinite(retry) && retry > 0 ? retry : undefined,
  )
}

import createClient from 'openapi-fetch'
import type { components, paths } from './schema'

export type Version = components['schemas']['Version']
export type ApiError = components['schemas']['Error']

export const api = createClient<paths>({
  // Same-origin API: the Go server serves both the app and /api, and the Vite
  // dev server proxies /api to it.
  baseUrl: window.location.origin,
  credentials: 'same-origin',
  // Resolve fetch per call so tests can stub it.
  fetch: (request) => globalThis.fetch(request),
})

/** Throws a readable Error for a failed openapi-fetch call, otherwise returns data. */
export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (result.data !== undefined) return result.data
  const err = result.error as Partial<ApiError> | undefined
  throw new Error(err?.error?.message ?? `Request failed: ${result.response.status}`)
}

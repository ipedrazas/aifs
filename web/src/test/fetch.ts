import { vi } from 'vitest'

type Reply = { status: number; body?: unknown; headers?: Record<string, string> }
type Handler = (req: Request) => Reply | Promise<Reply>

/**
 * Stubs global fetch with a route table keyed by "METHOD /path". Returns the
 * mock so tests can inspect calls. Unmatched requests fail the test loudly.
 */
export function mockApi(routes: Record<string, Reply | Handler>) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const req = input instanceof Request ? input : new Request(input, init)
    const key = `${req.method} ${new URL(req.url).pathname}`
    const route = routes[key]
    if (!route) throw new Error(`unexpected request: ${key}`)
    const reply = typeof route === 'function' ? await route(req) : route
    return new Response(reply.body === undefined ? null : JSON.stringify(reply.body), {
      status: reply.status,
      headers: { 'Content-Type': 'application/json', ...reply.headers },
    })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

export const loggedIn = { status: 200, body: { authenticated: true, expiresAt: '2030-01-01T00:00:00Z' } }
export const loggedOut = { status: 401, body: { error: { code: 'unauthenticated', message: 'log in' } } }

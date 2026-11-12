import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ReactElement } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { makeQueryClient } from '../api/queryClient'
import { ServerVersion } from './Layout'

function renderWithClient(ui: ReactElement) {
  const client = makeQueryClient()
  client.setDefaultOptions({ queries: { retry: false } })
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>)
}

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
}

describe('ServerVersion', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows the version reported by the API', async () => {
    mockFetch(200, { version: 'v1.2.3', commit: 'abc', schemaVersion: 1 })
    renderWithClient(<ServerVersion />)
    expect(await screen.findByTestId('server-version')).toHaveTextContent('Server v1.2.3 · schema v1')
  })

  it('shows a readable error when the API fails', async () => {
    mockFetch(500, { error: { code: 'internal', message: 'boom' } })
    renderWithClient(<ServerVersion />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Server unavailable: boom')
  })
})

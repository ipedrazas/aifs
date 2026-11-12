import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import type { ReactElement } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { makeQueryClient } from '../api/queryClient'
import { mockApi } from '../test/fetch'
import { ServerVersion } from './Layout'

function renderWithClient(ui: ReactElement) {
  return render(<QueryClientProvider client={makeQueryClient()}>{ui}</QueryClientProvider>)
}

describe('ServerVersion', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows the version reported by the API', async () => {
    mockApi({ 'GET /api/v1/version': { status: 200, body: { version: 'v1.2.3', commit: 'abc', schemaVersion: 1 } } })
    renderWithClient(<ServerVersion />)
    expect(await screen.findByTestId('server-version')).toHaveTextContent('Server v1.2.3 · schema v1')
  })

  it('shows a readable error when the API fails', async () => {
    mockApi({ 'GET /api/v1/version': { status: 400, body: { error: { code: 'bad', message: 'boom' } } } })
    renderWithClient(<ServerVersion />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Server unavailable: boom')
  })
})

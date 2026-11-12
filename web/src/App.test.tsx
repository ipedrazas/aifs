import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { loggedIn, loggedOut, mockApi } from './test/fetch'

const version = { status: 200, body: { version: 'v9', commit: 'c', schemaVersion: 2 } }
const noProducts = { status: 200, body: { items: [] } }

describe('authentication', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows the login screen when there is no session', async () => {
    mockApi({ 'GET /api/v1/session': loggedOut })
    render(<App />)
    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Products' })).not.toBeInTheDocument()
  })

  it('logs in with the correct password and shows the app', async () => {
    const fetch = mockApi({
      'GET /api/v1/session': loggedOut,
      'POST /api/v1/session': async (req) => {
        const { password } = await req.json()
        return password === 'right'
          ? loggedIn
          : { status: 401, body: { error: { code: 'invalid_password', message: 'incorrect password' } } }
      },
      'GET /api/v1/version': version,
      'GET /api/v1/products': noProducts,
    })
    const user = userEvent.setup()
    render(<App />)

    await user.type(await screen.findByLabelText('Password'), 'wrong')
    await user.click(screen.getByRole('button', { name: 'Log in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Incorrect password.')

    await user.type(screen.getByLabelText('Password'), 'right')
    await user.click(screen.getByRole('button', { name: 'Log in' }))
    expect(await screen.findByRole('heading', { name: 'Products' })).toBeInTheDocument()
    expect(await screen.findByTestId('server-version')).toHaveTextContent('Server v9')
    expect(fetch.mock.calls.length).toBeGreaterThanOrEqual(3)
  })

  it('explains rate limiting', async () => {
    mockApi({
      'GET /api/v1/session': loggedOut,
      'POST /api/v1/session': {
        status: 429,
        headers: { 'Retry-After': '12' },
        body: { error: { code: 'rate_limited', message: 'too many' } },
      },
    })
    const user = userEvent.setup()
    render(<App />)
    await user.type(await screen.findByLabelText('Password'), 'x')
    await user.click(screen.getByRole('button', { name: 'Log in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Try again in 12 seconds')
  })

  it('logs out', async () => {
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/products': noProducts,
      'DELETE /api/v1/session': { status: 204 },
    })
    const user = userEvent.setup()
    render(<App />)
    await user.click(await screen.findByRole('button', { name: 'Log out' }))
    expect(await screen.findByLabelText('Password')).toBeInTheDocument()
  })

  it('returns to login when the session expires mid-use', async () => {
    mockApi({ 'GET /api/v1/session': loggedIn, 'GET /api/v1/version': loggedOut, 'GET /api/v1/products': loggedOut })
    render(<App />)
    await waitFor(() => expect(screen.getByLabelText('Password')).toBeInTheDocument())
  })
})

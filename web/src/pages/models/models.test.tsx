import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import App from '../../App'
import { loggedIn, mockApi } from '../../test/fetch'
import { guessRole } from './guessRole'

const version = { status: 200, body: { version: 'v1', commit: 'c', schemaVersion: 4 } }

const model = {
  id: 3,
  name: 'Ava',
  description: 'relaxed',
  appearance: 'tall',
  preferredPoses: '',
  styling: '',
  origin: 'uploaded',
  archived: false,
  archivedAt: null,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  imageCount: 0,
  cover: null,
  images: [],
}

function visit(path: string) {
  window.history.pushState({}, '', path)
  return render(<App />)
}

beforeAll(() => {
  URL.createObjectURL = vi.fn(() => 'blob:preview')
  URL.revokeObjectURL = vi.fn()
})

describe('models', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('hides archived models unless asked', async () => {
    const fetch = mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/models': (req) =>
        new URL(req.url).searchParams.get('archived') === 'true'
          ? { status: 200, body: { items: [model, { ...model, id: 4, name: 'Old', archived: true }] } }
          : { status: 200, body: { items: [model] } },
    })
    const user = userEvent.setup()
    visit('/models')
    expect(await screen.findByText('Ava')).toBeInTheDocument()
    expect(screen.queryByText('Old')).not.toBeInTheDocument()

    await user.click(screen.getByLabelText('Show archived'))
    expect(await screen.findByText('Old')).toBeInTheDocument()
    expect(screen.getByText('Archived')).toBeInTheDocument()
    expect(fetch.mock.calls.some(([req]) => String((req as Request).url).includes('archived=true'))).toBe(true)
  })

  it('explains when deleting archives a used model', async () => {
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/models/3': { status: 200, body: model },
      'GET /api/v1/models': { status: 200, body: { items: [] } },
      'DELETE /api/v1/models/3': {
        status: 200,
        body: { ...model, archived: true, archivedAt: '2026-02-01T00:00:00Z', updatedAt: '2026-02-01T00:00:00Z' },
      },
    })
    const user = userEvent.setup()
    visit('/models/3')
    await user.click(await screen.findByRole('button', { name: 'Delete model' }))
    await user.click(screen.getByRole('button', { name: 'Delete model' })) // confirm
    expect(await screen.findByText(/archived\. It has been used/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Restore' })).toBeInTheDocument()
  })

  it('uploads reference images with a role', async () => {
    const roles: string[] = []
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/models/3': { status: 200, body: model },
      'GET /api/v1/models': { status: 200, body: { items: [model] } },
      'POST /api/v1/models/3/images': async (req) => {
        const raw = await req.text()
        roles.push(/name="role"\r\n\r\n([^\r]*)/.exec(raw)?.[1] ?? '')
        return { status: 201, body: {} }
      },
    })
    const user = userEvent.setup()
    visit('/models/3')
    await user.upload(await screen.findByLabelText(/Choose photographs/), [
      new File(['a'], 'ava-face.jpg', { type: 'image/jpeg' }),
      new File(['b'], 'ava-full-body.jpg', { type: 'image/jpeg' }),
    ])
    await user.click(screen.getByRole('button', { name: 'Upload 2 photographs' }))
    await waitFor(() => expect(roles).toEqual(['identity', 'full_body']))
  })
})

describe('guessRole', () => {
  it.each([
    ['face.jpg', 'identity'],
    ['headshot.png', 'identity'],
    ['full-body.jpg', 'full_body'],
    ['standing_outfit.webp', 'full_body'],
  ])('%s → %s', (name, role) => expect(guessRole(name)).toBe(role))
})

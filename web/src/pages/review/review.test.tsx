import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../../App'
import { loggedIn, mockApi } from '../../test/fetch'

const version = { status: 200, body: { version: 'v1', commit: 'c', schemaVersion: 6 } }

const snapshot = {
  recipe: 'reference',
  provider: 'fake',
  variations: 2,
  aspectRatio: '3:4',
  customInstructions: '',
  photoshoot: { id: 5, name: 'Jeans on Ava' },
  product: { id: 1, name: 'Jeans', sku: '', category: 'jeans', colour: '', printDetails: '' },
  model: { id: 2, name: 'Ava', appearance: '', preferredPoses: '', styling: '' },
  steps: [],
  inputs: [
    { position: 1, role: 'garment', label: 'front', hash: 'f'.repeat(64) },
    { position: 2, role: 'model', label: 'identity', hash: 'm'.repeat(64) },
  ],
}

function image(id: number, extra: Record<string, unknown> = {}) {
  return {
    id,
    generationId: 9,
    variation: id,
    hash: String(id).repeat(64),
    width: 768,
    height: 1024,
    reviewStatus: 'pending',
    rejectionReason: '',
    reviewerNotes: '',
    reviewedAt: null,
    createdAt: '2026-01-01T00:00:00Z',
    recipe: 'reference',
    provider: 'fake',
    photoshootId: 5,
    product: { id: 1, name: 'Jeans', sku: '', coverHash: '' },
    model: { id: 2, name: 'Ava', archived: false, coverHash: '' },
    ...extra,
  }
}

function detail(id: number, extra: Record<string, unknown> = {}) {
  return { ...image(id), prompt: 'p', settings: snapshot, steps: [], costMicros: 0, reviewHistory: [], ...extra }
}

const product = {
  id: 1,
  name: 'Jeans',
  sku: '',
  category: 'jeans',
  description: '',
  colour: '',
  printDetails: '',
  notes: '',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  imageCount: 2,
  cover: null,
  images: [
    { id: 1, productId: 1, hash: 'f'.repeat(64), view: 'front', filename: '', contentType: 'image/jpeg', size: 1, width: 1, height: 1, createdAt: '' },
    { id: 2, productId: 1, hash: 'p'.repeat(64), view: 'print', filename: '', contentType: 'image/jpeg', size: 1, width: 1, height: 1, createdAt: '' },
  ],
}

function visit(path: string) {
  window.history.pushState({}, '', path)
  return render(<App />)
}

function setup() {
  const reviews: { id: number; body: unknown }[] = []
  const fetch = mockApi({
    'GET /api/v1/session': loggedIn,
    'GET /api/v1/version': version,
    'GET /api/v1/images': { status: 200, body: { items: [image(1), image(2), image(3)] } },
    'GET /api/v1/images/1': { status: 200, body: detail(1) },
    'GET /api/v1/images/2': { status: 200, body: detail(2) },
    'GET /api/v1/images/3': { status: 200, body: detail(3) },
    'GET /api/v1/products/1': { status: 200, body: product },
    'GET /api/v1/products': { status: 200, body: { items: [product] } },
    'GET /api/v1/photoshoots': { status: 200, body: { items: [] } },
    'GET /api/v1/photoshoots/5': { status: 200, body: {} },
    'POST /api/v1/images/1/review': async (req) => {
      const body = await req.json()
      reviews.push({ id: 1, body })
      return { status: 200, body: detail(1, { reviewStatus: body.status }) }
    },
    'POST /api/v1/images/2/review': async (req) => {
      const body = await req.json()
      reviews.push({ id: 2, body })
      return { status: 200, body: detail(2, { reviewStatus: body.status }) }
    },
    'POST /api/v1/images/3/review': async (req) => {
      const body = await req.json()
      reviews.push({ id: 3, body })
      return { status: 200, body: detail(3, { reviewStatus: body.status }) }
    },
  })
  return { reviews, fetch }
}

describe('review gallery', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('defaults to pending images and filters through the URL', async () => {
    const { fetch } = setup()
    const user = userEvent.setup()
    visit('/review')
    expect(await screen.findAllByTestId('gallery-image')).toHaveLength(3)
    const urls = () => fetch.mock.calls.map(([r]) => String((r as Request).url)).filter((u) => u.includes('/api/v1/images?'))
    expect(urls().some((u) => u.includes('status=pending'))).toBe(true)

    await user.selectOptions(screen.getByLabelText('Status'), 'rejected')
    await waitFor(() => expect(window.location.search).toContain('status=rejected'))
    await user.selectOptions(screen.getByLabelText('Product'), '1')
    await waitFor(() => expect(window.location.search).toContain('productId=1'))
    await waitFor(() => expect(urls().some((u) => u.includes('productId=1') && u.includes('status=rejected'))).toBe(true))
  })
})

describe('review screen', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('reviews a batch with the keyboard only', async () => {
    const { reviews } = setup()
    const user = userEvent.setup()
    visit('/review/1?status=pending')
    expect(await screen.findByTestId('queue-position')).toHaveTextContent('1 of 3')

    // Notes, then approve: moves on to the next image.
    await user.keyboard('n')
    await user.keyboard('looks right')
    await user.keyboard('{Escape}')
    await user.keyboard('a')
    await waitFor(() => expect(screen.getByTestId('queue-position')).toHaveTextContent('2 of 3'))
    expect(reviews[0]).toEqual({ id: 1, body: { status: 'approved', notes: 'looks right' } })

    // Reject with a reason chosen by number.
    await user.keyboard('r')
    expect(screen.getByRole('group', { name: 'Rejection reason' })).toBeInTheDocument()
    await user.keyboard('1')
    await waitFor(() => expect(screen.getByTestId('queue-position')).toHaveTextContent('3 of 3'))
    expect(reviews[1]).toEqual({ id: 2, body: { status: 'rejected', reason: 'fidelity', notes: '' } })

    // Needs refinement on the last image; it stays on screen.
    await user.keyboard('f')
    await waitFor(() => expect(reviews).toHaveLength(3))
    expect(reviews[2]).toEqual({ id: 3, body: { status: 'needs_refinement', notes: '' } })
    expect(await screen.findByText('End of the queue.')).toBeInTheDocument()

    // Navigate back.
    await user.keyboard('k')
    await waitFor(() => expect(screen.getByTestId('queue-position')).toHaveTextContent('2 of 3'))
  })

  it('switches references, modes and zoom from the keyboard', async () => {
    setup()
    const user = userEvent.setup()
    visit('/review/1?status=pending')
    const refs = await screen.findByRole('list', { name: 'Reference photographs' })
    await waitFor(() => expect(within(refs).getAllByRole('button')).toHaveLength(3)) // front (sent), print, model
    expect(within(refs).getAllByRole('button')[0]).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByAltText('Reference: Front (sent)')).toBeInTheDocument()

    await user.keyboard(']')
    expect(within(refs).getAllByRole('button')[1]).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByAltText('Reference: Print close-up')).toBeInTheDocument()
    await user.keyboard('[[[[') // '[[' is one literal '[' in user-event syntax
    expect(within(refs).getAllByRole('button')[2]).toHaveAttribute('aria-pressed', 'true')

    const modes = screen.getByRole('group', { name: 'Comparison mode' })
    await user.keyboard('m')
    expect(within(modes).getByRole('button', { name: 'Swipe' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByTestId('pane-swipe')).toBeInTheDocument()
    await user.keyboard('m')
    expect(screen.getByTestId('pane-overlay')).toBeInTheDocument()
    await user.keyboard('m')
    expect(screen.getByTestId('pane-generated')).toBeInTheDocument()
    expect(screen.getByTestId('pane-reference')).toBeInTheDocument()

    await user.keyboard('+')
    expect(screen.getByText('150%')).toBeInTheDocument()
    await user.keyboard('0')
    expect(screen.getByText('100%')).toBeInTheDocument()

    await user.keyboard('?')
    expect(screen.getByLabelText('Keyboard shortcuts')).toBeInTheDocument()
  })

  it('shows the decision history', async () => {
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/images': { status: 200, body: { items: [] } },
      'GET /api/v1/images/1': {
        status: 200,
        body: detail(1, {
          reviewStatus: 'approved',
          reviewHistory: [
            { id: 1, status: 'rejected', rejectionReason: 'fidelity', notes: 'wrong stitching', createdAt: '2026-01-02T10:00:00Z' },
            { id: 2, status: 'approved', rejectionReason: '', notes: '', createdAt: '2026-01-03T10:00:00Z' },
          ],
        }),
      },
      'GET /api/v1/products/1': { status: 200, body: product },
    })
    visit('/review/1')
    const history = await screen.findByRole('list', { name: 'Review history' })
    const items = within(history).getAllByRole('listitem')
    expect(items).toHaveLength(2)
    expect(items[0]).toHaveTextContent('approved')
    expect(items[1]).toHaveTextContent('Product fidelity')
    expect(items[1]).toHaveTextContent('wrong stitching')
  })
})

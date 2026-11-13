import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../../App'
import { loggedIn, mockApi } from '../../test/fetch'

const version = { status: 200, body: { version: 'v1', commit: 'c', schemaVersion: 5 } }

const providers = {
  status: 200,
  body: {
    maxVariations: 4,
    providers: [
      {
        name: 'fake',
        label: 'Fake',
        available: true,
        reason: '',
        capabilities: ['reference_generate', 'try_on'],
        models: {},
        unitPriceMicros: 10_000,
        recipes: ['reference', 'tryon-photo', 'tryon'],
      },
      {
        name: 'fashn',
        label: 'FASHN virtual try-on',
        available: false,
        reason: 'set AIFS_FASHN_API_KEY to enable it',
        capabilities: [],
        models: {},
        unitPriceMicros: 0,
        recipes: [],
      },
    ],
    recipes: [
      { name: 'reference', label: 'Reference-guided', description: 'one call', steps: [{ capability: 'reference_generate', purpose: 'photograph' }] },
      { name: 'tryon-photo', label: 'Try-on on photo', description: 'one call', steps: [{ capability: 'try_on', purpose: 'try-on' }] },
      { name: 'tryon', label: 'Try-on', description: 'two calls', steps: [{ capability: 'reference_generate', purpose: 'scene' }, { capability: 'try_on', purpose: 'try-on' }] },
    ],
  },
}

const snapshot = {
  recipe: 'reference',
  provider: 'fake',
  variations: 2,
  aspectRatio: '3:4',
  customInstructions: '',
  photoshoot: { id: 5, name: 'Jeans on Ava' },
  product: { id: 1, name: 'Jeans', sku: 'J-1', category: 'jeans', colour: '', printDetails: '' },
  model: { id: 2, name: 'Ava', appearance: '', preferredPoses: '', styling: '' },
  steps: [{ position: 1, capability: 'reference_generate', purpose: 'photograph', provider: 'fake', providerModel: 'fake-v1', prompt: 'p', inputs: [1, 2], usesPrevious: false, source: 0 }],
  inputs: [
    { position: 1, role: 'garment', label: 'front', hash: 'a'.repeat(64) },
    { position: 2, role: 'model', label: 'identity', hash: 'b'.repeat(64) },
  ],
}

function generation(status: string, images: number) {
  return {
    id: 9,
    photoshootId: 5,
    productId: 1,
    modelId: 2,
    recipe: 'reference',
    provider: 'fake',
    variations: 2,
    status,
    error: '',
    costMicros: images * 10_000,
    cancelRequested: false,
    prompt: 'p',
    createdAt: '2026-01-01T00:00:00Z',
    startedAt: '2026-01-01T00:00:01Z',
    finishedAt: status === 'succeeded' ? '2026-01-01T00:00:09Z' : null,
    stepsTotal: 2,
    stepsDone: images,
    settings: snapshot,
    images: Array.from({ length: images }, (_, i) => ({
      id: 100 + i,
      generationId: 9,
      variation: i + 1,
      hash: String(i).repeat(64),
      width: 768,
      height: 1024,
      reviewStatus: 'pending',
      createdAt: '2026-01-01T00:00:05Z',
      recipe: 'reference',
      provider: 'fake',
      photoshootId: 5,
      product: { id: 1, name: 'Jeans', sku: 'J-1', coverHash: '' },
      model: { id: 2, name: 'Ava', archived: false, coverHash: '' },
    })),
  }
}

function photoshoot(generations: unknown[]) {
  return {
    id: 5,
    name: 'Jeans on Ava',
    productId: 1,
    modelId: 2,
    aspectRatio: '3:4',
    customInstructions: '',
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    product: { id: 1, name: 'Jeans', sku: 'J-1', coverHash: 'a'.repeat(64) },
    model: { id: 2, name: 'Ava', archived: false, coverHash: 'b'.repeat(64) },
    generationCount: generations.length,
    latestImageHash: '',
    generations,
  }
}

const productBody = {
  id: 1,
  name: 'Jeans',
  sku: 'J-1',
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
    { id: 11, productId: 1, hash: 'a'.repeat(64), view: 'front', filename: 'f.jpg', contentType: 'image/jpeg', size: 1, width: 1, height: 1, createdAt: '' },
    { id: 12, productId: 1, hash: 'c'.repeat(64), view: 'back', filename: 'b.jpg', contentType: 'image/jpeg', size: 1, width: 1, height: 1, createdAt: '' },
  ],
}
const productMock = { status: 200, body: productBody }

function visit(path: string) {
  window.history.pushState({}, '', path)
  return render(<App />)
}

describe('photoshoot generation', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('queues a generation and shows images as they arrive', async () => {
    let sent: unknown
    const states = [photoshoot([]), photoshoot([generation('running', 1)]), photoshoot([generation('succeeded', 2)])]
    let calls = 0
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/providers': providers,
      'GET /api/v1/products/1': productMock,
      'GET /api/v1/photoshoots/5': () => ({ status: 200, body: states[Math.min(calls++, states.length - 1)] }),
      'GET /api/v1/photoshoots': { status: 200, body: { items: [] } },
      'GET /api/v1/images': { status: 200, body: { items: [] } },
      'POST /api/v1/photoshoots/5/generations': async (req) => {
        sent = await req.json()
        return { status: 202, body: generation('queued', 0) }
      },
    })
    const user = userEvent.setup()
    visit('/photoshoots/5')

    expect(await screen.findByText('Nothing generated yet.')).toBeInTheDocument()
    expect(await screen.findByText(/Estimated cost: \$0\.02/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Generate' }))
    await waitFor(() => expect(sent).toEqual({ recipe: 'reference', provider: 'fake', variations: 2 }))

    // Running: one image and one placeholder.
    const card = await screen.findByTestId('generation')
    await waitFor(() => expect(card).toHaveAttribute('data-status', 'running'))
    expect(within(card).getAllByTestId('generated-image')).toHaveLength(1)
    expect(within(card).getByText('Generating…')).toBeInTheDocument()

    // Polling picks up completion.
    await waitFor(() => expect(screen.getByTestId('generation')).toHaveAttribute('data-status', 'succeeded'), { timeout: 4000 })
    expect(within(screen.getByTestId('generation')).getAllByTestId('generated-image')).toHaveLength(2)
  })

  it('cancels a running generation', async () => {
    let cancelled = false
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/providers': providers,
      'GET /api/v1/products/1': productMock,
      'GET /api/v1/photoshoots/5': () => ({
        status: 200,
        body: photoshoot([cancelled ? generation('cancelled', 0) : generation('running', 0)]),
      }),
      'POST /api/v1/generations/9/cancel': () => {
        cancelled = true
        return { status: 200, body: generation('cancelled', 0) }
      },
    })
    const user = userEvent.setup()
    visit('/photoshoots/5')
    await user.click(await screen.findByRole('button', { name: 'Cancel' }))
    await user.click(screen.getByRole('button', { name: 'Cancel generation' }))
    await waitFor(() => expect(screen.getByTestId('generation')).toHaveAttribute('data-status', 'cancelled'))
  })
})

describe('try-on options', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('offers the garment photograph and shows unavailable providers as disabled', async () => {
    let sent: unknown
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/providers': providers,
      'GET /api/v1/products/1': productMock,
      'GET /api/v1/photoshoots/5': { status: 200, body: photoshoot([]) },
      'GET /api/v1/photoshoots': { status: 200, body: { items: [] } },
      'GET /api/v1/images': { status: 200, body: { items: [] } },
      'POST /api/v1/photoshoots/5/generations': async (req) => {
        sent = await req.json()
        return { status: 202, body: generation('queued', 0) }
      },
    })
    const user = userEvent.setup()
    visit('/photoshoots/5')

    const fashn = await screen.findByRole('option', { name: /FASHN virtual try-on — set AIFS_FASHN_API_KEY/ })
    expect(fashn).toBeDisabled()
    expect(screen.queryByLabelText('Garment photograph')).not.toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('Recipe'), 'tryon-photo')
    await user.selectOptions(await screen.findByLabelText('Garment photograph'), '12')
    await user.click(screen.getByRole('button', { name: 'Generate' }))
    await waitFor(() => expect(sent).toEqual({ recipe: 'tryon-photo', provider: 'fake', variations: 2, garmentImageId: 12 }))
  })
})

describe('image provenance', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows what produced the image', async () => {
    const im = generation('succeeded', 1).images[0]
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/images/100': {
        status: 200,
        body: { ...im, prompt: 'Reproduce the garment exactly', settings: snapshot, steps: [], costMicros: 10_000 },
      },
    })
    visit('/images/100')
    const prov = await screen.findByLabelText('Provenance')
    expect(within(prov).getByRole('link', { name: 'Jeans' })).toHaveAttribute('href', '/products/1')
    expect(within(prov).getByRole('link', { name: 'Ava' })).toHaveAttribute('href', '/models/2')
    expect(within(prov).getByText('photograph: fake-v1')).toBeInTheDocument()
    expect(within(prov).getByText('$0.01')).toBeInTheDocument()
    expect(screen.getByAltText('garment: front')).toBeInTheDocument()
    expect(screen.getByAltText('model: identity')).toBeInTheDocument()
  })
})

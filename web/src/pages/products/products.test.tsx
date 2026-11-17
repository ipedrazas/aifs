import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import App from '../../App'
import { loggedIn, mockApi } from '../../test/fetch'
import { guessView } from './guessView'

const version = { status: 200, body: { version: 'v1', commit: 'c', schemaVersion: 3 } }

const image = {
  id: 7,
  productId: 1,
  hash: 'a'.repeat(64),
  view: 'front',
  filename: 'front.jpg',
  contentType: 'image/jpeg',
  size: 100,
  width: 800,
  height: 1200,
  createdAt: '2026-01-01T00:00:00Z',
  sourceImageId: null,
  processing: null,
  processedImageId: null,
}

const product = {
  id: 1,
  name: 'Slim jeans',
  sku: 'SJ-1',
  category: 'jeans',
  description: '',
  colour: 'indigo',
  printDetails: '',
  notes: '',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
  imageCount: 1,
  cover: image,
}

function visit(path: string) {
  window.history.pushState({}, '', path)
  return render(<App />)
}

beforeAll(() => {
  // jsdom has no object URLs.
  URL.createObjectURL = vi.fn(() => 'blob:preview')
  URL.revokeObjectURL = vi.fn()
})

describe('products', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('lists products with their cover thumbnail', async () => {
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/products': { status: 200, body: { items: [product, { ...product, id: 2, name: 'Empty', sku: '', imageCount: 0, cover: null }] } },
    })
    visit('/products')
    const card = (await screen.findByText('Slim jeans')).closest('a')!
    expect(card).toHaveAttribute('href', '/products/1')
    expect(within(card).getByRole('presentation')).toHaveAttribute('src', `/api/v1/blobs/${image.hash}?w=512`)
    expect(screen.getByText('No photographs')).toBeInTheDocument()
    expect(screen.getByText('No identifier · 0 photos')).toBeInTheDocument()
  })

  it('shows field errors from the server when creating', async () => {
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'POST /api/v1/products': {
        status: 409,
        body: { error: { code: 'conflict', message: 'another product already uses this identifier' } },
      },
    })
    const user = userEvent.setup()
    visit('/products/new')
    await user.type(await screen.findByLabelText('Name'), 'Jeans')
    await user.type(screen.getByLabelText('Identifier (SKU)'), 'SJ-1')
    await user.click(screen.getByRole('button', { name: 'Create product' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('already uses this identifier')
  })

  it('creates a product and opens it', async () => {
    let sent: unknown
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'POST /api/v1/products': async (req) => {
        sent = await req.json()
        return { status: 201, body: { ...product, images: [] } }
      },
      'GET /api/v1/products/1': { status: 200, body: { ...product, images: [] } },
      'GET /api/v1/products': { status: 200, body: { items: [product] } },
    })
    const user = userEvent.setup()
    visit('/products/new')
    await user.type(await screen.findByLabelText('Name'), 'Slim jeans')
    await user.type(screen.getByLabelText('Colour'), 'indigo')
    await user.selectOptions(screen.getByLabelText('Print complexity'), 'complex')
    await user.click(screen.getByLabelText('In the fidelity test set'))
    await user.click(screen.getByRole('button', { name: 'Create product' }))
    expect(await screen.findByRole('heading', { name: 'Slim jeans' })).toBeInTheDocument()
    expect(sent).toMatchObject({ name: 'Slim jeans', colour: 'indigo', category: 'jeans', printComplexity: 'complex', testSet: true })
  })

  it('uploads photographs as multipart with the chosen view', async () => {
    const uploads: { view: string; hasFile: boolean }[] = []
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/products/1': { status: 200, body: { ...product, images: [image] } },
      'GET /api/v1/products': { status: 200, body: { items: [product] } },
      'POST /api/v1/products/1/images': async (req) => {
        // jsdom cannot parse multipart bodies back (and drops file names), so
        // read the raw parts and go by upload order.
        const raw = await req.text()
        const view = /name="view"\r\n\r\n([^\r]*)/.exec(raw)?.[1] ?? ''
        const hasFile = /name="file"; filename="/.test(raw)
        uploads.push({ view, hasFile })
        if (uploads.length === 2) {
          return { status: 422, body: { error: { code: 'invalid_image', message: 'the image is damaged or incomplete' } } }
        }
        return { status: 201, body: { ...image, id: 8, view } }
      },
    })
    const user = userEvent.setup()
    visit('/products/1')

    await screen.findByRole('heading', { name: 'Slim jeans' })
    expect(screen.getAllByTestId('product-image')).toHaveLength(1)

    const input = screen.getByLabelText(/Choose photographs/)
    await user.upload(input, [
      new File(['a'], 'jeans-back.jpg', { type: 'image/jpeg' }),
      new File(['b'], 'broken.jpg', { type: 'image/jpeg' }),
    ])
    await user.selectOptions(screen.getByLabelText('View for broken.jpg'), 'detail')
    await user.click(screen.getByRole('button', { name: 'Upload 2 photographs' }))

    await waitFor(() => expect(uploads).toHaveLength(2))
    expect(uploads).toEqual([
      { view: 'back', hasFile: true },
      { view: 'detail', hasFile: true },
    ])
    // The successful one leaves the queue; the failed one stays with its reason.
    expect(await screen.findByText('the image is damaged or incomplete')).toBeInTheDocument()
    expect(screen.queryByText('jeans-back.jpg')).not.toBeInTheDocument()
  })
})

const replicate = {
  name: 'replicate',
  label: 'Replicate background removal',
  available: true,
  reason: '',
  capabilities: ['remove_background'],
  models: {},
  unitPriceMicros: 500,
  recipes: [],
}

function providersReply(...providers: object[]) {
  return { status: 200, body: { providers, recipes: [], maxVariations: 4 } }
}

const processed = {
  ...image,
  id: 9,
  hash: 'b'.repeat(64),
  filename: 'front-processed.png',
  contentType: 'image/png',
  width: 700,
  height: 1100,
  sourceImageId: 7,
  processing: { steps: ['remove_background', 'trim', 'resize'], provider: 'replicate', maxEdge: 3072, costMicros: 500, durationMs: 2100 },
}

describe('photograph processing', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('processes a photograph and uses the processed version', async () => {
    let images: object[] = [image]
    let sent: unknown
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/products/1': () => ({ status: 200, body: { ...product, images } }),
      'GET /api/v1/products': { status: 200, body: { items: [product] } },
      'GET /api/v1/providers': providersReply(replicate),
      'POST /api/v1/products/1/images/7/process': async (req) => {
        sent = await req.json()
        images = [{ ...image, processedImageId: 9 }, processed]
        return { status: 201, body: processed }
      },
    })
    const user = userEvent.setup()
    visit('/products/1')

    await user.click(await screen.findByRole('button', { name: 'Process…' }))
    expect(await screen.findByText(/Replicate background removal · about \$0\.0005 per photograph/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Process' }))
    await waitFor(() => expect(sent).toEqual({ removeBackground: true, trim: true, maxEdge: 3072 }))

    expect(await screen.findByText('Processed · used for generation')).toBeInTheDocument()
    expect(screen.getByText('Background removed, trimmed, resized to 3072 px · $0.0005')).toBeInTheDocument()
    expect(screen.getAllByTestId('product-image')).toHaveLength(1)
    expect(screen.getByRole('img')).toHaveAttribute('src', `/api/v1/blobs/${processed.hash}?w=512`)

    await user.click(screen.getByRole('button', { name: 'Show original' }))
    expect(screen.getByRole('img')).toHaveAttribute('src', `/api/v1/blobs/${image.hash}?w=512`)
    expect(screen.getByText('Original (not used)')).toBeInTheDocument()
  })

  it('explains when background removal is unavailable and shows field errors', async () => {
    let sent: unknown
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/products/1': { status: 200, body: { ...product, images: [image] } },
      'GET /api/v1/products': { status: 200, body: { items: [product] } },
      'GET /api/v1/providers': providersReply({
        ...replicate,
        available: false,
        reason: 'set AIFS_REPLICATE_API_TOKEN to enable it',
      }),
      'POST /api/v1/products/1/images/7/process': async (req) => {
        sent = await req.json()
        return {
          status: 422,
          body: {
            error: {
              code: 'invalid',
              message: 'some fields are invalid',
              fields: { trim: 'trimming needs a transparent background: remove the background as well' },
            },
          },
        }
      },
    })
    const user = userEvent.setup()
    visit('/products/1')

    await user.click(await screen.findByRole('button', { name: 'Process…' }))
    expect(await screen.findByText('Unavailable: set AIFS_REPLICATE_API_TOKEN to enable it')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: /Remove the background/ })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Process' }))
    await waitFor(() => expect(sent).toEqual({ removeBackground: false, trim: true, maxEdge: 3072 }))
    expect(await screen.findByRole('alert')).toHaveTextContent('trimming needs a transparent background')
  })

  it('processes every unprocessed photograph', async () => {
    const back = { ...image, id: 8, hash: 'c'.repeat(64), view: 'back', filename: 'back.jpg' }
    const calls: string[] = []
    mockApi({
      'GET /api/v1/session': loggedIn,
      'GET /api/v1/version': version,
      'GET /api/v1/products/1': { status: 200, body: { ...product, images: [image, back] } },
      'GET /api/v1/products': { status: 200, body: { items: [product] } },
      'GET /api/v1/providers': providersReply(replicate),
      'POST /api/v1/products/1/images/7/process': () => {
        calls.push('front')
        return { status: 201, body: processed }
      },
      'POST /api/v1/products/1/images/8/process': () => {
        calls.push('back')
        return { status: 502, body: { error: { code: 'provider_failed', message: 'rate limit reached on Replicate' } } }
      },
    })
    const user = userEvent.setup()
    visit('/products/1')

    expect(await screen.findByText('2 photographs not processed yet.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Process all…' }))
    await user.click(await screen.findByRole('button', { name: 'Process 2' }))
    expect(await screen.findByText('Processed 1 of 2.')).toBeInTheDocument()
    expect(calls).toEqual(['front', 'back'])
    expect(screen.getByRole('alert')).toHaveTextContent('Back back.jpg: rate limit reached on Replicate')
  })
})

describe('guessView', () => {
  it.each([
    ['IMG_001.jpg', 'front'],
    ['jeans_BACK.jpg', 'back'],
    ['side-profile.png', 'side'],
    ['logo-print.webp', 'print'],
    ['closeup.jpg', 'detail'],
  ])('%s → %s', (name, view) => expect(guessView(name)).toBe(view))
})

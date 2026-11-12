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
    await user.click(screen.getByRole('button', { name: 'Create product' }))
    expect(await screen.findByRole('heading', { name: 'Slim jeans' })).toBeInTheDocument()
    expect(sent).toMatchObject({ name: 'Slim jeans', colour: 'indigo', category: 'jeans' })
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

describe('guessView', () => {
  it.each([
    ['IMG_001.jpg', 'front'],
    ['jeans_BACK.jpg', 'back'],
    ['side-profile.png', 'side'],
    ['logo-print.webp', 'print'],
    ['closeup.jpg', 'detail'],
  ])('%s → %s', (name, view) => expect(guessView(name)).toBe(view))
})

import { expect, test } from '@playwright/test'
import { fixture, fixturePath } from './helpers.ts'

test('create a product, upload labelled photographs, download them unchanged', async ({ page }) => {
  await page.goto('/products')
  await page.getByRole('link', { name: 'New product' }).click()

  const sku = `E2E-${Date.now()}`
  await page.getByLabel('Name').fill('E2E straight jeans')
  await page.getByLabel('Identifier (SKU)').fill(sku)
  await page.getByLabel('Colour').fill('mid-blue')
  await page.getByLabel('Print details').fill('embroidered back pocket')
  await page.getByRole('button', { name: 'Create product' }).click()
  await expect(page.getByRole('heading', { name: 'E2E straight jeans' })).toBeVisible()

  await page.getByLabel(/Choose photographs/).setInputFiles([fixturePath('rot6.jpg'), fixturePath('plain.png')])
  await page.getByLabel('View for plain.png').selectOption('back')
  await page.getByRole('button', { name: 'Upload 2 photographs' }).click()
  await expect(page.getByTestId('product-image')).toHaveCount(2)
  await expect(page.getByRole('heading', { name: 'Front' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Back' })).toBeVisible()

  // Thumbnails render (decoded by the browser).
  const thumbs = page.getByTestId('product-image').locator('img')
  for (const img of await thumbs.all()) {
    await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0)
  }

  // The originals come back byte for byte.
  const id = Number(page.url().split('/').pop())
  const product = await (await page.request.get(`/api/v1/products/${id}`)).json()
  const byView = Object.fromEntries(product.images.map((im: { view: string; hash: string }) => [im.view, im.hash]))
  for (const [view, file] of [['front', 'rot6.jpg'], ['back', 'plain.png']]) {
    const res = await page.request.get(`/api/v1/blobs/${byView[view]}`)
    expect(res.status()).toBe(200)
    expect(Buffer.compare(await res.body(), fixture(file)), `${view} bytes`).toBe(0)
  }
})

test('non-images and damaged images are rejected with a clear message', async ({ page }) => {
  await page.goto('/products/new')
  await page.getByLabel('Name').fill('E2E rejects')
  await page.getByRole('button', { name: 'Create product' }).click()
  await expect(page.getByRole('heading', { name: 'E2E rejects' })).toBeVisible()

  await page.getByLabel(/Choose photographs/).setInputFiles([fixturePath('text.jpg'), fixturePath('truncated.jpg')])
  await page.getByRole('button', { name: 'Upload 2 photographs' }).click()
  await expect(page.getByText(/only JPEG, PNG and WebP/i).first()).toBeVisible()
  await expect(page.getByText(/damaged or incomplete/)).toBeVisible()
  await expect(page.getByTestId('product-image')).toHaveCount(0)
})

test('delete a photograph and then the product', async ({ page }) => {
  await page.goto('/products/new')
  await page.getByLabel('Name').fill('E2E delete me')
  await page.getByRole('button', { name: 'Create product' }).click()
  await expect(page.getByRole('heading', { name: 'E2E delete me' })).toBeVisible()

  await page.getByLabel(/Choose photographs/).setInputFiles(fixturePath('plain.webp'))
  await page.getByRole('button', { name: 'Upload 1 photograph' }).click()
  await expect(page.getByTestId('product-image')).toHaveCount(1)

  const card = page.getByTestId('product-image')
  await card.getByRole('button', { name: 'Delete' }).click()
  await card.getByRole('button', { name: 'Delete' }).click() // confirm
  await expect(page.getByTestId('product-image')).toHaveCount(0)

  await page.getByRole('button', { name: 'Delete product' }).click()
  await page.getByRole('button', { name: 'Delete product and photographs' }).click()
  await expect(page).toHaveURL(/\/products$/)
  await expect(page.getByText('E2E delete me')).toHaveCount(0)
})

import { expect, test } from '@playwright/test'
import { fixture, fixturePath } from './helpers.ts'

test('process a product photograph, generate with it, then revert to the original', async ({ page }) => {
  await page.goto('/products/new')
  await page.getByLabel('Name').fill('E2E processing')
  await page.getByRole('button', { name: 'Create product' }).click()
  await expect(page.getByRole('heading', { name: 'E2E processing' })).toBeVisible()
  const productId = Number(page.url().split('/').pop())

  // plain.png is two flat colours; the fake provider treats the corner colour as background.
  await page.getByLabel(/Choose photographs/).setInputFiles([fixturePath('plain.png')])
  await page.getByRole('button', { name: 'Upload 1 photograph' }).click()
  const card = page.getByTestId('product-image')
  await expect(card).toHaveCount(1)
  await expect(card.getByText('Original', { exact: true })).toBeVisible()

  await card.getByRole('button', { name: 'Process…' }).click()
  await expect(card.getByText(/Fake \(offline test provider\) · about \$0\.01 per photograph/)).toBeVisible()
  await card.getByRole('button', { name: 'Process', exact: true }).click()
  await expect(card.getByText('Processed · used for generation')).toBeVisible()
  await expect(card.getByText('Background removed, trimmed · $0.01')).toBeVisible()

  // The original is kept byte for byte; the processed version is a lossless cut-out.
  const product = await (await page.request.get(`/api/v1/products/${productId}`)).json()
  expect(product.imageCount).toBe(1)
  const [original, processed] = product.images
  expect(original.processedImageId).toBe(processed.id)
  expect(processed.sourceImageId).toBe(original.id)
  expect(processed.contentType).toBe('image/png')
  const res = await page.request.get(`/api/v1/blobs/${original.hash}`)
  expect(Buffer.compare(await res.body(), fixture('plain.png'))).toBe(0)

  // A generation sends the processed version.
  const model = await (await page.request.post('/api/v1/models', { data: { name: 'E2E processing model' } })).json()
  await page.request.post(`/api/v1/models/${model.id}/images`, {
    multipart: { role: 'identity', file: { name: 'face.jpg', mimeType: 'image/jpeg', buffer: fixture('rot6.jpg') } },
  })
  const shoot = await (await page.request.post('/api/v1/photoshoots', { data: { productId, modelId: model.id } })).json()
  const gen = await (
    await page.request.post(`/api/v1/photoshoots/${shoot.id}/generations`, { data: { recipe: 'reference', provider: 'fake', variations: 1 } })
  ).json()
  expect(gen.settings.inputs[0]).toMatchObject({ role: 'garment', hash: processed.hash, processed: true })

  // Reverting deletes the processed version; the original is used again.
  await card.getByRole('button', { name: 'Revert to original' }).click()
  await card.getByRole('button', { name: 'Revert', exact: true }).click()
  await expect(card.getByText('Original', { exact: true })).toBeVisible()
  await expect(page.getByText('1 photograph not processed yet.')).toBeVisible()
})

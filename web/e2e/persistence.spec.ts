import { expect, test } from '@playwright/test'
import { fixture } from './helpers.ts'

// Run in two passes by `task e2e`: @seed before a container restart and
// @after-restart after it.
const name = 'E2E persisted jeans'

test('seed a product with a photograph @seed', async ({ page }) => {
  const created = await page.request.post('/api/v1/products', { data: { name, sku: 'E2E-PERSIST' } })
  expect(created.status()).toBe(201)
  const { id } = await created.json()
  const up = await page.request.post(`/api/v1/products/${id}/images`, {
    multipart: { view: 'front', file: { name: 'front.jpg', mimeType: 'image/jpeg', buffer: fixture('rot6.jpg') } },
  })
  expect(up.status()).toBe(201)
})

test('the product and its photograph survive a restart @after-restart', async ({ page }) => {
  await page.goto('/products')
  await page.getByText(name).click()
  await expect(page.getByTestId('product-image')).toHaveCount(1)

  const id = Number(page.url().split('/').pop())
  const product = await (await page.request.get(`/api/v1/products/${id}`)).json()
  const res = await page.request.get(`/api/v1/blobs/${product.images[0].hash}`)
  expect(Buffer.compare(await res.body(), fixture('rot6.jpg'))).toBe(0)
})

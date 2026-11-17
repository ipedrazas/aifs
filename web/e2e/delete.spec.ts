import { expect, test } from '@playwright/test'
import { seedPhotoshoot } from './studio.ts'

test('a photoshoot with reviewed generations can be deleted, and then its product', async ({ page }) => {
  const ps = await seedPhotoshoot(page.request, 'E2E-DELETE')
  let gen = await (
    await page.request.post(`/api/v1/photoshoots/${ps.id}/generations`, { data: { recipe: 'reference', provider: 'fake', variations: 1 } })
  ).json()
  await expect
    .poll(async () => (gen = await (await page.request.get(`/api/v1/generations/${gen.id}`)).json()).status, { timeout: 20_000 })
    .toBe('succeeded')
  const image = gen.images[0]
  expect(
    (await page.request.post(`/api/v1/images/${image.id}/review`, { data: { status: 'rejected', reason: 'fidelity', notes: 'wrong pocket' } })).status(),
  ).toBe(200)

  // The product is still used, so it cannot be deleted yet.
  await page.goto(`/products/${ps.productId}`)
  await page.getByRole('button', { name: 'Delete product' }).click()
  await page.getByRole('button', { name: 'Delete product and photographs' }).click()
  await expect(page.getByRole('alert')).toContainText('kept for traceability')

  await page.goto(`/photoshoots/${ps.id}`)
  await expect(page.getByText(/also deletes its 1 generation, their images and reviews/)).toBeVisible()
  await page.getByRole('button', { name: 'Delete photoshoot', exact: true }).click()
  await page.getByRole('button', { name: 'Delete photoshoot and 1 generation' }).click()
  await expect(page).toHaveURL(/\/photoshoots$/)
  expect((await page.request.get(`/api/v1/images/${image.id}`)).status()).toBe(404)
  expect((await page.request.get(`/api/v1/blobs/${image.hash}`)).status()).toBe(404)

  await page.goto(`/products/${ps.productId}`)
  await page.getByRole('button', { name: 'Delete product' }).click()
  await page.getByRole('button', { name: 'Delete product and photographs' }).click()
  await expect(page).toHaveURL(/\/products$/)
})

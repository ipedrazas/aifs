import { expect, test } from '@playwright/test'
import { seedPhotoshoot, seedSubjects } from './studio.ts'

test('create a photoshoot, generate, see images arrive and their provenance', async ({ page }) => {
  const { product, model } = await seedSubjects(page.request, 'E2E-GEN')

  await page.goto('/photoshoots/new')
  await page.getByLabel('Product').selectOption(String(product.id))
  await page.getByLabel('Model').selectOption(String(model.id))
  await page.getByLabel('Custom instructions').first().fill('hands in pockets')
  await page.getByRole('button', { name: 'Create photoshoot' }).click()
  await expect(page.getByRole('heading', { name: `${product.name} on ${model.name}` })).toBeVisible()

  await page.getByLabel('Variations').fill('2')
  await page.getByRole('button', { name: 'Generate' }).click()

  // Returns immediately; the card shows progress, then the images.
  const card = page.getByTestId('generation').first()
  await expect(card).toBeVisible()
  await expect(card).toHaveAttribute('data-status', 'succeeded', { timeout: 20_000 })
  await expect(card.getByTestId('generated-image')).toHaveCount(2)
  await expect(card.getByText('$0.02')).toBeVisible()

  await card.getByTestId('generated-image').first().click()
  const prov = page.getByLabel('Provenance')
  await expect(prov.getByRole('link', { name: product.name })).toBeVisible()
  await expect(prov.getByRole('link', { name: model.name })).toBeVisible()
  await expect(prov.getByText('reference', { exact: true })).toBeVisible()
  await expect(prov.getByText('hands in pockets')).toBeVisible()
  await expect(page.getByAltText('garment: front')).toBeVisible()
  const img = page.getByAltText(/Generated image/)
  await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.naturalWidth)).toBeGreaterThan(0)
})

test('a provider failure is shown with its reason', async ({ page }) => {
  const ps = await seedPhotoshoot(page.request, 'E2E-FAIL', '[fake:fail]')
  await page.goto(`/photoshoots/${ps.id}`)
  await page.getByLabel('Recipe').selectOption('tryon')
  await page.getByLabel('Variations').fill('1')
  await page.getByRole('button', { name: 'Generate' }).click()
  const card = page.getByTestId('generation').first()
  await expect(card).toHaveAttribute('data-status', 'failed', { timeout: 20_000 })
  await expect(card.getByText(/simulated/)).toBeVisible()
})

test('a running generation can be cancelled', async ({ page }) => {
  const ps = await seedPhotoshoot(page.request, 'E2E-CANCEL', '[fake:slow]')
  await page.goto(`/photoshoots/${ps.id}`)
  await page.getByRole('button', { name: 'Generate' }).click()
  const card = page.getByTestId('generation').first()
  await expect(card).toHaveAttribute('data-status', 'running', { timeout: 10_000 })
  await card.getByRole('button', { name: 'Cancel' }).click()
  await card.getByRole('button', { name: 'Cancel generation' }).click()
  await expect(card).toHaveAttribute('data-status', 'cancelled', { timeout: 10_000 })
})

test('the variation cap is enforced by the server', async ({ page }) => {
  const ps = await seedPhotoshoot(page.request, 'E2E-CAP')
  const res = await page.request.post(`/api/v1/photoshoots/${ps.id}/generations`, {
    data: { recipe: 'reference', provider: 'fake', variations: 99 },
  })
  expect(res.status()).toBe(422)
  expect((await res.json()).error.fields.variations).toMatch(/between 1 and 4/)
})

test('FASHN is listed but unavailable without a key; try-on on the model photo runs', async ({ page }) => {
  const providers = await (await page.request.get('/api/v1/providers')).json()
  const fashn = providers.providers.find((p: { name: string }) => p.name === 'fashn')
  expect(fashn.available).toBe(false)
  expect(fashn.reason).toContain('AIFS_FASHN_API_KEY')

  const ps = await seedPhotoshoot(page.request, 'E2E-TRYON')
  const product = await (await page.request.get(`/api/v1/products/${ps.productId}`)).json()
  await page.goto(`/photoshoots/${ps.id}`)
  await expect(page.getByRole('option', { name: /FASHN virtual try-on/ })).toBeDisabled()

  await page.getByLabel('Recipe').selectOption('tryon-photo')
  await page.getByLabel('Garment photograph').selectOption(String(product.images[0].id))
  await page.getByLabel('Variations').fill('1')
  await page.getByRole('button', { name: 'Generate' }).click()
  const card = page.getByTestId('generation').first()
  await expect(card).toHaveAttribute('data-status', 'succeeded', { timeout: 20_000 })

  // The image page records which photograph was the garment input.
  await card.getByTestId('generated-image').first().click()
  await expect(page.getByText('sent to try-on')).toBeVisible()
  await expect(page.getByText('dressed by try-on')).toBeVisible()
})

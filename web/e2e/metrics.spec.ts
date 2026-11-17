import { expect, test } from '@playwright/test'
import { seedPhotoshoot } from './studio.ts'

test('reviewed images feed the metrics for the test set', async ({ page }) => {
  const ps = await seedPhotoshoot(page.request, 'E2E-METRICS')
  expect(
    (await page.request.patch(`/api/v1/products/${ps.productId}`, { data: { testSet: true, printComplexity: 'complex' } })).status(),
  ).toBe(200)

  let gen = await (
    await page.request.post(`/api/v1/photoshoots/${ps.id}/generations`, { data: { recipe: 'reference', provider: 'fake', variations: 2 } })
  ).json()
  await expect
    .poll(async () => (gen = await (await page.request.get(`/api/v1/generations/${gen.id}`)).json()).status, { timeout: 20_000 })
    .toBe('succeeded')
  const [first, second] = gen.images
  expect((await page.request.post(`/api/v1/images/${first.id}/review`, { data: { status: 'approved' } })).status()).toBe(200)
  expect(
    (await page.request.post(`/api/v1/images/${second.id}/review`, { data: { status: 'rejected', reason: 'fidelity' } })).status(),
  ).toBe(200)

  await page.goto(`/metrics?testSet=true&productId=${ps.productId}&groupBy=printComplexity`)
  const summary = page.getByLabel('Summary')
  await expect(summary.getByText('50%')).toHaveCount(2) // approval and fidelity pass
  const table = page.getByRole('table', { name: 'Metrics by group' })
  await expect(table.getByRole('rowheader', { name: 'complex' })).toBeVisible()
  await expect(page.getByLabel('Product')).toHaveValue(String(ps.productId))
})

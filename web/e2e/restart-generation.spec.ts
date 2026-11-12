import { expect, test } from '@playwright/test'
import { seedPhotoshoot } from './studio.ts'

// Run by `task e2e` in two passes around a container restart.
const NAME = 'E2E-RESTART'

test('start a slow generation @mid-generation', async ({ page }) => {
  const ps = await seedPhotoshoot(page.request, NAME, '[fake:slow]')
  const res = await page.request.post(`/api/v1/photoshoots/${ps.id}/generations`, {
    data: { recipe: 'reference', provider: 'fake', variations: 2 },
  })
  expect(res.status()).toBe(202)
  const { id } = await res.json()

  // Wait until the provider jobs are submitted, so the restart interrupts them.
  await expect
    .poll(async () => {
      const g = await (await page.request.get(`/api/v1/generations/${id}`)).json()
      return g.steps.filter((s: { status: string }) => s.status === 'submitted').length
    })
    .toBe(2)
})

test('the interrupted generation finishes after the restart, without duplicates @after-restart', async ({ page }) => {
  test.setTimeout(90_000)
  const shoots = (await (await page.request.get('/api/v1/photoshoots')).json()).items
  const ps = shoots.find((s: { name: string }) => s.name === `${NAME} shoot`)
  expect(ps).toBeTruthy()
  const detail = await (await page.request.get(`/api/v1/photoshoots/${ps.id}`)).json()
  expect(detail.generations).toHaveLength(1)
  const id = detail.generations[0].id

  await expect
    .poll(async () => (await (await page.request.get(`/api/v1/generations/${id}`)).json()).status, { timeout: 60_000 })
    .toBe('succeeded')
  const g = await (await page.request.get(`/api/v1/generations/${id}`)).json()
  expect(g.images).toHaveLength(2)
  expect(new Set(g.images.map((i: { variation: number }) => i.variation)).size).toBe(2)
  // The lost provider jobs were resubmitted once each.
  for (const st of g.steps) expect(st.attempts).toBe(2)
})

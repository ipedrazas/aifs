import { expect, test, type APIRequestContext } from '@playwright/test'
import { seedPhotoshoot } from './studio.ts'

async function generate(request: APIRequestContext, tag: string, variations: number) {
  const ps = await seedPhotoshoot(request, tag)
  const res = await request.post(`/api/v1/photoshoots/${ps.id}/generations`, {
    data: { recipe: 'reference', provider: 'fake', variations },
  })
  const { id } = await res.json()
  await expect
    .poll(async () => (await (await request.get(`/api/v1/generations/${id}`)).json()).status, { timeout: 20_000 })
    .toBe('succeeded')
  return (await (await request.get(`/api/v1/generations/${id}`)).json()) as {
    id: number
    images: { id: number }[]
  }
}

test('review a batch with the keyboard only, then change a decision', async ({ page }) => {
  const g = await generate(page.request, 'E2E-REVIEW', 3)

  await page.goto(`/review?generationId=${g.id}&status=pending`)
  await expect(page.getByTestId('gallery-image')).toHaveCount(3)
  await page.getByRole('link', { name: 'Review 3 images' }).click()
  await expect(page.getByTestId('queue-position')).toHaveText('1 of 3')
  const firstId = Number(new URL(page.url()).pathname.split('/').pop())

  await page.keyboard.press('n')
  await page.keyboard.type('print matches')
  await page.keyboard.press('Escape')
  await page.keyboard.press('a')
  await expect(page.getByTestId('queue-position')).toHaveText('2 of 3')
  await page.keyboard.press('r')
  await page.keyboard.press('2')
  await expect(page.getByTestId('queue-position')).toHaveText('3 of 3')
  await page.keyboard.press('f')
  await expect(page.getByText('End of the queue.')).toBeVisible()

  const statuses = async () => {
    const items = (await (await page.request.get(`/api/v1/images?generationId=${g.id}&status=`)).json()).items
    return Object.fromEntries(items.map((i: { id: number; reviewStatus: string }) => [i.id, i.reviewStatus]))
  }
  const after = await statuses()
  expect(Object.values(after).sort()).toEqual(['approved', 'needs_refinement', 'rejected'])
  expect(after[firstId]).toBe('approved')

  // Change the first decision; both are kept with their time.
  await page.goto(`/review/${firstId}`)
  await expect(page.getByTestId('queue-position')).toBeVisible()
  await page.keyboard.press('r')
  await page.keyboard.press('1')
  // The screen moves on to the next pending image; check the record itself.
  await expect
    .poll(async () => (await (await page.request.get(`/api/v1/images/${firstId}`)).json()).reviewHistory.length)
    .toBe(2)
  const d = await (await page.request.get(`/api/v1/images/${firstId}`)).json()
  expect(d.reviewStatus).toBe('rejected')
  expect(d.rejectionReason).toBe('fidelity')
  expect(d.reviewHistory.map((e: { status: string }) => e.status)).toEqual(['approved', 'rejected'])
  expect(d.reviewHistory[0].notes).toBe('print matches')

  await page.goto(`/review/${firstId}?generationId=${g.id}&status=`)
  await expect(page.getByRole('list', { name: 'Review history' }).getByRole('listitem')).toHaveCount(2)

  // Nothing pending is left in this generation.
  await page.goto(`/review?generationId=${g.id}&status=pending`)
  await expect(page.getByText('No images match these filters.')).toBeVisible()
})

test('zoom and pan are linked between the generated image and the reference', async ({ page }) => {
  const g = await generate(page.request, 'E2E-ZOOM', 1)
  await page.goto(`/review/${g.images[0].id}`)
  await expect(page.getByTestId('queue-position')).toBeVisible()

  const gen = page.getByTestId('pane-generated')
  const ref = page.getByTestId('pane-reference')
  await expect(gen).toBeVisible()
  await expect(ref).toBeVisible()
  const transform = async (pane: typeof gen) => pane.locator('div.origin-top-left').evaluate((el) => (el as HTMLElement).style.transform)

  // Zoom with the wheel over the generated image, then drag to pan.
  const box = (await gen.boundingBox())!
  await page.mouse.move(box.x + box.width / 3, box.y + box.height / 3)
  await page.mouse.wheel(0, -400)
  await expect(page.getByText('100%')).toHaveCount(0)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 4 })
  await page.mouse.up()

  const a = await transform(gen)
  expect(a).toMatch(/scale\((?!1\))/)
  expect(await transform(ref)).toBe(a)

  // The same view carries into swipe mode.
  await page.keyboard.press('m')
  await expect(page.getByTestId('pane-swipe')).toBeVisible()
  await page.keyboard.press('0')
  await expect(page.getByText('100%')).toBeVisible()
})

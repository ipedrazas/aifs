import { expect, test } from '@playwright/test'

test('health check reports ok', async ({ request }) => {
  const res = await request.get('/healthz')
  expect(res.status()).toBe(200)
  expect(await res.json()).toEqual({ status: 'ok' })
})

test('version endpoint returns JSON', async ({ request }) => {
  const res = await request.get('/api/v1/version')
  expect(res.status()).toBe(200)
  const body = await res.json()
  expect(body.version).toBeTruthy()
  expect(body.schemaVersion).toBeGreaterThanOrEqual(1)
})

test('the app shell shows the server version', async ({ page }) => {
  const expected = process.env.E2E_EXPECT_VERSION
  await page.goto('/')
  await expect(page.getByRole('link', { name: 'AI Fashion Studio' })).toBeVisible()
  const version = page.getByTestId('server-version')
  await expect(version).toBeVisible()
  if (expected) await expect(version).toContainText(`Server ${expected}`)
})

test('client-side routes survive a reload', async ({ page }) => {
  await page.goto('/does/not/exist')
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()
})

test('unknown API routes return a JSON 404', async ({ request }) => {
  const res = await request.get('/api/v1/nope')
  expect(res.status()).toBe(404)
  expect((await res.json()).error.code).toBe('not_found')
})

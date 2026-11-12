import { expect, test } from '@playwright/test'
import { logIn, password } from './helpers.ts'

test.describe('logged out', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('health check is public and reports ok', async ({ request }) => {
    const res = await request.get('/healthz')
    expect(res.status()).toBe(200)
    expect(await res.json()).toEqual({ status: 'ok' })
  })

  test('the API refuses requests without a session', async ({ request }) => {
    for (const path of ['/api/v1/version', '/api/v1/session', '/api/v1/products', '/api/v1/blobs/abc', '/api/v1/nope']) {
      const res = await request.get(path)
      expect(res.status(), path).toBe(401)
      expect((await res.json()).error.code, path).toBe('unauthenticated')
    }
  })

  test('a wrong password is rejected', async ({ page }) => {
    await page.goto('/')
    await page.getByLabel('Password').fill('not the password')
    await page.getByRole('button', { name: 'Log in' }).click()
    await expect(page.getByRole('alert')).toHaveText('Incorrect password.')
  })

  test('cross-origin logins are rejected', async ({ request }) => {
    const res = await request.post('/api/v1/session', {
      data: { password },
      headers: { Origin: 'https://evil.example', 'Sec-Fetch-Site': 'cross-site' },
    })
    expect(res.status()).toBe(403)
  })

  // Uses its own session, so logging out does not affect other tests.
  test('log in, see the server version, survive a reload, log out', async ({ page }) => {
    await logIn(page)

    const version = page.getByTestId('server-version')
    await expect(version).toBeVisible()
    const expected = process.env.E2E_EXPECT_VERSION
    if (expected) await expect(version).toContainText(`Server ${expected}`)

    const cookies = await page.context().cookies()
    const session = cookies.find((c) => c.name.endsWith('aifs_session'))
    expect(session?.httpOnly).toBe(true)
    expect(session?.sameSite).toBe('Strict')

    await page.reload()
    await expect(page.getByTestId('server-version')).toBeVisible()

    const res = await page.request.get('/api/v1/version')
    expect(res.status()).toBe(200)

    await page.getByRole('button', { name: 'Log out' }).click()
    await expect(page.getByLabel('Password')).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Password')).toBeVisible()
  })
})

test('client-side routes survive a reload', async ({ page }) => {
  await page.goto('/does/not/exist')
  await expect(page.getByRole('heading', { name: 'Page not found' })).toBeVisible()
})

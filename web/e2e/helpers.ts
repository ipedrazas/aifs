import { expect, type Page } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

export const password = process.env.E2E_PASSWORD ?? 'e2e-password-123'

export async function logIn(page: Page) {
  await page.goto('/')
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page.getByRole('button', { name: 'Log out' })).toBeVisible()
}

/** Image fixtures shared with the Go tests. */
export function fixturePath(name: string): string {
  return fileURLToPath(new URL(`../../api/internal/imaging/testdata/${name}`, import.meta.url))
}

export function fixture(name: string): Buffer {
  return readFileSync(fixturePath(name))
}

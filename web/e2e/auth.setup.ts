import { test as setup } from '@playwright/test'
import { authState } from '../playwright.config.ts'
import { logIn } from './helpers.ts'

setup('log in', async ({ page }) => {
  await logIn(page)
  await page.context().storageState({ path: authState })
})

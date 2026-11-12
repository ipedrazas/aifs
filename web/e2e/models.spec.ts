import { expect, test } from '@playwright/test'
import { fixture, fixturePath } from './helpers.ts'

test('create a model, add reference images by role, edit, delete', async ({ page }) => {
  await page.goto('/models')
  await page.getByRole('link', { name: 'New model' }).click()

  await page.getByLabel('Name').fill('E2E Ava')
  await page.getByLabel('Appearance').fill('tall, short dark hair')
  await page.getByRole('button', { name: 'Create model' }).click()
  await expect(page.getByRole('heading', { name: 'E2E Ava' })).toBeVisible()

  await page.getByLabel(/Choose photographs/).setInputFiles([fixturePath('rot6.jpg'), fixturePath('plain.png')])
  await page.getByLabel('Role for plain.png').selectOption('full_body')
  await page.getByRole('button', { name: 'Upload 2 photographs' }).click()
  await expect(page.getByTestId('model-image')).toHaveCount(2)
  await expect(page.getByRole('heading', { name: 'Identity (face)' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Full body' })).toBeVisible()

  const id = Number(page.url().split('/').pop())
  const model = await (await page.request.get(`/api/v1/models/${id}`)).json()
  expect(model.images.map((i: { role: string }) => i.role)).toEqual(['identity', 'full_body'])
  const res = await page.request.get(`/api/v1/blobs/${model.images[0].hash}`)
  expect(Buffer.compare(await res.body(), fixture('rot6.jpg'))).toBe(0)

  await page.getByLabel('Styling').fill('white trainers')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByLabel('Styling')).toHaveValue('white trainers')

  await page.goto('/models')
  await expect(page.getByText('E2E Ava')).toBeVisible()
  await page.getByText('E2E Ava').click()

  // Never used, so it is really deleted.
  await page.getByRole('button', { name: 'Delete model' }).click()
  await page.getByRole('button', { name: 'Delete model' }).click()
  await expect(page).toHaveURL(/\/models$/)
  await expect(page.getByText('E2E Ava')).toHaveCount(0)
  expect((await page.request.get(`/api/v1/models/${id}`)).status()).toBe(404)
})

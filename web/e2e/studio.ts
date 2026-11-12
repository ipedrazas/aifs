import { expect, type APIRequestContext } from '@playwright/test'
import { fixture } from './helpers.ts'

/** Creates a product and a model with reference images through the API. */
export async function seedSubjects(request: APIRequestContext, tag: string) {
  const product = await (
    await request.post('/api/v1/products', { data: { name: `${tag} jeans`, sku: `${tag}-${Date.now()}` } })
  ).json()
  expect(
    (
      await request.post(`/api/v1/products/${product.id}/images`, {
        multipart: { view: 'front', file: { name: 'front.png', mimeType: 'image/png', buffer: fixture('plain.png') } },
      })
    ).status(),
  ).toBe(201)

  const model = await (await request.post('/api/v1/models', { data: { name: `${tag} model` } })).json()
  expect(
    (
      await request.post(`/api/v1/models/${model.id}/images`, {
        multipart: { role: 'identity', file: { name: 'face.jpg', mimeType: 'image/jpeg', buffer: fixture('rot6.jpg') } },
      })
    ).status(),
  ).toBe(201)
  return { product, model }
}

/** Creates a photoshoot through the API. */
export async function seedPhotoshoot(request: APIRequestContext, tag: string, customInstructions = '') {
  const { product, model } = await seedSubjects(request, tag)
  const res = await request.post('/api/v1/photoshoots', {
    data: { productId: product.id, modelId: model.id, name: `${tag} shoot`, customInstructions },
  })
  expect(res.status()).toBe(201)
  return res.json()
}

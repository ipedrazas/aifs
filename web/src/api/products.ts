import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type PrintComplexity, type ProcessOptions, type ProductInput, type View } from './client'

export const VIEWS: View[] = ['front', 'back', 'side', 'detail', 'print']

export const VIEW_LABELS: Record<View, string> = {
  front: 'Front',
  back: 'Back',
  side: 'Side',
  detail: 'Detail',
  print: 'Print close-up',
}

const keys = {
  all: ['products'] as const,
  one: (id: number) => ['products', id] as const,
}

export function useProducts() {
  return useQuery({
    queryKey: keys.all,
    queryFn: async () => unwrap(await api.GET('/api/v1/products')).items,
  })
}

export function useProduct(id: number) {
  return useQuery({
    queryKey: keys.one(id),
    queryFn: async () => unwrap(await api.GET('/api/v1/products/{id}', { params: { path: { id } } })),
  })
}

export function useCreateProduct() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (body: ProductInput) => unwrap(await api.POST('/api/v1/products', { body })),
    onSuccess: (product) => {
      client.setQueryData(keys.one(product.id), product)
      void client.invalidateQueries({ queryKey: keys.all, exact: true })
    },
  })
}

export function useUpdateProduct(id: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (body: ProductInput) =>
      unwrap(await api.PATCH('/api/v1/products/{id}', { params: { path: { id } }, body })),
    onSuccess: (product) => {
      client.setQueryData(keys.one(id), product)
      void client.invalidateQueries({ queryKey: keys.all, exact: true })
    },
  })
}

export function useDeleteProduct(id: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE('/api/v1/products/{id}', { params: { path: { id } } }))
    },
    onSuccess: () => {
      client.removeQueries({ queryKey: keys.one(id) })
      void client.invalidateQueries({ queryKey: keys.all, exact: true })
    },
  })
}

export function useUploadImage(productId: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ file, view }: { file: File; view: View }) => {
      const form = new FormData()
      form.append('view', view)
      form.append('file', file, file.name)
      return unwrap(
        await api.POST('/api/v1/products/{id}/images', {
          params: { path: { id: productId } },
          // The generated type describes the fields; send them as multipart.
          body: { view, file: file as unknown as string },
          bodySerializer: () => form,
        }),
      )
    },
    onSuccess: () => client.invalidateQueries({ queryKey: keys.all }),
  })
}

export function useDeleteImage(productId: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (imageId: number) => {
      unwrap(
        await api.DELETE('/api/v1/products/{id}/images/{imageId}', {
          params: { path: { id: productId, imageId } },
        }),
      )
    },
    onSuccess: () => client.invalidateQueries({ queryKey: keys.all }),
  })
}

/** Creates the processed version of an original photograph (replacing any previous one). */
export function useProcessImage(productId: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ imageId, options }: { imageId: number; options: ProcessOptions }) =>
      unwrap(
        await api.POST('/api/v1/products/{id}/images/{imageId}/process', {
          params: { path: { id: productId, imageId } },
          body: options,
        }),
      ),
    onSettled: () => client.invalidateQueries({ queryKey: keys.all }),
  })
}

export const STEP_LABELS: Record<string, string> = {
  remove_background: 'background removed',
  trim: 'trimmed',
  resize: 'resized',
}

export const COMPLEXITY_LABELS: Record<PrintComplexity, string> = {
  '': 'Not rated',
  none: 'No print',
  simple: 'Simple print',
  complex: 'Complex print',
}

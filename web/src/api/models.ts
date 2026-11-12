import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap, type Model, type ModelImageRole, type ModelInput } from './client'

export const ROLES: ModelImageRole[] = ['identity', 'full_body']

export const ROLE_LABELS: Record<ModelImageRole, string> = {
  identity: 'Identity (face)',
  full_body: 'Full body',
}

const keys = {
  all: ['models'] as const,
  list: (archived: boolean) => ['models', 'list', archived] as const,
  one: (id: number) => ['models', id] as const,
}

export function useModels(includeArchived = false) {
  return useQuery({
    queryKey: keys.list(includeArchived),
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/models', { params: { query: { archived: includeArchived } } })).items,
  })
}

export function useModel(id: number) {
  return useQuery({
    queryKey: keys.one(id),
    queryFn: async () => unwrap(await api.GET('/api/v1/models/{id}', { params: { path: { id } } })),
  })
}

function useInvalidateLists() {
  const client = useQueryClient()
  return () => client.invalidateQueries({ queryKey: [...keys.all, 'list'] })
}

export function useCreateModel() {
  const client = useQueryClient()
  const invalidate = useInvalidateLists()
  return useMutation({
    mutationFn: async (body: ModelInput) => unwrap(await api.POST('/api/v1/models', { body })),
    onSuccess: (model) => {
      client.setQueryData(keys.one(model.id), model)
      void invalidate()
    },
  })
}

export function useUpdateModel(id: number) {
  const client = useQueryClient()
  const invalidate = useInvalidateLists()
  return useMutation({
    mutationFn: async (body: ModelInput) =>
      unwrap(await api.PATCH('/api/v1/models/{id}', { params: { path: { id } }, body })),
    onSuccess: (model) => {
      client.setQueryData(keys.one(id), model)
      void invalidate()
    },
  })
}

/**
 * Deletes an unused model. A model that has been used is archived by the
 * server instead; the result says which happened.
 */
export function useDeleteModel(id: number) {
  const client = useQueryClient()
  const invalidate = useInvalidateLists()
  return useMutation({
    mutationFn: async (): Promise<{ archived: Model | null }> => {
      const result = await api.DELETE('/api/v1/models/{id}', { params: { path: { id } } })
      const data = unwrap(result)
      return { archived: result.response.status === 200 ? (data as Model) : null }
    },
    onSuccess: ({ archived }) => {
      if (archived) client.setQueryData(keys.one(id), archived)
      else client.removeQueries({ queryKey: keys.one(id) })
      void invalidate()
    },
  })
}

export function useUploadModelImage(modelId: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({ file, role }: { file: File; role: ModelImageRole }) => {
      const form = new FormData()
      form.append('role', role)
      form.append('file', file, file.name)
      return unwrap(
        await api.POST('/api/v1/models/{id}/images', {
          params: { path: { id: modelId } },
          body: { role, file: file as unknown as string },
          bodySerializer: () => form,
        }),
      )
    },
    onSuccess: () => client.invalidateQueries({ queryKey: keys.all }),
  })
}

export function useDeleteModelImage(modelId: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (imageId: number) => {
      unwrap(
        await api.DELETE('/api/v1/models/{id}/images/{imageId}', {
          params: { path: { id: modelId, imageId } },
        }),
      )
    },
    onSuccess: () => client.invalidateQueries({ queryKey: keys.all }),
  })
}

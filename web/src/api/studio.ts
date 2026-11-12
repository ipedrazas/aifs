import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  api,
  unwrap,
  type Generation,
  type GenerationRequest,
  type Photoshoot,
  type PhotoshootInput,
  type ReviewStatus,
} from './client'

/** How often to refresh while a generation is queued or running. */
export const ACTIVE_POLL_MS = 1500

export function isActive(g: Pick<Generation, 'status'>): boolean {
  return g.status === 'queued' || g.status === 'running'
}

const keys = {
  photoshoots: ['photoshoots'] as const,
  photoshootList: (productId?: number) => ['photoshoots', 'list', productId ?? 0] as const,
  photoshoot: (id: number) => ['photoshoots', id] as const,
  images: ['images'] as const,
  image: (id: number) => ['images', id] as const,
  providers: ['providers'] as const,
}

export function useProviders() {
  return useQuery({
    queryKey: keys.providers,
    queryFn: async () => unwrap(await api.GET('/api/v1/providers')),
    staleTime: 60_000,
  })
}

export function usePhotoshoots(productId?: number) {
  return useQuery({
    queryKey: keys.photoshootList(productId),
    queryFn: async () =>
      unwrap(await api.GET('/api/v1/photoshoots', { params: { query: productId ? { productId } : {} } })).items,
  })
}

/** A photoshoot with its generations; refreshes while any is active. */
export function usePhotoshoot(id: number) {
  return useQuery({
    queryKey: keys.photoshoot(id),
    queryFn: async () => unwrap(await api.GET('/api/v1/photoshoots/{id}', { params: { path: { id } } })),
    refetchInterval: (q) => (q.state.data?.generations?.some(isActive) ? ACTIVE_POLL_MS : false),
  })
}

export function useCreatePhotoshoot() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (body: PhotoshootInput) => unwrap(await api.POST('/api/v1/photoshoots', { body })),
    onSuccess: (ps: Photoshoot) => {
      client.setQueryData(keys.photoshoot(ps.id), ps)
      void client.invalidateQueries({ queryKey: [...keys.photoshoots, 'list'] })
    },
  })
}

export function useUpdatePhotoshoot(id: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (body: PhotoshootInput) =>
      unwrap(await api.PATCH('/api/v1/photoshoots/{id}', { params: { path: { id } }, body })),
    onSuccess: (ps: Photoshoot) => {
      client.setQueryData(keys.photoshoot(id), ps)
      void client.invalidateQueries({ queryKey: [...keys.photoshoots, 'list'] })
    },
  })
}

export function useDeletePhotoshoot(id: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE('/api/v1/photoshoots/{id}', { params: { path: { id } } }))
    },
    onSuccess: () => {
      client.removeQueries({ queryKey: keys.photoshoot(id) })
      void client.invalidateQueries({ queryKey: [...keys.photoshoots, 'list'] })
    },
  })
}

export function useStartGeneration(photoshootId: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (body: GenerationRequest) =>
      unwrap(
        await api.POST('/api/v1/photoshoots/{id}/generations', { params: { path: { id: photoshootId } }, body }),
      ),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.photoshoots })
      void client.invalidateQueries({ queryKey: keys.images })
    },
  })
}

export function useCancelGeneration(photoshootId: number) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (id: number) =>
      unwrap(await api.POST('/api/v1/generations/{id}/cancel', { params: { path: { id } } })),
    onSettled: () => client.invalidateQueries({ queryKey: keys.photoshoot(photoshootId) }),
  })
}

export function useImages(filter: { productId?: number; photoshootId?: number; status?: ReviewStatus }) {
  return useQuery({
    queryKey: [...keys.images, 'list', filter],
    queryFn: async () => unwrap(await api.GET('/api/v1/images', { params: { query: filter } })).items,
  })
}

export function useImage(id: number) {
  return useQuery({
    queryKey: keys.image(id),
    queryFn: async () => unwrap(await api.GET('/api/v1/images/{id}', { params: { path: { id } } })),
  })
}

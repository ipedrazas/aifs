import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from './client'

export function useVersion() {
  return useQuery({
    queryKey: ['version'],
    queryFn: async () => unwrap(await api.GET('/api/v1/version')),
    staleTime: Infinity,
  })
}

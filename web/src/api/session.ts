import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, isUnauthenticated, unwrap, type Session } from './client'

export const sessionKey = ['session'] as const

/** The current session, or null when logged out. */
export function useSession() {
  return useQuery({
    queryKey: sessionKey,
    queryFn: async (): Promise<Session | null> => {
      try {
        return unwrap(await api.GET('/api/v1/session'))
      } catch (err) {
        if (isUnauthenticated(err)) return null
        throw err
      }
    },
    staleTime: Infinity,
  })
}

export function useLogin() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (password: string) =>
      unwrap(await api.POST('/api/v1/session', { body: { password } })),
    onSuccess: (session) => client.setQueryData(sessionKey, session),
  })
}

export function useLogout() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async () => {
      const result = await api.DELETE('/api/v1/session')
      // Already logged out is fine.
      if (result.response.status !== 401) unwrap(result)
    },
    onSettled: () => {
      // Drop every cached response so nothing from the session lingers.
      client.setQueryData(sessionKey, null)
      client.removeQueries({ predicate: (q) => q.queryKey[0] !== sessionKey[0] })
    },
  })
}

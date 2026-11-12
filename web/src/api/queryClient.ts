import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { ApiError, isUnauthenticated } from './client'
import { sessionKey } from './session'

export function makeQueryClient() {
  // Any request that finds the session gone sends the user back to login.
  const onError = (err: unknown) => {
    if (isUnauthenticated(err)) client.setQueryData(sessionKey, null)
  }
  const client: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError }),
    mutationCache: new MutationCache({ onError }),
    defaultOptions: {
      queries: {
        refetchOnWindowFocus: false,
        // Client errors will not fix themselves; retry only server/network errors once.
        retry: (count, err) => count < 1 && !(err instanceof ApiError && err.status < 500),
      },
    },
  })
  return client
}

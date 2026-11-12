import type { ReactNode } from 'react'
import { useSession } from '../api/session'
import { Login } from '../pages/Login'

/** Renders children only for a logged-in user; otherwise the login screen. */
export function AuthGate({ children }: { children: ReactNode }) {
  const { data: session, error, isPending, refetch } = useSession()

  if (isPending) {
    return <div className="p-8 text-center text-stone-500">Loading…</div>
  }
  if (error) {
    return (
      <div role="alert" className="p-8 text-center text-stone-700">
        <p>Cannot reach the server: {error.message}</p>
        <button className="mt-3 underline" onClick={() => void refetch()}>
          Try again
        </button>
      </div>
    )
  }
  if (!session) return <Login />
  return <>{children}</>
}

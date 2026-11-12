import { Link, Outlet } from 'react-router'
import { useVersion } from '../api/queries'
import { useLogout } from '../api/session'

export function Layout() {
  return (
    <div className="flex min-h-screen flex-col bg-stone-50 text-stone-900">
      <header className="border-b border-stone-200 bg-white">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-4 py-3">
          <Link to="/" className="font-semibold tracking-tight">
            AI Fashion Studio
          </Link>
          <LogoutButton />
        </div>
      </header>
      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-8">
        <Outlet />
      </main>
      <footer className="border-t border-stone-200 px-4 py-3 text-center text-xs text-stone-500">
        <ServerVersion />
      </footer>
    </div>
  )
}

export function ServerVersion() {
  const { data, error, isPending } = useVersion()
  if (isPending) return <span>Connecting to server…</span>
  if (error) return <span role="alert">Server unavailable: {error.message}</span>
  return (
    <span data-testid="server-version">
      Server {data.version} · schema v{data.schemaVersion}
    </span>
  )
}

function LogoutButton() {
  const logout = useLogout()
  return (
    <button
      type="button"
      onClick={() => logout.mutate()}
      disabled={logout.isPending}
      className="text-sm text-stone-600 hover:text-stone-900 disabled:opacity-50"
    >
      Log out
    </button>
  )
}

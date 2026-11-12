import { useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'
import { useLogin } from '../api/session'

function describe(err: Error): string {
  if (err instanceof ApiError) {
    if (err.code === 'invalid_password') return 'Incorrect password.'
    if (err.status === 429) {
      return err.retryAfter
        ? `Too many attempts. Try again in ${err.retryAfter} seconds.`
        : 'Too many attempts. Try again shortly.'
    }
  }
  return err.message
}

export function Login() {
  const [password, setPassword] = useState('')
  const login = useLogin()

  function submit(e: FormEvent) {
    e.preventDefault()
    if (password) login.mutate(password, { onError: () => setPassword('') })
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-stone-50 px-4">
      <form
        onSubmit={submit}
        className="w-full max-w-sm space-y-4 rounded-lg border border-stone-200 bg-white p-6 shadow-sm"
      >
        <h1 className="text-xl font-semibold text-stone-900">AI Fashion Studio</h1>
        <label className="block space-y-1">
          <span className="text-sm text-stone-600">Password</span>
          <input
            type="password"
            name="password"
            autoComplete="current-password"
            autoFocus
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full rounded border border-stone-300 px-3 py-2 focus:border-stone-500 focus:outline-none"
          />
        </label>
        {login.error && (
          <p role="alert" className="text-sm text-red-700">
            {describe(login.error)}
          </p>
        )}
        <button
          type="submit"
          disabled={login.isPending || !password}
          className="w-full rounded bg-stone-900 px-3 py-2 font-medium text-white disabled:opacity-50"
        >
          {login.isPending ? 'Logging in…' : 'Log in'}
        </button>
      </form>
    </div>
  )
}

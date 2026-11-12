import { Link } from 'react-router'

export function NotFound() {
  return (
    <section className="space-y-2">
      <h1 className="text-2xl font-semibold">Page not found</h1>
      <Link to="/" className="text-stone-600 underline">
        Back to the start
      </Link>
    </section>
  )
}

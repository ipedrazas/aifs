export function ErrorMessage({ error }: { error: Error | null | undefined }) {
  if (!error) return null
  return (
    <p role="alert" className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800">
      {error.message}
    </p>
  )
}

import { useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'

export type FieldSpec<K extends string> = {
  name: K
  label: string
  multiline?: boolean
  hint?: string
  required?: boolean
}

/** A form of text fields that shows per-field errors from the API. */
export function FieldsForm<K extends string>({
  idPrefix,
  fields,
  initial,
  onSubmit,
  submitLabel,
  pending,
  error,
}: {
  idPrefix: string
  fields: FieldSpec<K>[]
  initial: Record<K, string>
  onSubmit: (values: Record<K, string>) => void
  submitLabel: string
  pending: boolean
  error: Error | null
}) {
  const [values, setValues] = useState(initial)
  const fieldErrors = error instanceof ApiError ? error.fields : {}
  const generalError = error && Object.keys(fieldErrors).length === 0 ? error.message : null

  function submit(e: FormEvent) {
    e.preventDefault()
    onSubmit(values)
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      {fields.map((f) => {
        const id = `${idPrefix}-${f.name}`
        const err = fieldErrors[f.name]
        const common = {
          id,
          name: f.name,
          value: values[f.name],
          onChange: (e: { target: { value: string } }) => setValues((v) => ({ ...v, [f.name]: e.target.value })),
          'aria-invalid': err ? true : undefined,
          className: 'w-full rounded border border-stone-300 px-3 py-2 focus:border-stone-500 focus:outline-none',
        }
        return (
          <div key={f.name} className="space-y-1">
            <label htmlFor={id} className="block text-sm font-medium text-stone-700">
              {f.label}
            </label>
            {f.multiline ? <textarea rows={3} {...common} /> : <input type="text" {...common} required={f.required} />}
            {f.hint && !err && <p className="text-xs text-stone-500">{f.hint}</p>}
            {err && <p className="text-xs text-red-700">{err}</p>}
          </div>
        )
      })}
      {generalError && (
        <p role="alert" className="text-sm text-red-700">
          {generalError}
        </p>
      )}
      <button
        type="submit"
        disabled={pending}
        className="rounded bg-stone-900 px-4 py-2 font-medium text-white disabled:opacity-50"
      >
        {pending ? 'Saving…' : submitLabel}
      </button>
    </form>
  )
}

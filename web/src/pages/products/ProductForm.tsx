import { useState, type FormEvent } from 'react'
import { ApiError, type Product, type ProductInput } from '../../api/client'

type Field = keyof Required<ProductInput>

const FIELDS: { name: Field; label: string; multiline?: boolean; hint?: string }[] = [
  { name: 'name', label: 'Name' },
  { name: 'sku', label: 'Identifier (SKU)', hint: 'Used in exported file names.' },
  { name: 'category', label: 'Category' },
  { name: 'colour', label: 'Colour' },
  { name: 'printDetails', label: 'Print details', multiline: true, hint: 'Prints, embroidery, logos, distressing.' },
  { name: 'description', label: 'Description', multiline: true },
  { name: 'notes', label: 'Notes', multiline: true },
]

function initialValues(p?: Product): Record<Field, string> {
  return {
    name: p?.name ?? '',
    sku: p?.sku ?? '',
    category: p?.category ?? 'jeans',
    colour: p?.colour ?? '',
    printDetails: p?.printDetails ?? '',
    description: p?.description ?? '',
    notes: p?.notes ?? '',
  }
}

export function ProductForm({
  product,
  onSubmit,
  submitLabel,
  pending,
  error,
}: {
  product?: Product
  onSubmit: (input: ProductInput) => void
  submitLabel: string
  pending: boolean
  error: Error | null
}) {
  const [values, setValues] = useState(() => initialValues(product))
  const fieldErrors = error instanceof ApiError ? error.fields : {}
  const generalError = error && Object.keys(fieldErrors).length === 0 ? error.message : null

  function submit(e: FormEvent) {
    e.preventDefault()
    onSubmit(values)
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      {FIELDS.map((f) => {
        const id = `product-${f.name}`
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
            {f.multiline ? <textarea rows={3} {...common} /> : <input type="text" {...common} required={f.name === 'name'} />}
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

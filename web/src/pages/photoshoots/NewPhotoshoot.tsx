import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router'
import { ApiError, type AspectRatio } from '../../api/client'
import { useModels } from '../../api/models'
import { useProducts } from '../../api/products'
import { useCreatePhotoshoot } from '../../api/studio'
import { ASPECT_RATIOS } from './aspectRatios'

const fieldClass = 'w-full rounded border border-stone-300 px-3 py-2 focus:border-stone-500 focus:outline-none'

export function NewPhotoshoot() {
  const [params] = useSearchParams()
  const products = useProducts()
  const models = useModels()
  const create = useCreatePhotoshoot()
  const navigate = useNavigate()

  const [name, setName] = useState('')
  const [productId, setProductId] = useState(params.get('productId') ?? '')
  const [modelId, setModelId] = useState(params.get('modelId') ?? '')
  const [aspectRatio, setAspectRatio] = useState<AspectRatio>('3:4')
  const [instructions, setInstructions] = useState('')

  const fieldErrors = create.error instanceof ApiError ? create.error.fields : {}

  function submit(e: FormEvent) {
    e.preventDefault()
    create.mutate(
      {
        name,
        productId: Number(productId),
        modelId: Number(modelId),
        aspectRatio,
        customInstructions: instructions,
      },
      { onSuccess: (ps) => void navigate(`/photoshoots/${ps.id}`) },
    )
  }

  return (
    <section className="max-w-xl space-y-6">
      <Link to="/photoshoots" className="text-sm text-stone-600 hover:underline">
        ← Photoshoots
      </Link>
      <h1 className="text-2xl font-semibold">New photoshoot</h1>

      <form onSubmit={submit} className="space-y-4">
        <div className="space-y-1">
          <label htmlFor="ps-product" className="block text-sm font-medium text-stone-700">
            Product
          </label>
          <select id="ps-product" required value={productId} onChange={(e) => setProductId(e.target.value)} className={fieldClass}>
            <option value="">Choose a product…</option>
            {products.data?.map((p) => (
              <option key={p.id} value={p.id} disabled={p.imageCount === 0}>
                {p.name}
                {p.sku ? ` (${p.sku})` : ''}
                {p.imageCount === 0 ? ' — no photographs' : ''}
              </option>
            ))}
          </select>
          {fieldErrors.productId && <p className="text-xs text-red-700">{fieldErrors.productId}</p>}
        </div>

        <div className="space-y-1">
          <label htmlFor="ps-model" className="block text-sm font-medium text-stone-700">
            Model
          </label>
          <select id="ps-model" required value={modelId} onChange={(e) => setModelId(e.target.value)} className={fieldClass}>
            <option value="">Choose a model…</option>
            {models.data?.map((m) => (
              <option key={m.id} value={m.id} disabled={m.imageCount === 0}>
                {m.name}
                {m.imageCount === 0 ? ' — no images' : ''}
              </option>
            ))}
          </select>
          {fieldErrors.modelId && <p className="text-xs text-red-700">{fieldErrors.modelId}</p>}
        </div>

        <div className="space-y-1">
          <label htmlFor="ps-name" className="block text-sm font-medium text-stone-700">
            Name
          </label>
          <input id="ps-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Defaults to “product on model”" className={fieldClass} />
        </div>

        <div className="space-y-1">
          <label htmlFor="ps-ratio" className="block text-sm font-medium text-stone-700">
            Aspect ratio
          </label>
          <select id="ps-ratio" value={aspectRatio} onChange={(e) => setAspectRatio(e.target.value as AspectRatio)} className={fieldClass}>
            {ASPECT_RATIOS.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1">
          <label htmlFor="ps-instructions" className="block text-sm font-medium text-stone-700">
            Custom instructions
          </label>
          <textarea id="ps-instructions" rows={3} value={instructions} onChange={(e) => setInstructions(e.target.value)} className={fieldClass} />
          <p className="text-xs text-stone-500">Passed to the provider with every generation, e.g. “hands in pockets”.</p>
        </div>

        {create.error && Object.keys(fieldErrors).length === 0 && (
          <p role="alert" className="text-sm text-red-700">
            {create.error.message}
          </p>
        )}
        <button type="submit" disabled={create.isPending} className="rounded bg-stone-900 px-4 py-2 font-medium text-white disabled:opacity-50">
          {create.isPending ? 'Creating…' : 'Create photoshoot'}
        </button>
      </form>
    </section>
  )
}

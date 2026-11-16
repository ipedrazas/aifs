import { useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { blobUrl, type AspectRatio, type Generation, type Photoshoot } from '../../api/client'
import {
  isActive,
  useCancelGeneration,
  useDeletePhotoshoot,
  usePhotoshoot,
  useProviders,
  useStartGeneration,
  useUpdatePhotoshoot,
} from '../../api/studio'
import { StatusBadge } from '../../components/StatusBadge'
import { ConfirmButton } from '../../components/ui/ConfirmButton'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { useProduct, VIEW_LABELS } from '../../api/products'
import { formatCost, formatDate, formatDuration } from '../../lib/format'
import { NotFound } from '../NotFound'
import { ASPECT_RATIOS } from './aspectRatios'

const fieldClass = 'w-full rounded border border-stone-300 px-3 py-2 focus:border-stone-500 focus:outline-none'

export function PhotoshootDetail() {
  const id = Number(useParams().id)
  const { data: ps, error, isPending } = usePhotoshoot(id)

  if (!Number.isInteger(id) || id <= 0) return <NotFound />
  if (isPending) return <p className="text-stone-500">Loading…</p>
  if (error) {
    if ('status' in error && error.status === 404) return <NotFound />
    return <ErrorMessage error={error} />
  }

  return (
    <section className="space-y-8">
      <div>
        <Link to="/photoshoots" className="text-sm text-stone-600 hover:underline">
          ← Photoshoots
        </Link>
        <h1 className="mt-2 text-2xl font-semibold">{ps.name}</h1>
      </div>

      <div className="grid gap-8 lg:grid-cols-[1fr_2fr]">
        <div className="space-y-6">
          <Subjects ps={ps} />
          <GeneratePanel ps={ps} />
          <Settings key={ps.updatedAt} ps={ps} />
        </div>
        <div className="space-y-4">
          <h2 className="text-lg font-semibold">Generations</h2>
          {(ps.generations ?? []).length === 0 && <p className="text-stone-600">Nothing generated yet.</p>}
          {(ps.generations ?? []).map((g) => (
            <GenerationCard key={g.id} g={g} photoshootId={ps.id} />
          ))}
        </div>
      </div>
    </section>
  )
}

function Subjects({ ps }: { ps: Photoshoot }) {
  return (
    <div className="grid grid-cols-2 gap-3">
      {[
        { to: `/products/${ps.product.id}`, label: 'Product', name: ps.product.name, hash: ps.product.coverHash },
        { to: `/models/${ps.model.id}`, label: 'Model', name: ps.model.name, hash: ps.model.coverHash },
      ].map((s) => (
        <Link key={s.label} to={s.to} className="overflow-hidden rounded border border-stone-200 bg-white hover:border-stone-400">
          <div className="aspect-square bg-stone-100">
            {s.hash && <img src={blobUrl(s.hash, 256)} alt="" className="h-full w-full object-contain" />}
          </div>
          <div className="p-2">
            <p className="text-xs text-stone-500">{s.label}</p>
            <p className="truncate text-sm font-medium">{s.name}</p>
          </div>
        </Link>
      ))}
      {ps.model.archived && (
        <p className="col-span-2 text-sm text-amber-800">The model is archived. Restore it or choose another model to generate.</p>
      )}
    </div>
  )
}

function GeneratePanel({ ps }: { ps: Photoshoot }) {
  const providers = useProviders()
  const product = useProduct(ps.productId)
  const start = useStartGeneration(ps.id)
  const [recipe, setRecipe] = useState('reference')
  const [provider, setProvider] = useState('')
  const [variations, setVariations] = useState(2)
  const [garment, setGarment] = useState('')

  const info = providers.data
  const available = info?.providers.filter((p) => p.available) ?? []
  const unavailable = info?.providers.filter((p) => !p.available) ?? []
  const eligible = available.filter((p) => p.recipes.includes(recipe))
  const chosen = eligible.find((p) => p.name === provider) ?? eligible[0]
  const max = info?.maxVariations ?? 4
  const recipeInfo = info?.recipes.find((r) => r.name === recipe)
  const steps = recipeInfo?.steps.length ?? 1
  const usesTryOn = recipeInfo?.steps.some((s) => s.capability === 'try_on') ?? false
  const images = product.data?.images ?? []

  function submit(e: FormEvent) {
    e.preventDefault()
    if (!chosen) return
    start.mutate({ recipe, provider: chosen.name, variations, ...(garment ? { garmentImageId: Number(garment) } : {}) })
  }

  if (info && available.length === 0) {
    return (
      <div className="space-y-1 rounded border border-stone-200 bg-white p-4 text-sm text-stone-600">
        <p>No AI provider is configured, so nothing can be generated yet.</p>
        {unavailable.map((p) => (
          <p key={p.name} className="text-xs">
            {p.label}: {p.reason}
          </p>
        ))}
      </div>
    )
  }

  return (
    <form onSubmit={submit} className="space-y-3 rounded border border-stone-200 bg-white p-4">
      <h2 className="font-semibold">Generate</h2>
      <div className="space-y-1">
        <label htmlFor="gen-recipe" className="block text-sm text-stone-700">
          Recipe
        </label>
        <select id="gen-recipe" value={recipe} onChange={(e) => setRecipe(e.target.value)} className={fieldClass}>
          {info?.recipes.map((r) => (
            <option key={r.name} value={r.name}>
              {r.label}
            </option>
          ))}
        </select>
        <p className="text-xs text-stone-500">{info?.recipes.find((r) => r.name === recipe)?.description}</p>
      </div>
      <div className="space-y-1">
        <label htmlFor="gen-provider" className="block text-sm text-stone-700">
          Provider
        </label>
        <select id="gen-provider" value={chosen?.name ?? ''} onChange={(e) => setProvider(e.target.value)} className={fieldClass}>
          {eligible.map((p) => (
            <option key={p.name} value={p.name}>
              {p.label}
            </option>
          ))}
          {unavailable.map((p) => (
            <option key={p.name} value={p.name} disabled>
              {p.label} — {p.reason}
            </option>
          ))}
        </select>
        {!chosen && <p className="text-xs text-amber-800">No configured provider can run this recipe.</p>}
      </div>
      {usesTryOn && images.length > 0 && (
        <div className="space-y-1">
          <label htmlFor="gen-garment" className="block text-sm text-stone-700">
            Garment photograph
          </label>
          <select id="gen-garment" value={garment} onChange={(e) => setGarment(e.target.value)} className={fieldClass}>
            <option value="">Front view (default)</option>
            {images.map((im) => (
              <option key={im.id} value={im.id}>
                {VIEW_LABELS[im.view]}
                {im.filename ? ` — ${im.filename}` : ''}
              </option>
            ))}
          </select>
          <p className="text-xs text-stone-500">Try-on sends one garment photograph; choose the view that matches the shot.</p>
        </div>
      )}
      <div className="space-y-1">
        <label htmlFor="gen-variations" className="block text-sm text-stone-700">
          Variations
        </label>
        <input
          id="gen-variations"
          type="number"
          min={1}
          max={max}
          value={variations}
          onChange={(e) => setVariations(Math.max(1, Math.min(max, Number(e.target.value) || 1)))}
          className={fieldClass}
        />
        <p className="text-xs text-stone-500">
          {`Up to ${max}. Estimated cost: ${chosen ? formatCost(chosen.unitPriceMicros * steps * variations) : '–'} ` +
            `(price per call × ${steps} call${steps === 1 ? '' : 's'} × ${variations}).`}
        </p>
      </div>
      <ErrorMessage error={start.error} />
      <button
        type="submit"
        disabled={start.isPending || !chosen || ps.model.archived}
        className="w-full rounded bg-stone-900 px-4 py-2 font-medium text-white disabled:opacity-50"
      >
        {start.isPending ? 'Queueing…' : 'Generate'}
      </button>
    </form>
  )
}

function Settings({ ps }: { ps: Photoshoot }) {
  const update = useUpdatePhotoshoot(ps.id)
  const del = useDeletePhotoshoot(ps.id)
  const navigate = useNavigate()
  const [aspectRatio, setAspectRatio] = useState<AspectRatio>(ps.aspectRatio)
  const [instructions, setInstructions] = useState(ps.customInstructions)

  return (
    <div className="space-y-3 rounded border border-stone-200 bg-white p-4">
      <h2 className="font-semibold">Settings</h2>
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault()
          update.mutate({ aspectRatio, customInstructions: instructions })
        }}
      >
        <div className="space-y-1">
          <label htmlFor="set-ratio" className="block text-sm text-stone-700">
            Aspect ratio
          </label>
          <select id="set-ratio" value={aspectRatio} onChange={(e) => setAspectRatio(e.target.value as AspectRatio)} className={fieldClass}>
            {ASPECT_RATIOS.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
        </div>
        <div className="space-y-1">
          <label htmlFor="set-instructions" className="block text-sm text-stone-700">
            Custom instructions
          </label>
          <textarea id="set-instructions" rows={3} value={instructions} onChange={(e) => setInstructions(e.target.value)} className={fieldClass} />
        </div>
        <ErrorMessage error={update.error} />
        <button type="submit" disabled={update.isPending} className="rounded border border-stone-300 px-3 py-1.5 text-sm font-medium disabled:opacity-50">
          Save settings
        </button>
        <p className="text-xs text-stone-500">Changes apply to new generations; past ones keep the settings they used.</p>
      </form>
      {ps.generationCount === 0 && (
        <div className="border-t border-stone-200 pt-3">
          <ErrorMessage error={del.error} />
          <ConfirmButton
            confirmLabel="Delete photoshoot"
            disabled={del.isPending}
            onConfirm={() => del.mutate(undefined, { onSuccess: () => void navigate('/photoshoots') })}
          >
            Delete photoshoot
          </ConfirmButton>
        </div>
      )}
    </div>
  )
}

function GenerationCard({ g, photoshootId }: { g: Generation; photoshootId: number }) {
  const cancel = useCancelGeneration(photoshootId)
  const active = isActive(g)
  const byVariation = new Map(g.images.map((im) => [im.variation, im]))
  const slots = Array.from({ length: g.variations }, (_, i) => i + 1)

  return (
    <article className="space-y-3 rounded border border-stone-200 bg-white p-4" data-testid="generation" data-status={g.status}>
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <StatusBadge status={g.status} />
          <span className="text-sm text-stone-700">
            {g.recipe} · {g.provider} · {g.variations} variation{g.variations === 1 ? '' : 's'}
          </span>
        </div>
        <div className="flex items-center gap-3 text-xs text-stone-500">
          <span title="Created">{formatDate(g.createdAt)}</span>
          <span title="Duration">{formatDuration(g.startedAt, g.finishedAt)}</span>
          <span title="Cost">{formatCost(g.costMicros)}</span>
          {g.images.length > 0 && (
            <Link to={`/review/${g.images[g.images.length - 1].id}?generationId=${g.id}&status=`} className="font-medium text-stone-700 underline">
              Review
            </Link>
          )}
          {active && (
            <ConfirmButton confirmLabel="Cancel generation" disabled={cancel.isPending || g.cancelRequested} onConfirm={() => cancel.mutate(g.id)}>
              {g.cancelRequested ? 'Cancelling…' : 'Cancel'}
            </ConfirmButton>
          )}
        </div>
      </header>

      {active && (
        <div className="h-1.5 overflow-hidden rounded bg-stone-100" role="progressbar" aria-valuemin={0} aria-valuemax={g.stepsTotal} aria-valuenow={g.stepsDone}>
          <div className="h-full bg-sky-500 transition-all" style={{ width: `${(100 * g.stepsDone) / Math.max(1, g.stepsTotal)}%` }} />
        </div>
      )}
      {g.error && <p className="text-sm text-red-700">{g.error}</p>}
      <ErrorMessage error={cancel.error} />

      <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {slots.map((v) => {
          const im = byVariation.get(v)
          if (im) {
            return (
              <li key={v}>
                <Link to={`/images/${im.id}`} className="relative block overflow-hidden rounded border border-stone-200 hover:border-stone-400" data-testid="generated-image">
                  <img src={blobUrl(im.hash, 512)} alt={`Variation ${v}`} loading="lazy" className="aspect-[3/4] w-full bg-stone-100 object-cover" />
                  {im.reviewStatus !== 'pending' && (
                    <span className="absolute left-1 top-1">
                      <StatusBadge status={im.reviewStatus} />
                    </span>
                  )}
                </Link>
              </li>
            )
          }
          return (
            <li key={v} className="flex aspect-[3/4] items-center justify-center rounded border border-dashed border-stone-300 text-xs text-stone-400">
              {active ? 'Generating…' : 'No image'}
            </li>
          )
        })}
      </ul>
    </article>
  )
}

import { Link, useParams } from 'react-router'
import { blobUrl } from '../../api/client'
import { useImage } from '../../api/studio'
import { StatusBadge } from '../../components/StatusBadge'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { formatCost, formatDate, formatDuration } from '../../lib/format'
import { NotFound } from '../NotFound'

/** A generated image with everything that produced it. */
export function ImageDetailPage() {
  const id = Number(useParams().id)
  const { data: im, error, isPending } = useImage(id)

  if (!Number.isInteger(id) || id <= 0) return <NotFound />
  if (isPending) return <p className="text-stone-500">Loading…</p>
  if (error) {
    if ('status' in error && error.status === 404) return <NotFound />
    return <ErrorMessage error={error} />
  }

  const s = im.settings
  const first = im.steps[0]
  const last = im.steps[im.steps.length - 1]

  return (
    <section className="space-y-6">
      <Link to={`/photoshoots/${im.photoshootId}`} className="text-sm text-stone-600 hover:underline">
        ← {s.photoshoot.name}
      </Link>

      <div className="grid gap-8 lg:grid-cols-[3fr_2fr]">
        <a href={blobUrl(im.hash)} target="_blank" rel="noreferrer" title="Open the full-size image">
          <img src={blobUrl(im.hash, 2048)} alt={`Generated image ${im.id}`} className="w-full rounded border border-stone-200 bg-white" />
        </a>

        <div className="space-y-6 text-sm">
          <div className="flex items-center justify-between">
            <h1 className="text-xl font-semibold">Image {im.id}</h1>
            <StatusBadge status={im.reviewStatus} />
          </div>

          <dl className="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2" aria-label="Provenance">
            <dt className="text-stone-500">Product</dt>
            <dd>
              <Link to={`/products/${s.product.id}`} className="underline">
                {s.product.name}
              </Link>
              {s.product.sku && <span className="text-stone-500"> · {s.product.sku}</span>}
            </dd>
            <dt className="text-stone-500">Model</dt>
            <dd>
              <Link to={`/models/${s.model.id}`} className="underline">
                {s.model.name}
              </Link>
            </dd>
            <dt className="text-stone-500">Recipe</dt>
            <dd>{s.recipe}</dd>
            <dt className="text-stone-500">Provider</dt>
            <dd>{s.provider}</dd>
            <dt className="text-stone-500">Provider models</dt>
            <dd>{s.steps.map((st) => `${st.purpose}: ${st.providerModel}`).join(', ')}</dd>
            <dt className="text-stone-500">Aspect ratio</dt>
            <dd>{s.aspectRatio}</dd>
            <dt className="text-stone-500">Variation</dt>
            <dd>
              {im.variation} of {s.variations}
            </dd>
            <dt className="text-stone-500">Instructions</dt>
            <dd>{s.customInstructions || '–'}</dd>
            <dt className="text-stone-500">Created</dt>
            <dd>{formatDate(im.createdAt)}</dd>
            <dt className="text-stone-500">Time</dt>
            <dd>{formatDuration(first?.startedAt, last?.finishedAt)}</dd>
            <dt className="text-stone-500">Cost</dt>
            <dd>{formatCost(im.costMicros)}</dd>
          </dl>

          <div className="space-y-2">
            <h2 className="font-semibold">Reference photographs sent</h2>
            <ul className="grid grid-cols-4 gap-2">
              {s.inputs.map((input) => (
                <li key={input.position} className="space-y-1">
                  <a href={blobUrl(input.hash)} target="_blank" rel="noreferrer">
                    <img src={blobUrl(input.hash, 256)} alt={`${input.role}: ${input.label}`} className="aspect-square w-full rounded border border-stone-200 bg-stone-100 object-contain" />
                  </a>
                  <p className="text-xs text-stone-500">
                    {input.role} · {input.label}
                  </p>
                </li>
              ))}
            </ul>
          </div>

          <details className="space-y-2">
            <summary className="cursor-pointer font-semibold">Prompt</summary>
            <pre className="whitespace-pre-wrap rounded bg-stone-100 p-3 text-xs">{im.prompt || '(none)'}</pre>
          </details>
        </div>
      </div>
    </section>
  )
}

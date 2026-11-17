import { useState, type FormEvent } from 'react'
import { ApiError, blobUrl, type ProcessOptions, type ProductImage } from '../../api/client'
import { STEP_LABELS, useDeleteImage, useProcessImage, VIEW_LABELS, VIEWS } from '../../api/products'
import { useProviders } from '../../api/studio'
import { ConfirmButton } from '../../components/ui/ConfirmButton'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { formatCost } from '../../lib/format'

const fieldClass = 'rounded border border-stone-300 px-2 py-1 text-sm focus:border-stone-500 focus:outline-none'

/** Long-edge sizes offered by the resize step (0 = keep the size). */
const EDGES = [0, 4096, 3072, 2048, 1536]

const DEFAULTS: ProcessOptions = { removeBackground: true, trim: true, maxEdge: 3072 }

/** Validation errors name the field; show the reasons, not "some fields are invalid". */
function readable(error: Error | null): Error | null {
  if (error instanceof ApiError && Object.keys(error.fields).length > 0) {
    return new Error(Object.values(error.fields).join('; '))
  }
  return error
}

/** The service that removes backgrounds, or why there is none. */
function useBackgroundRemover() {
  const { data } = useProviders()
  const all = data?.providers.filter((p) => p.capabilities.includes('remove_background')) ?? []
  const available = all.filter((p) => p.available)
  // A real service is preferred over the offline fake, as on the server.
  const remover = available.find((p) => p.name !== 'fake') ?? available[0]
  const reason = all.find((p) => !p.available)?.reason ?? 'no background-removal service is configured'
  return { remover, reason }
}

function describeProcessing(im: ProductImage): string {
  const p = im.processing
  if (!p) return ''
  const steps = p.steps.map((s) => (s === 'resize' && p.maxEdge ? `resized to ${p.maxEdge} px` : STEP_LABELS[s] ?? s))
  const text = steps.join(', ')
  return text.charAt(0).toUpperCase() + text.slice(1) + (p.costMicros > 0 ? ` · ${formatCost(p.costMicros)}` : '')
}

/**
 * The product's reference photographs, grouped by view. Each original can
 * have one processed version (background removed, trimmed, resized), which
 * generation uses instead of the original.
 */
export function ProductPhotos({ productId, images }: { productId: number; images: ProductImage[] }) {
  const originals = images.filter((i) => i.sourceImageId == null)
  const processed = new Map(images.filter((i) => i.sourceImageId != null).map((i) => [i.sourceImageId!, i]))

  if (originals.length === 0) {
    return <p className="text-stone-600">No photographs yet. Upload front, back and close-ups of any print.</p>
  }
  return (
    <div className="space-y-4">
      <ProcessAll productId={productId} pending={originals.filter((o) => !processed.has(o.id))} />
      {VIEWS.filter((v) => originals.some((o) => o.view === v)).map((v) => (
        <div key={v} className="space-y-2">
          <h3 className="text-sm font-medium text-stone-700">{VIEW_LABELS[v]}</h3>
          <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {originals
              .filter((o) => o.view === v)
              .map((o) => (
                <PhotoCard key={o.id} productId={productId} original={o} version={processed.get(o.id)} />
              ))}
          </ul>
        </div>
      ))}
    </div>
  )
}

function PhotoCard({
  productId,
  original,
  version,
}: {
  productId: number
  original: ProductImage
  version?: ProductImage
}) {
  const [showOriginal, setShowOriginal] = useState(false)
  const [editing, setEditing] = useState(false)
  const process = useProcessImage(productId)
  const del = useDeleteImage(productId)
  const shown = version && !showOriginal ? version : original
  const label = `${VIEW_LABELS[original.view]}: ${shown.filename}`

  return (
    <li className="overflow-hidden rounded border border-stone-200 bg-white" data-testid="product-image">
      <a href={blobUrl(shown.hash)} target="_blank" rel="noreferrer" title="Open full size">
        <img
          src={blobUrl(shown.hash, 512)}
          alt={label}
          loading="lazy"
          className="aspect-[3/4] w-full bg-stone-100 object-contain"
        />
      </a>
      <div className="space-y-2 p-2 text-xs">
        <div className="flex flex-wrap items-center gap-2">
          {version ? (
            <span className="rounded bg-emerald-100 px-1.5 py-0.5 font-medium text-emerald-800">
              {showOriginal ? 'Original (not used)' : 'Processed · used for generation'}
            </span>
          ) : (
            <span className="rounded bg-stone-100 px-1.5 py-0.5 font-medium text-stone-700">Original</span>
          )}
          <span className="min-w-0 truncate text-stone-500" title={shown.filename}>
            {shown.width}×{shown.height}
            {shown.filename ? ` · ${shown.filename}` : ''}
          </span>
        </div>
        {version && !showOriginal && <p className="text-stone-600">{describeProcessing(version)}</p>}
        {process.isPending && <p className="text-stone-600">Processing… this takes a few seconds.</p>}
        <ErrorMessage error={readable(process.error ?? del.error)} />
        {editing && !process.isPending && (
          <ProcessForm
            submitLabel={version ? 'Process again' : 'Process'}
            onCancel={() => setEditing(false)}
            onSubmit={(options) =>
              process.mutate(
                { imageId: original.id, options },
                {
                  onSuccess: () => {
                    setEditing(false)
                    setShowOriginal(false)
                  },
                },
              )
            }
          />
        )}
        <div className="flex flex-wrap gap-3">
          {version && (
            <button type="button" className="text-stone-700 underline" onClick={() => setShowOriginal(!showOriginal)}>
              {showOriginal ? 'Show processed' : 'Show original'}
            </button>
          )}
          {!editing && (
            <button
              type="button"
              className="text-stone-700 underline"
              disabled={process.isPending}
              onClick={() => setEditing(true)}
            >
              {version ? 'Process again…' : 'Process…'}
            </button>
          )}
          {version && (
            <ConfirmButton
              onConfirm={() => del.mutate(version.id, { onSuccess: () => setShowOriginal(false) })}
              disabled={del.isPending || process.isPending}
              confirmLabel="Revert"
            >
              Revert to original
            </ConfirmButton>
          )}
          <ConfirmButton
            onConfirm={() => del.mutate(original.id)}
            disabled={del.isPending || process.isPending}
            confirmLabel={version ? 'Delete both versions' : 'Delete'}
          >
            Delete
          </ConfirmButton>
        </div>
      </div>
    </li>
  )
}

/** The processing steps. Background removal needs a configured service. */
function ProcessForm({
  submitLabel,
  onSubmit,
  onCancel,
}: {
  submitLabel: string
  onSubmit: (options: ProcessOptions) => void
  onCancel?: () => void
}) {
  const { remover, reason } = useBackgroundRemover()
  const [removeBackground, setRemoveBackground] = useState(Boolean(DEFAULTS.removeBackground))
  const [trim, setTrim] = useState(Boolean(DEFAULTS.trim))
  const [maxEdge, setMaxEdge] = useState(DEFAULTS.maxEdge ?? 0)
  const remove = removeBackground && Boolean(remover)
  const nothing = !remove && !trim && maxEdge === 0

  function submit(e: FormEvent) {
    e.preventDefault()
    if (nothing) return
    onSubmit({ removeBackground: remove, trim, maxEdge })
  }

  return (
    <form onSubmit={submit} className="space-y-2 rounded border border-stone-200 bg-stone-50 p-2" aria-label="Processing steps">
      <label className="flex items-start gap-2">
        <input
          type="checkbox"
          checked={remove}
          disabled={!remover}
          onChange={(e) => setRemoveBackground(e.target.checked)}
        />
        <span>
          Remove the background
          <span className="block text-stone-500">
            {remover
              ? `${remover.label} · about ${formatCost(remover.unitPriceMicros)} per photograph. Only the outline comes from the service; the garment's pixels are kept.`
              : `Unavailable: ${reason}`}
          </span>
        </span>
      </label>
      <label className="flex items-start gap-2">
        <input type="checkbox" checked={trim} onChange={(e) => setTrim(e.target.checked)} />
        <span>
          Trim to the garment
          <span className="block text-stone-500">Needs a transparent background.</span>
        </span>
      </label>
      <label className="flex items-center gap-2">
        <span>Long edge</span>
        <select value={maxEdge} onChange={(e) => setMaxEdge(Number(e.target.value))} className={fieldClass}>
          {EDGES.map((edge) => (
            <option key={edge} value={edge}>
              {edge === 0 ? 'Keep the size' : `At most ${edge} px`}
            </option>
          ))}
        </select>
      </label>
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={nothing}
          className="rounded bg-stone-900 px-3 py-1 text-xs font-medium text-white disabled:opacity-50"
        >
          {submitLabel}
        </button>
        {onCancel && (
          <button type="button" onClick={onCancel} className="rounded border border-stone-300 px-3 py-1 text-xs">
            Cancel
          </button>
        )}
      </div>
    </form>
  )
}

/** Processes, one at a time, every original that has no processed version yet. */
function ProcessAll({ productId, pending }: { productId: number; pending: ProductImage[] }) {
  const [open, setOpen] = useState(false)
  const [progress, setProgress] = useState<{ done: number; total: number; failed: string[] } | null>(null)
  const process = useProcessImage(productId)
  const running = progress !== null && progress.done < progress.total

  async function run(options: ProcessOptions) {
    const queue = [...pending]
    const failed: string[] = []
    setProgress({ done: 0, total: queue.length, failed })
    for (const [i, im] of queue.entries()) {
      try {
        await process.mutateAsync({ imageId: im.id, options })
      } catch (err) {
        failed.push(`${VIEW_LABELS[im.view]} ${im.filename}: ${readable(err as Error)?.message}`)
      }
      setProgress({ done: i + 1, total: queue.length, failed: [...failed] })
    }
    setOpen(false)
  }

  if (pending.length === 0 && !progress) return null
  return (
    <div className="space-y-2 rounded border border-stone-200 bg-white p-3 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-stone-700">
          {pending.length === 0
            ? 'Every photograph has a processed version.'
            : `${pending.length} photograph${pending.length === 1 ? '' : 's'} not processed yet.`}
        </p>
        {!open && !running && pending.length > 0 && (
          <button type="button" onClick={() => setOpen(true)} className="rounded border border-stone-300 px-3 py-1 text-xs">
            Process all…
          </button>
        )}
      </div>
      {open && !running && (
        <ProcessForm submitLabel={`Process ${pending.length}`} onSubmit={(o) => void run(o)} onCancel={() => setOpen(false)} />
      )}
      {progress && (
        <p className="text-xs text-stone-600" role="status">
          {running ? `Processing ${progress.done + 1} of ${progress.total}…` : `Processed ${progress.total - progress.failed.length} of ${progress.total}.`}
        </p>
      )}
      {progress?.failed.map((f) => (
        <p key={f} role="alert" className="text-xs text-red-800">
          {f}
        </p>
      ))}
    </div>
  )
}

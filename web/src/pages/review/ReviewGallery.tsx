import { Link, useSearchParams } from 'react-router'
import { blobUrl, type ReviewStatus } from '../../api/client'
import { useProducts } from '../../api/products'
import { useImages, usePhotoshoots } from '../../api/studio'
import { StatusBadge } from '../../components/StatusBadge'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { filterFromParams, paramsFromFilter, STATUS_OPTIONS } from './filters'

const selectClass = 'rounded border border-stone-300 px-2 py-1.5 text-sm'

export function ReviewGallery() {
  const [params, setParams] = useSearchParams()
  const filter = filterFromParams(params)
  const products = useProducts()
  const shoots = usePhotoshoots(filter.productId)
  const { data: images, error, isPending } = useImages(filter)
  const query = paramsFromFilter(filter).toString()

  function update(change: Partial<typeof filter>) {
    const next = { ...filter, ...change }
    if ('productId' in change) next.photoshootId = undefined
    setParams(paramsFromFilter(next))
  }

  return (
    <section className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold">Review</h1>
        {images && images.length > 0 && (
          <Link to={`/review/${images[0].id}?${query}`} className="rounded bg-stone-900 px-4 py-2 text-sm font-medium text-white">
            Review {images.length} image{images.length === 1 ? '' : 's'}
          </Link>
        )}
      </div>

      <div className="flex flex-wrap gap-3" role="group" aria-label="Filters">
        <label className="flex items-center gap-2 text-sm text-stone-600">
          Status
          <select
            className={selectClass}
            value={filter.status ?? ''}
            onChange={(e) => update({ status: (e.target.value || undefined) as ReviewStatus | undefined })}
          >
            {STATUS_OPTIONS.map((o) => (
              <option key={o.value} value={o.value}>
                {o.label}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center gap-2 text-sm text-stone-600">
          Product
          <select className={selectClass} value={filter.productId ?? ''} onChange={(e) => update({ productId: Number(e.target.value) || undefined })}>
            <option value="">All products</option>
            {products.data?.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center gap-2 text-sm text-stone-600">
          Photoshoot
          <select className={selectClass} value={filter.photoshootId ?? ''} onChange={(e) => update({ photoshootId: Number(e.target.value) || undefined })}>
            <option value="">All photoshoots</option>
            {shoots.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
      </div>

      <ErrorMessage error={error} />
      {isPending && <p className="text-stone-500">Loading…</p>}
      {images && images.length === 0 && <p className="text-stone-600">No images match these filters.</p>}

      {images && images.length > 0 && (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-4 lg:grid-cols-6">
          {images.map((im) => (
            <li key={im.id}>
              <Link
                to={`/review/${im.id}?${query}`}
                className="block overflow-hidden rounded border border-stone-200 bg-white hover:border-stone-400"
                data-testid="gallery-image"
              >
                <img src={blobUrl(im.hash, 512)} alt={`Image ${im.id}`} loading="lazy" className="aspect-[3/4] w-full bg-stone-100 object-cover" />
                <div className="space-y-1 p-2">
                  <StatusBadge status={im.reviewStatus} />
                  <p className="truncate text-xs text-stone-600">{im.product.name}</p>
                  <p className="truncate text-xs text-stone-400">
                    {im.model.name} · {im.recipe}
                  </p>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

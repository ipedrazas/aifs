import { Link } from 'react-router'
import { blobUrl } from '../../api/client'
import { usePhotoshoots } from '../../api/studio'
import { ErrorMessage } from '../../components/ui/ErrorMessage'

export function PhotoshootList() {
  const { data: shoots, error, isPending } = usePhotoshoots()

  return (
    <section className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Photoshoots</h1>
        <Link to="/photoshoots/new" className="rounded bg-stone-900 px-4 py-2 text-sm font-medium text-white">
          New photoshoot
        </Link>
      </div>

      <ErrorMessage error={error} />
      {isPending && <p className="text-stone-500">Loading…</p>}
      {shoots && shoots.length === 0 && (
        <p className="text-stone-600">No photoshoots yet. A photoshoot pairs a product with a model; generate from it as often as you like.</p>
      )}

      {shoots && shoots.length > 0 && (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">
          {shoots.map((ps) => {
            const thumb = ps.latestImageHash || ps.product.coverHash
            return (
              <li key={ps.id}>
                <Link
                  to={`/photoshoots/${ps.id}`}
                  className="block overflow-hidden rounded-lg border border-stone-200 bg-white hover:border-stone-400"
                >
                  <div className="flex aspect-[3/4] items-center justify-center bg-stone-100">
                    {thumb ? (
                      <img src={blobUrl(thumb, 512)} alt="" loading="lazy" className="h-full w-full object-cover" />
                    ) : (
                      <span className="text-sm text-stone-400">No images yet</span>
                    )}
                  </div>
                  <div className="space-y-0.5 p-3">
                    <p className="font-medium">{ps.name}</p>
                    <p className="text-xs text-stone-500">
                      {ps.generationCount} generation{ps.generationCount === 1 ? '' : 's'} · {ps.aspectRatio}
                    </p>
                  </div>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

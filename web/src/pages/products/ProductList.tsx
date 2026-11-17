import { Link } from 'react-router'
import { blobUrl } from '../../api/client'
import { COMPLEXITY_LABELS, useProducts } from '../../api/products'
import { ErrorMessage } from '../../components/ui/ErrorMessage'

export function ProductList() {
  const { data: products, error, isPending } = useProducts()

  return (
    <section className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Products</h1>
        <Link to="/products/new" className="rounded bg-stone-900 px-4 py-2 text-sm font-medium text-white">
          New product
        </Link>
      </div>

      <ErrorMessage error={error} />
      {isPending && <p className="text-stone-500">Loading…</p>}
      {products && products.length === 0 && (
        <p className="text-stone-600">No products yet. Create one and upload its reference photographs.</p>
      )}

      {products && products.length > 0 && (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">
          {products.map((p) => (
            <li key={p.id}>
              <Link
                to={`/products/${p.id}`}
                className="block overflow-hidden rounded-lg border border-stone-200 bg-white hover:border-stone-400"
              >
                <div className="flex aspect-[3/4] items-center justify-center bg-stone-100">
                  {p.cover ? (
                    <img
                      src={blobUrl(p.cover.hash, 512)}
                      alt=""
                      loading="lazy"
                      className="h-full w-full object-cover"
                    />
                  ) : (
                    <span className="text-sm text-stone-400">No photographs</span>
                  )}
                </div>
                <div className="space-y-0.5 p-3">
                  <p className="font-medium">{p.name}</p>
                  <p className="text-xs text-stone-500">
                    {p.sku || 'No identifier'} · {p.imageCount} photo{p.imageCount === 1 ? '' : 's'}
                  </p>
                  {(p.testSet || p.printComplexity) && (
                    <p className="flex flex-wrap gap-1 pt-1 text-xs">
                      {p.testSet && <span className="rounded bg-sky-100 px-1.5 py-0.5 text-sky-800">Test set</span>}
                      {p.printComplexity && (
                        <span className="rounded bg-stone-100 px-1.5 py-0.5 text-stone-700">{COMPLEXITY_LABELS[p.printComplexity]}</span>
                      )}
                    </p>
                  )}
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

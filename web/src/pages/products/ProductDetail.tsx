import { Link, useNavigate, useParams } from 'react-router'
import { blobUrl, type ProductImage } from '../../api/client'
import {
  useDeleteImage,
  useDeleteProduct,
  useProduct,
  useUpdateProduct,
  VIEW_LABELS,
  VIEWS,
} from '../../api/products'
import { ConfirmButton } from '../../components/ui/ConfirmButton'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { NotFound } from '../NotFound'
import { ImageUploader } from './ImageUploader'
import { ProductForm } from './ProductForm'

export function ProductDetail() {
  const id = Number(useParams().id)
  const { data: product, error, isPending } = useProduct(id)

  if (!Number.isInteger(id) || id <= 0) return <NotFound />
  if (isPending) return <p className="text-stone-500">Loading…</p>
  if (error) {
    if ('status' in error && error.status === 404) return <NotFound />
    return <ErrorMessage error={error} />
  }

  return (
    <section className="space-y-8">
      <div>
        <Link to="/products" className="text-sm text-stone-600 hover:underline">
          ← Products
        </Link>
        <h1 className="mt-2 text-2xl font-semibold">{product.name}</h1>
        {product.sku && <p className="text-sm text-stone-500">{product.sku}</p>}
      </div>

      <div className="grid gap-8 lg:grid-cols-[2fr_1fr]">
        <div className="space-y-6">
          <h2 className="text-lg font-semibold">Reference photographs</h2>
          <Photos productId={product.id} images={product.images ?? []} />
          <ImageUploader productId={product.id} />
        </div>
        <div className="space-y-6">
          <h2 className="text-lg font-semibold">Details</h2>
          <EditProduct key={product.updatedAt} product={product} />
          <DeleteProduct id={product.id} />
        </div>
      </div>
    </section>
  )
}

function EditProduct({ product }: { product: NonNullable<ReturnType<typeof useProduct>['data']> }) {
  const update = useUpdateProduct(product.id)
  return (
    <ProductForm
      product={product}
      submitLabel="Save changes"
      pending={update.isPending}
      error={update.error}
      onSubmit={(input) => update.mutate(input)}
    />
  )
}

function Photos({ productId, images }: { productId: number; images: ProductImage[] }) {
  const del = useDeleteImage(productId)
  if (images.length === 0) {
    return <p className="text-stone-600">No photographs yet. Upload front, back and close-ups of any print.</p>
  }
  return (
    <div className="space-y-4">
      <ErrorMessage error={del.error} />
      {VIEWS.filter((v) => images.some((i) => i.view === v)).map((view) => (
        <div key={view} className="space-y-2">
          <h3 className="text-sm font-medium text-stone-700">{VIEW_LABELS[view]}</h3>
          <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            {images
              .filter((i) => i.view === view)
              .map((im) => (
                <li key={im.id} className="overflow-hidden rounded border border-stone-200 bg-white" data-testid="product-image">
                  <a href={blobUrl(im.hash)} target="_blank" rel="noreferrer" title="Open the original">
                    <img
                      src={blobUrl(im.hash, 512)}
                      alt={`${VIEW_LABELS[view]} view: ${im.filename}`}
                      loading="lazy"
                      className="aspect-[3/4] w-full bg-stone-100 object-contain"
                    />
                  </a>
                  <div className="flex items-center justify-between gap-2 p-2">
                    <p className="min-w-0 truncate text-xs text-stone-500" title={im.filename}>
                      {im.width}×{im.height}
                      {im.filename ? ` · ${im.filename}` : ''}
                    </p>
                    <ConfirmButton onConfirm={() => del.mutate(im.id)} disabled={del.isPending} confirmLabel="Delete">
                      Delete
                    </ConfirmButton>
                  </div>
                </li>
              ))}
          </ul>
        </div>
      ))}
    </div>
  )
}

function DeleteProduct({ id }: { id: number }) {
  const del = useDeleteProduct(id)
  const navigate = useNavigate()
  return (
    <div className="space-y-2 border-t border-stone-200 pt-4">
      <ErrorMessage error={del.error} />
      <ConfirmButton
        confirmLabel="Delete product and photographs"
        disabled={del.isPending}
        onConfirm={() => del.mutate(undefined, { onSuccess: () => void navigate('/products') })}
      >
        Delete product
      </ConfirmButton>
    </div>
  )
}

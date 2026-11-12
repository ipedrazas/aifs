import { Link, useNavigate, useParams } from 'react-router'
import type { ProductImage, View } from '../../api/client'
import {
  useDeleteImage,
  useDeleteProduct,
  useProduct,
  useUpdateProduct,
  useUploadImage,
  VIEW_LABELS,
  VIEWS,
} from '../../api/products'
import { ImageGroups } from '../../components/ImageGroups'
import { ImageUploader } from '../../components/ImageUploader'
import { ConfirmButton } from '../../components/ui/ConfirmButton'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { NotFound } from '../NotFound'
import { guessView } from './guessView'
import { ProductForm } from './ProductForm'

const VIEW_OPTIONS = VIEWS.map((v) => ({ value: v, label: VIEW_LABELS[v] }))

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
          <Uploader productId={product.id} />
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

function Uploader({ productId }: { productId: number }) {
  const upload = useUploadImage(productId)
  return (
    <ImageUploader
      options={VIEW_OPTIONS}
      labelName="View"
      guess={guessView}
      upload={(file, view) => upload.mutateAsync({ file, view: view as View })}
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
      <ImageGroups
        images={images.map((i) => ({ ...i, group: i.view }))}
        groups={VIEW_OPTIONS}
        onDelete={(id) => del.mutate(id)}
        deleting={del.isPending}
        testId="product-image"
      />
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

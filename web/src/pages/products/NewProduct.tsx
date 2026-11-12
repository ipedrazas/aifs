import { Link, useNavigate } from 'react-router'
import { useCreateProduct } from '../../api/products'
import { ProductForm } from './ProductForm'

export function NewProduct() {
  const create = useCreateProduct()
  const navigate = useNavigate()

  return (
    <section className="max-w-xl space-y-6">
      <Link to="/products" className="text-sm text-stone-600 hover:underline">
        ← Products
      </Link>
      <h1 className="text-2xl font-semibold">New product</h1>
      <ProductForm
        submitLabel="Create product"
        pending={create.isPending}
        error={create.error}
        onSubmit={(input) =>
          create.mutate(input, { onSuccess: (p) => void navigate(`/products/${p.id}`) })
        }
      />
    </section>
  )
}

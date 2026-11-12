import type { Product, ProductInput } from '../../api/client'
import { FieldsForm, type FieldSpec } from '../../components/FieldsForm'

type Field = keyof Required<Omit<ProductInput, never>>

const FIELDS: FieldSpec<Field>[] = [
  { name: 'name', label: 'Name', required: true },
  { name: 'sku', label: 'Identifier (SKU)', hint: 'Used in exported file names.' },
  { name: 'category', label: 'Category' },
  { name: 'colour', label: 'Colour' },
  { name: 'printDetails', label: 'Print details', multiline: true, hint: 'Prints, embroidery, logos, distressing.' },
  { name: 'description', label: 'Description', multiline: true },
  { name: 'notes', label: 'Notes', multiline: true },
]

export function ProductForm({
  product,
  ...rest
}: {
  product?: Product
  onSubmit: (input: ProductInput) => void
  submitLabel: string
  pending: boolean
  error: Error | null
}) {
  return (
    <FieldsForm
      idPrefix="product"
      fields={FIELDS}
      initial={{
        name: product?.name ?? '',
        sku: product?.sku ?? '',
        category: product?.category ?? 'jeans',
        colour: product?.colour ?? '',
        printDetails: product?.printDetails ?? '',
        description: product?.description ?? '',
        notes: product?.notes ?? '',
      }}
      {...rest}
    />
  )
}

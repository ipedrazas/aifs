import type { PrintComplexity, Product, ProductInput } from '../../api/client'
import { COMPLEXITY_LABELS } from '../../api/products'
import { FieldsForm, type FieldSpec } from '../../components/FieldsForm'

type Field = keyof Required<ProductInput>

const FIELDS: FieldSpec<Field>[] = [
  { name: 'name', label: 'Name', required: true },
  { name: 'sku', label: 'Identifier (SKU)', hint: 'Used in exported file names.' },
  { name: 'category', label: 'Category' },
  { name: 'colour', label: 'Colour' },
  { name: 'printDetails', label: 'Print details', multiline: true, hint: 'Prints, embroidery, logos, distressing.' },
  { name: 'description', label: 'Description', multiline: true },
  { name: 'notes', label: 'Notes', multiline: true },
  {
    name: 'printComplexity',
    label: 'Print complexity',
    hint: 'How hard the print is to reproduce. Metrics can be read per complexity.',
    options: Object.entries(COMPLEXITY_LABELS).map(([value, label]) => ({ value, label })),
  },
  {
    name: 'testSet',
    label: 'In the fidelity test set',
    hint: 'Garments used to evaluate the providers (include the hardest prints).',
    checkbox: true,
  },
]

export function ProductForm({
  product,
  onSubmit,
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
        printComplexity: product?.printComplexity ?? '',
        testSet: String(product?.testSet ?? false),
      }}
      onSubmit={(v) =>
        onSubmit({ ...v, printComplexity: v.printComplexity as PrintComplexity, testSet: v.testSet === 'true' })
      }
      {...rest}
    />
  )
}

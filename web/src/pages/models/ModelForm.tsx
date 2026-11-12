import type { Model, ModelInput } from '../../api/client'
import { FieldsForm, type FieldSpec } from '../../components/FieldsForm'

type Field = 'name' | 'description' | 'appearance' | 'preferredPoses' | 'styling'

const FIELDS: FieldSpec<Field>[] = [
  { name: 'name', label: 'Name', required: true },
  { name: 'description', label: 'Profile', multiline: true, hint: 'Who this model is and the impression they give.' },
  { name: 'appearance', label: 'Appearance', multiline: true, hint: 'Height, build, hair, skin tone, age range.' },
  { name: 'preferredPoses', label: 'Preferred poses', multiline: true },
  { name: 'styling', label: 'Styling', multiline: true, hint: 'Default tops, shoes and accessories to pair with garments.' },
]

export function ModelForm({
  model,
  ...rest
}: {
  model?: Model
  onSubmit: (input: ModelInput) => void
  submitLabel: string
  pending: boolean
  error: Error | null
}) {
  return (
    <FieldsForm
      idPrefix="model"
      fields={FIELDS}
      initial={{
        name: model?.name ?? '',
        description: model?.description ?? '',
        appearance: model?.appearance ?? '',
        preferredPoses: model?.preferredPoses ?? '',
        styling: model?.styling ?? '',
      }}
      {...rest}
    />
  )
}

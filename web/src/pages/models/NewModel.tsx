import { Link, useNavigate } from 'react-router'
import { useCreateModel } from '../../api/models'
import { ModelForm } from './ModelForm'

export function NewModel() {
  const create = useCreateModel()
  const navigate = useNavigate()

  return (
    <section className="max-w-xl space-y-6">
      <Link to="/models" className="text-sm text-stone-600 hover:underline">
        ← Models
      </Link>
      <h1 className="text-2xl font-semibold">New model</h1>
      <p className="text-sm text-stone-600">
        Use only synthetic people or photographs you hold the rights to.
      </p>
      <ModelForm
        submitLabel="Create model"
        pending={create.isPending}
        error={create.error}
        onSubmit={(input) => create.mutate(input, { onSuccess: (m) => void navigate(`/models/${m.id}`) })}
      />
    </section>
  )
}

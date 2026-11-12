import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import type { Model, ModelImageRole } from '../../api/client'
import {
  ROLE_LABELS,
  ROLES,
  useDeleteModel,
  useDeleteModelImage,
  useModel,
  useUpdateModel,
  useUploadModelImage,
} from '../../api/models'
import { ImageGroups } from '../../components/ImageGroups'
import { ImageUploader } from '../../components/ImageUploader'
import { ConfirmButton } from '../../components/ui/ConfirmButton'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { NotFound } from '../NotFound'
import { guessRole } from './guessRole'
import { ModelForm } from './ModelForm'

const ROLE_OPTIONS = ROLES.map((r) => ({ value: r, label: ROLE_LABELS[r] }))

export function ModelDetail() {
  const id = Number(useParams().id)
  const { data: model, error, isPending } = useModel(id)

  if (!Number.isInteger(id) || id <= 0) return <NotFound />
  if (isPending) return <p className="text-stone-500">Loading…</p>
  if (error) {
    if ('status' in error && error.status === 404) return <NotFound />
    return <ErrorMessage error={error} />
  }

  return (
    <section className="space-y-8">
      <div>
        <Link to="/models" className="text-sm text-stone-600 hover:underline">
          ← Models
        </Link>
        <h1 className="mt-2 text-2xl font-semibold">{model.name}</h1>
      </div>

      {model.archived && <ArchivedBanner model={model} />}

      <div className="grid gap-8 lg:grid-cols-[2fr_1fr]">
        <div className="space-y-6">
          <h2 className="text-lg font-semibold">Reference images</h2>
          <Images model={model} />
          <Uploader modelId={model.id} />
        </div>
        <div className="space-y-6">
          <h2 className="text-lg font-semibold">Profile</h2>
          <EditModel key={model.updatedAt} model={model} />
          {!model.archived && <DeleteModel id={model.id} />}
        </div>
      </div>
    </section>
  )
}

function ArchivedBanner({ model }: { model: Model }) {
  const update = useUpdateModel(model.id)
  return (
    <div role="status" className="flex items-center justify-between gap-4 rounded border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900">
      <p>This model is archived. It has been used, so it is kept for traceability but cannot be chosen for new photoshoots.</p>
      <button
        type="button"
        onClick={() => update.mutate({ archived: false })}
        disabled={update.isPending}
        className="shrink-0 rounded border border-amber-300 bg-white px-3 py-1 font-medium"
      >
        Restore
      </button>
    </div>
  )
}

function EditModel({ model }: { model: Model }) {
  const update = useUpdateModel(model.id)
  return (
    <ModelForm
      model={model}
      submitLabel="Save changes"
      pending={update.isPending}
      error={update.error}
      onSubmit={(input) => update.mutate(input)}
    />
  )
}

function Uploader({ modelId }: { modelId: number }) {
  const upload = useUploadModelImage(modelId)
  return (
    <ImageUploader
      options={ROLE_OPTIONS}
      labelName="Role"
      guess={guessRole}
      upload={(file, role) => upload.mutateAsync({ file, role: role as ModelImageRole })}
    />
  )
}

function Images({ model }: { model: Model }) {
  const del = useDeleteModelImage(model.id)
  const images = model.images ?? []
  if (images.length === 0) {
    return <p className="text-stone-600">No images yet. Upload at least one face close-up (identity) and one full-body photograph.</p>
  }
  return (
    <div className="space-y-4">
      <ErrorMessage error={del.error} />
      <ImageGroups
        images={images.map((i) => ({ ...i, group: i.role }))}
        groups={ROLE_OPTIONS}
        onDelete={(imageId) => del.mutate(imageId)}
        deleting={del.isPending}
        testId="model-image"
      />
    </div>
  )
}

function DeleteModel({ id }: { id: number }) {
  const del = useDeleteModel(id)
  const navigate = useNavigate()
  const [archivedNotice, setArchivedNotice] = useState(false)
  return (
    <div className="space-y-2 border-t border-stone-200 pt-4">
      <ErrorMessage error={del.error} />
      {archivedNotice && (
        <p role="status" className="text-sm text-stone-600">
          This model has been used, so it was archived instead of deleted.
        </p>
      )}
      <ConfirmButton
        confirmLabel="Delete model"
        disabled={del.isPending}
        onConfirm={() =>
          del.mutate(undefined, {
            onSuccess: ({ archived }) => (archived ? setArchivedNotice(true) : void navigate('/models')),
          })
        }
      >
        Delete model
      </ConfirmButton>
    </div>
  )
}

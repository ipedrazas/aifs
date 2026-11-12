import { useState } from 'react'
import { Link } from 'react-router'
import { blobUrl } from '../../api/client'
import { useModels } from '../../api/models'
import { ErrorMessage } from '../../components/ui/ErrorMessage'

export function ModelList() {
  const [showArchived, setShowArchived] = useState(false)
  const { data: models, error, isPending } = useModels(showArchived)

  return (
    <section className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold">Models</h1>
        <div className="flex items-center gap-4">
          <label className="flex items-center gap-2 text-sm text-stone-600">
            <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} />
            Show archived
          </label>
          <Link to="/models/new" className="rounded bg-stone-900 px-4 py-2 text-sm font-medium text-white">
            New model
          </Link>
        </div>
      </div>

      <ErrorMessage error={error} />
      {isPending && <p className="text-stone-500">Loading…</p>}
      {models && models.length === 0 && (
        <p className="text-stone-600">No models yet. Create one and upload a face close-up and a full-body photograph.</p>
      )}

      {models && models.length > 0 && (
        <ul className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4">
          {models.map((m) => (
            <li key={m.id}>
              <Link
                to={`/models/${m.id}`}
                className={`block overflow-hidden rounded-lg border border-stone-200 bg-white hover:border-stone-400 ${m.archived ? 'opacity-60' : ''}`}
              >
                <div className="flex aspect-[3/4] items-center justify-center bg-stone-100">
                  {m.cover ? (
                    <img src={blobUrl(m.cover.hash, 512)} alt="" loading="lazy" className="h-full w-full object-cover" />
                  ) : (
                    <span className="text-sm text-stone-400">No images</span>
                  )}
                </div>
                <div className="space-y-0.5 p-3">
                  <p className="font-medium">
                    {m.name}
                    {m.archived && <span className="ml-2 text-xs font-normal text-stone-500">Archived</span>}
                  </p>
                  <p className="text-xs text-stone-500">
                    {m.imageCount} image{m.imageCount === 1 ? '' : 's'}
                  </p>
                </div>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

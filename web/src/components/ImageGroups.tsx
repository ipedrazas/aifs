import { blobUrl } from '../api/client'
import { ConfirmButton } from './ui/ConfirmButton'

export type GroupedImage = {
  id: number
  hash: string
  filename: string
  width: number
  height: number
  group: string
}

/**
 * Reference images grouped (by view or role) in a fixed order. Thumbnails are
 * not cropped; clicking one opens the original.
 */
export function ImageGroups({
  images,
  groups,
  onDelete,
  deleting,
  testId,
}: {
  images: GroupedImage[]
  groups: { value: string; label: string }[]
  onDelete: (id: number) => void
  deleting: boolean
  testId: string
}) {
  return (
    <div className="space-y-4">
      {groups
        .filter((g) => images.some((i) => i.group === g.value))
        .map((g) => (
          <div key={g.value} className="space-y-2">
            <h3 className="text-sm font-medium text-stone-700">{g.label}</h3>
            <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3">
              {images
                .filter((i) => i.group === g.value)
                .map((im) => (
                  <li key={im.id} className="overflow-hidden rounded border border-stone-200 bg-white" data-testid={testId}>
                    <a href={blobUrl(im.hash)} target="_blank" rel="noreferrer" title="Open the original">
                      <img
                        src={blobUrl(im.hash, 512)}
                        alt={`${g.label}: ${im.filename}`}
                        loading="lazy"
                        className="aspect-[3/4] w-full bg-stone-100 object-contain"
                      />
                    </a>
                    <div className="flex items-center justify-between gap-2 p-2">
                      <p className="min-w-0 truncate text-xs text-stone-500" title={im.filename}>
                        {im.width}×{im.height}
                        {im.filename ? ` · ${im.filename}` : ''}
                      </p>
                      <ConfirmButton onConfirm={() => onDelete(im.id)} disabled={deleting} confirmLabel="Delete">
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

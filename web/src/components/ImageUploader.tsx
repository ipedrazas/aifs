import { useEffect, useId, useRef, useState, type DragEvent } from 'react'

const ACCEPT = ['image/jpeg', 'image/png', 'image/webp']

export type LabelOption = { value: string; label: string }

type Item = {
  key: string
  file: File
  label: string
  preview: string
  status: 'ready' | 'uploading' | 'failed'
  error?: string
}

let counter = 0

/**
 * Queue photographs, label each one (view or role), then upload them one by
 * one. Failed uploads stay in the queue with the server's reason.
 */
export function ImageUploader({
  options,
  labelName,
  guess,
  upload,
}: {
  options: LabelOption[]
  /** What the label is called, e.g. "View" or "Role". */
  labelName: string
  /** Initial label for a file, from its name. */
  guess: (filename: string) => string
  upload: (file: File, label: string) => Promise<unknown>
}) {
  const [items, setItems] = useState<Item[]>([])
  const [dragging, setDragging] = useState(false)
  const inputId = useId()
  const inputRef = useRef<HTMLInputElement>(null)
  const previews = useRef(new Set<string>())

  // Release object URLs when the component goes away.
  useEffect(() => {
    const urls = previews.current
    return () => urls.forEach((u) => URL.revokeObjectURL(u))
  }, [])

  function add(files: FileList | File[]) {
    const next: Item[] = []
    for (const file of Array.from(files)) {
      const preview = URL.createObjectURL(file)
      previews.current.add(preview)
      next.push({
        key: `f${++counter}`,
        file,
        label: guess(file.name),
        preview,
        status: 'ready',
        error: ACCEPT.includes(file.type) ? undefined : 'Only JPEG, PNG and WebP photographs are accepted.',
      })
    }
    setItems((cur) => [...cur, ...next])
  }

  function remove(key: string) {
    setItems((cur) => {
      const item = cur.find((i) => i.key === key)
      if (item) {
        URL.revokeObjectURL(item.preview)
        previews.current.delete(item.preview)
      }
      return cur.filter((i) => i.key !== key)
    })
  }

  function patch(key: string, change: Partial<Item>) {
    setItems((cur) => cur.map((i) => (i.key === key ? { ...i, ...change } : i)))
  }

  async function uploadAll() {
    for (const item of items) {
      if (item.status === 'uploading') continue
      patch(item.key, { status: 'uploading', error: undefined })
      try {
        await upload(item.file, item.label)
        remove(item.key)
      } catch (err) {
        patch(item.key, { status: 'failed', error: (err as Error).message })
      }
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    setDragging(false)
    if (e.dataTransfer.files.length) add(e.dataTransfer.files)
  }

  const busy = items.some((i) => i.status === 'uploading')

  return (
    <div className="space-y-3">
      <div
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        className={`rounded-lg border-2 border-dashed p-6 text-center ${dragging ? 'border-stone-500 bg-stone-100' : 'border-stone-300'}`}
      >
        <label htmlFor={inputId} className="cursor-pointer text-stone-700">
          <span className="font-medium underline">Choose photographs</span> or drop them here
        </label>
        <input
          ref={inputRef}
          id={inputId}
          type="file"
          accept={ACCEPT.join(',')}
          multiple
          className="sr-only"
          onChange={(e) => {
            if (e.target.files) add(e.target.files)
            e.target.value = ''
          }}
        />
        <p className="mt-1 text-xs text-stone-500">JPEG, PNG or WebP. Originals are stored unchanged.</p>
      </div>

      {items.length > 0 && (
        <ul className="space-y-2" aria-label="Photographs to upload">
          {items.map((item) => (
            <li key={item.key} className="flex items-center gap-3 rounded border border-stone-200 bg-white p-2">
              <img src={item.preview} alt="" className="h-14 w-14 rounded object-cover" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm">{item.file.name}</p>
                {item.error && <p className="text-xs text-red-700">{item.error}</p>}
              </div>
              <label className="text-sm">
                <span className="sr-only">
                  {labelName} for {item.file.name}
                </span>
                <select
                  value={item.label}
                  disabled={item.status === 'uploading'}
                  onChange={(e) => patch(item.key, { label: e.target.value })}
                  className="rounded border border-stone-300 px-2 py-1"
                >
                  {options.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              </label>
              <button
                type="button"
                onClick={() => remove(item.key)}
                disabled={item.status === 'uploading'}
                className="text-sm text-stone-500 hover:text-stone-900"
              >
                Remove
              </button>
            </li>
          ))}
        </ul>
      )}

      {items.length > 0 && (
        <button
          type="button"
          onClick={() => void uploadAll()}
          disabled={busy}
          className="rounded bg-stone-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
        >
          {busy ? 'Uploading…' : `Upload ${items.length} photograph${items.length === 1 ? '' : 's'}`}
        </button>
      )}
    </div>
  )
}

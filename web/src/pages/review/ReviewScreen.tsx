import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
import { blobUrl, type ImageDetail, type RejectionReason, type ReviewDecision } from '../../api/client'
import { useProduct, VIEW_LABELS } from '../../api/products'
import { useImage, useImages, useReviewImage } from '../../api/studio'
import { StatusBadge } from '../../components/StatusBadge'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { ComparisonViewer } from '../../components/viewer/ComparisonViewer'
import { INITIAL_VIEW, pan, zoomAt, type CompareMode } from '../../components/viewer/viewMath'
import { formatDate } from '../../lib/format'
import { NotFound } from '../NotFound'
import { filterFromParams } from './filters'

const REASONS: { value: RejectionReason; label: string; key: string }[] = [
  { value: 'fidelity', label: 'Product fidelity', key: '1' },
  { value: 'photo_quality', label: 'Photographic quality', key: '2' },
  { value: 'other', label: 'Other', key: '3' },
]

const MODES: { value: CompareMode; label: string }[] = [
  { value: 'side', label: 'Side by side' },
  { value: 'swipe', label: 'Swipe' },
  { value: 'overlay', label: 'Overlay' },
]

const SHORTCUTS: [string, string][] = [
  ['A', 'Approve'],
  ['R then 1 / 2 / 3', 'Reject: fidelity / photo quality / other'],
  ['F', 'Needs refinement'],
  ['N', 'Write notes (Esc to leave)'],
  ['J or →', 'Next image'],
  ['K or ←', 'Previous image'],
  ['[ and ]', 'Previous / next reference photograph'],
  ['M', 'Change comparison mode'],
  ['+ / − / 0', 'Zoom in / out / reset'],
  ['Shift + arrows', 'Pan'],
  ['?', 'Show or hide shortcuts'],
]

/**
 * Reviews a queue of images: the filter in the URL selects the queue, which
 * is fixed when the screen opens so it does not shift as decisions change
 * what matches the filter.
 */
export function ReviewScreen() {
  const id = Number(useParams().id)
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const list = useImages(filterFromParams(params))
  const [queue, setQueue] = useState<number[] | null>(null)

  // Freeze the queue the first time the list arrives (state adjusted during
  // render, as React recommends for derived-once values).
  if (queue === null && list.data) {
    const ids = list.data.map((im) => im.id)
    setQueue(ids.includes(id) ? ids : [id, ...ids])
  }

  const { data: image, error, isPending } = useImage(id)

  if (!Number.isInteger(id) || id <= 0) return <NotFound />
  if (isPending) return <p className="text-stone-500">Loading…</p>
  if (error) {
    if ('status' in error && error.status === 404) return <NotFound />
    return <ErrorMessage error={error} />
  }

  const ids = queue ?? [id]
  const pos = ids.indexOf(id)
  const go = (to: number | undefined) => to !== undefined && navigate(`/review/${to}?${params.toString()}`)

  return (
    <Workspace
      key={id}
      image={image}
      position={pos}
      total={ids.length}
      galleryHref={`/review?${params.toString()}`}
      onPrev={pos > 0 ? () => go(ids[pos - 1]) : undefined}
      onNext={pos < ids.length - 1 ? () => go(ids[pos + 1]) : undefined}
    />
  )
}

type Ref = { hash: string; label: string }

function Workspace({
  image,
  position,
  total,
  galleryHref,
  onPrev,
  onNext,
}: {
  image: ImageDetail
  position: number
  total: number
  galleryHref: string
  onPrev?: () => void
  onNext?: () => void
}) {
  const product = useProduct(image.settings.product.id)
  const review = useReviewImage()

  // Reference photographs: those sent with the generation first, then any
  // other current photograph of the product.
  const refs: Ref[] = []
  const seen = new Set<string>()
  for (const input of image.settings.inputs) {
    if (input.role !== 'garment' || seen.has(input.hash)) continue
    seen.add(input.hash)
    refs.push({ hash: input.hash, label: `${VIEW_LABELS[input.label as keyof typeof VIEW_LABELS] ?? input.label} (sent)` })
  }
  for (const pi of product.data?.images ?? []) {
    if (seen.has(pi.hash)) continue
    seen.add(pi.hash)
    refs.push({ hash: pi.hash, label: VIEW_LABELS[pi.view] })
  }
  for (const input of image.settings.inputs) {
    if (input.role !== 'model' || seen.has(input.hash)) continue
    seen.add(input.hash)
    refs.push({ hash: input.hash, label: `Model: ${input.label === 'identity' ? 'identity' : 'full body'}` })
  }

  const [refIndex, setRefIndex] = useState(0)
  const [mode, setMode] = useState<CompareMode>('side')
  const [view, setView] = useState(INITIAL_VIEW)
  const [swipe, setSwipe] = useState(0.5)
  const [opacity, setOpacity] = useState(0.5)
  const [notes, setNotes] = useState(image.reviewerNotes)
  const [rejecting, setRejecting] = useState(false)
  const [help, setHelp] = useState(false)
  const notesRef = useRef<HTMLTextAreaElement>(null)
  const viewerRef = useRef<HTMLDivElement>(null)

  const ref = refs[Math.min(refIndex, Math.max(0, refs.length - 1))]

  function decide(status: ReviewDecision, reason?: RejectionReason) {
    if (review.isPending) return
    setRejecting(false)
    review.mutate({ id: image.id, status, reason, notes }, { onSuccess: () => onNext?.() })
  }

  // Keyboard shortcuts. Read the latest state through a ref so the listener
  // is registered once.
  const actions = useRef({ decide, onPrev, onNext, rejecting, refs, mode })
  useEffect(() => {
    actions.current = { decide, onPrev, onNext, rejecting, refs, mode }
  })
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const t = e.target as HTMLElement
      if (t.tagName === 'TEXTAREA' || t.tagName === 'INPUT' || t.tagName === 'SELECT') {
        if (e.key === 'Escape') t.blur()
        return
      }
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const a = actions.current
      const center = () => {
        const r = viewerRef.current?.querySelector('[data-pane]')?.getBoundingClientRect()
        return r ? [r.width / 2, r.height / 2] : [0, 0]
      }
      if (a.rejecting) {
        const reason = REASONS.find((r) => r.key === e.key)
        if (reason) a.decide('rejected', reason.value)
        else if (e.key === 'Escape') setRejecting(false)
        e.preventDefault()
        return
      }
      if (e.shiftKey && e.key.startsWith('Arrow')) {
        const step = 60
        const d = { ArrowLeft: [step, 0], ArrowRight: [-step, 0], ArrowUp: [0, step], ArrowDown: [0, -step] }[e.key]
        if (d) setView((v) => pan(v, d[0], d[1]))
        e.preventDefault()
        return
      }
      switch (e.key) {
        case 'a':
          a.decide('approved')
          break
        case 'f':
          a.decide('needs_refinement')
          break
        case 'r':
          setRejecting(true)
          break
        case 'n':
          notesRef.current?.focus()
          break
        case 'j':
        case 'ArrowRight':
          a.onNext?.()
          break
        case 'k':
        case 'ArrowLeft':
          a.onPrev?.()
          break
        case ']':
          setRefIndex((i) => (a.refs.length ? (i + 1) % a.refs.length : 0))
          break
        case '[':
          setRefIndex((i) => (a.refs.length ? (i - 1 + a.refs.length) % a.refs.length : 0))
          break
        case 'm':
          setMode(MODES[(MODES.findIndex((m) => m.value === a.mode) + 1) % MODES.length].value)
          break
        case '+':
        case '=': {
          const [x, y] = center()
          setView((v) => zoomAt(v, 1.5, x, y))
          break
        }
        case '-': {
          const [x, y] = center()
          setView((v) => zoomAt(v, 1 / 1.5, x, y))
          break
        }
        case '0':
          setView(INITIAL_VIEW)
          break
        case '?':
          setHelp((h) => !h)
          break
        default:
          return
      }
      e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const btn = 'rounded px-3 py-2 text-sm font-medium disabled:opacity-50'

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <Link to={galleryHref} className="text-sm text-stone-600 hover:underline">
            ← Gallery
          </Link>
          <span className="text-sm text-stone-500" data-testid="queue-position">
            {position + 1} of {total}
          </span>
          <StatusBadge status={image.reviewStatus} />
        </div>
        <div className="flex items-center gap-2 text-sm">
          <button type="button" onClick={onPrev} disabled={!onPrev} className="rounded border border-stone-300 px-2 py-1 disabled:opacity-40">
            ← Previous
          </button>
          <button type="button" onClick={onNext} disabled={!onNext} className="rounded border border-stone-300 px-2 py-1 disabled:opacity-40">
            Next →
          </button>
          <button type="button" onClick={() => setHelp((h) => !h)} className="rounded border border-stone-300 px-2 py-1" aria-expanded={help}>
            Shortcuts (?)
          </button>
        </div>
      </div>

      {help && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-1 rounded border border-stone-200 bg-white p-4 text-sm" aria-label="Keyboard shortcuts">
          {SHORTCUTS.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="font-mono text-stone-700">{k}</dt>
              <dd className="text-stone-600">{v}</dd>
            </div>
          ))}
        </dl>
      )}

      <div className="grid gap-4 xl:grid-cols-[1fr_20rem]">
        <div className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex gap-1" role="group" aria-label="Comparison mode">
              {MODES.map((m) => (
                <button
                  key={m.value}
                  type="button"
                  aria-pressed={mode === m.value}
                  onClick={() => setMode(m.value)}
                  className={`rounded px-2 py-1 text-sm ${mode === m.value ? 'bg-stone-900 text-white' : 'border border-stone-300'}`}
                >
                  {m.label}
                </button>
              ))}
            </div>
            <div className="flex items-center gap-2 text-sm text-stone-600">
              <span>{Math.round(view.scale * 100)}%</span>
              <button type="button" onClick={() => setView(INITIAL_VIEW)} className="rounded border border-stone-300 px-2 py-1">
                Reset zoom
              </button>
            </div>
          </div>

          <div ref={viewerRef}>
            <ComparisonViewer
              generated={{ src: blobUrl(image.hash), alt: `Generated image ${image.id}` }}
              reference={ref ? { src: blobUrl(ref.hash), alt: `Reference: ${ref.label}` } : null}
              mode={mode}
              view={view}
              onViewChange={setView}
              swipe={swipe}
              onSwipeChange={setSwipe}
              opacity={opacity}
              onOpacityChange={setOpacity}
            />
          </div>

          <ul className="flex gap-2 overflow-x-auto pb-1" aria-label="Reference photographs">
            {refs.map((r, i) => (
              <li key={r.hash} className="shrink-0">
                <button
                  type="button"
                  onClick={() => setRefIndex(i)}
                  aria-pressed={i === refIndex}
                  className={`block w-20 overflow-hidden rounded border-2 ${i === refIndex ? 'border-stone-900' : 'border-transparent'}`}
                  title={r.label}
                >
                  <img src={blobUrl(r.hash, 256)} alt={r.label} className="aspect-square w-full bg-stone-100 object-contain" />
                  <span className="block truncate px-1 text-[10px] text-stone-600">{r.label}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>

        <aside className="space-y-4">
          <div className="space-y-1 text-sm">
            <p className="font-medium">{image.product.name}</p>
            <p className="text-stone-500">
              {image.model.name} · {image.recipe} · {image.provider}
            </p>
            <Link to={`/images/${image.id}`} className="text-xs text-stone-500 underline">
              Full provenance
            </Link>
          </div>

          <div className="space-y-2">
            <label htmlFor="review-notes" className="block text-sm font-medium text-stone-700">
              Notes <span className="font-normal text-stone-400">(N)</span>
            </label>
            <textarea
              id="review-notes"
              ref={notesRef}
              rows={3}
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder="What is wrong or right with this image?"
              className="w-full rounded border border-stone-300 px-3 py-2 text-sm focus:border-stone-500 focus:outline-none"
            />
          </div>

          <div className="space-y-2">
            <button type="button" onClick={() => decide('approved')} disabled={review.isPending} className={`${btn} w-full bg-emerald-700 text-white`}>
              Approve (A)
            </button>
            {!rejecting ? (
              <button type="button" onClick={() => setRejecting(true)} disabled={review.isPending} className={`${btn} w-full bg-red-700 text-white`}>
                Reject… (R)
              </button>
            ) : (
              <div className="space-y-1 rounded border border-red-200 bg-red-50 p-2" role="group" aria-label="Rejection reason">
                <p className="text-xs text-red-900">Why is it rejected?</p>
                {REASONS.map((r) => (
                  <button
                    key={r.value}
                    type="button"
                    onClick={() => decide('rejected', r.value)}
                    disabled={review.isPending}
                    className={`${btn} w-full border border-red-300 bg-white text-left text-red-900`}
                  >
                    {r.key}. {r.label}
                  </button>
                ))}
                <button type="button" onClick={() => setRejecting(false)} className="text-xs text-stone-600">
                  Cancel (Esc)
                </button>
              </div>
            )}
            <button type="button" onClick={() => decide('needs_refinement')} disabled={review.isPending} className={`${btn} w-full bg-violet-700 text-white`}>
              Needs refinement (F)
            </button>
            <ErrorMessage error={review.error} />
            {!onNext && review.isSuccess && <p className="text-sm text-stone-600">End of the queue.</p>}
          </div>

          <div className="space-y-2">
            <h2 className="text-sm font-medium text-stone-700">History</h2>
            {image.reviewHistory.length === 0 ? (
              <p className="text-xs text-stone-500">Not reviewed yet.</p>
            ) : (
              <ol className="space-y-2" aria-label="Review history">
                {[...image.reviewHistory].reverse().map((ev) => (
                  <li key={ev.id} className="text-xs">
                    <div className="flex items-center gap-2">
                      <StatusBadge status={ev.status} />
                      {ev.rejectionReason && <span className="text-stone-600">{REASONS.find((r) => r.value === ev.rejectionReason)?.label}</span>}
                    </div>
                    <p className="text-stone-400">{formatDate(ev.createdAt)}</p>
                    {ev.notes && <p className="text-stone-600">{ev.notes}</p>}
                  </li>
                ))}
              </ol>
            )}
          </div>
        </aside>
      </div>
    </section>
  )
}

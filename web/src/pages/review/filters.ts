import type { ReviewStatus } from '../../api/client'
import type { ImageFilter } from '../../api/studio'

export const STATUS_OPTIONS: { value: ReviewStatus | ''; label: string }[] = [
  { value: 'pending', label: 'Pending review' },
  { value: 'approved', label: 'Approved' },
  { value: 'rejected', label: 'Rejected' },
  { value: 'needs_refinement', label: 'Needs refinement' },
  { value: '', label: 'All' },
]

const STATUSES = new Set(['pending', 'approved', 'rejected', 'needs_refinement'])

/** Reads the gallery filter from URL search params. Status defaults to pending. */
export function filterFromParams(params: URLSearchParams): ImageFilter {
  const num = (k: string) => {
    const n = Number(params.get(k))
    return Number.isInteger(n) && n > 0 ? n : undefined
  }
  const raw = params.get('status')
  const status = raw === null ? 'pending' : STATUSES.has(raw) ? (raw as ReviewStatus) : undefined
  return { productId: num('productId'), photoshootId: num('photoshootId'), generationId: num('generationId'), status }
}

/** The inverse of filterFromParams; "all statuses" is an explicit empty value. */
export function paramsFromFilter(f: ImageFilter): URLSearchParams {
  const p = new URLSearchParams()
  if (f.productId) p.set('productId', String(f.productId))
  if (f.photoshootId) p.set('photoshootId', String(f.photoshootId))
  if (f.generationId) p.set('generationId', String(f.generationId))
  p.set('status', f.status ?? '')
  return p
}

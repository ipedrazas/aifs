const STYLES: Record<string, string> = {
  queued: 'bg-stone-100 text-stone-700',
  running: 'bg-sky-100 text-sky-800',
  succeeded: 'bg-emerald-100 text-emerald-800',
  failed: 'bg-red-100 text-red-800',
  cancelled: 'bg-stone-200 text-stone-600',
  pending: 'bg-amber-100 text-amber-800',
  approved: 'bg-emerald-100 text-emerald-800',
  rejected: 'bg-red-100 text-red-800',
  needs_refinement: 'bg-violet-100 text-violet-800',
}

const LABELS: Record<string, string> = {
  needs_refinement: 'needs refinement',
  pending: 'pending review',
}

export function StatusBadge({ status }: { status: string }) {
  return (
    <span className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${STYLES[status] ?? STYLES.queued}`}>
      {LABELS[status] ?? status}
    </span>
  )
}

/** Formats a cost in millionths of a US dollar. */
export function formatCost(micros: number): string {
  const dollars = micros / 1_000_000
  if (dollars === 0) return '$0.00'
  if (dollars < 0.01) return `$${dollars.toFixed(4)}`
  return `$${dollars.toFixed(2)}`
}

/** Formats the time between two ISO timestamps, e.g. "1m 05s". */
export function formatDuration(from?: string | null, to?: string | null): string {
  if (!from) return '–'
  const ms = (to ? Date.parse(to) : Date.now()) - Date.parse(from)
  if (!Number.isFinite(ms) || ms < 0) return '–'
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  return `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

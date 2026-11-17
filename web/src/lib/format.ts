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

/** Formats a 0–1 rate as a percentage with one decimal, or "–". */
export function formatPercent(v: number | null | undefined): string {
  return v == null ? '–' : `${Math.round(v * 1000) / 10}%`
}

/** Formats a number of seconds compactly, or "–". */
export function formatSeconds(v: number | null | undefined): string {
  if (v == null) return '–'
  const s = Math.round(v)
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`
  const h = Math.floor(s / 3600)
  return h < 48 ? `${h}h ${String(Math.floor((s % 3600) / 60)).padStart(2, '0')}m` : `${Math.round(h / 24)}d`
}

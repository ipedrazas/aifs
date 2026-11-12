export type ViewState = { scale: number; x: number; y: number }
export type CompareMode = 'side' | 'swipe' | 'overlay'

export const INITIAL_VIEW: ViewState = { scale: 1, x: 0, y: 0 }
export const MIN_SCALE = 1
export const MAX_SCALE = 16

/** Zooms by factor around a point given in pane coordinates. */
export function zoomAt(v: ViewState, factor: number, px: number, py: number): ViewState {
  const scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, v.scale * factor))
  if (scale === MIN_SCALE) return INITIAL_VIEW
  const k = scale / v.scale
  return { scale, x: px - (px - v.x) * k, y: py - (py - v.y) * k }
}

/** Moves the view; at 1x there is nothing to pan. */
export function pan(v: ViewState, dx: number, dy: number): ViewState {
  if (v.scale === 1) return v
  return { ...v, x: v.x + dx, y: v.y + dy }
}

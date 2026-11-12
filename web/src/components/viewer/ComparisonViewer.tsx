import { useEffect, useRef, type CSSProperties, type PointerEvent, type ReactNode } from 'react'
import { pan, zoomAt, type CompareMode, type ViewState } from './viewMath'

type Img = { src: string; alt: string }

/**
 * Compares two images with one shared zoom and pan, so the same region of
 * both is always in view. Both images are fitted ("contain") into panes of
 * equal size, and the same transform is applied to each. Scroll to zoom
 * around the cursor, drag to pan.
 */
export function ComparisonViewer({
  generated,
  reference,
  mode,
  view,
  onViewChange,
  swipe,
  onSwipeChange,
  opacity,
  onOpacityChange,
}: {
  generated: Img
  reference: Img | null
  mode: CompareMode
  view: ViewState
  onViewChange: (v: ViewState) => void
  /** Swipe divider position, 0–1. */
  swipe: number
  onSwipeChange: (v: number) => void
  /** Reference opacity in overlay mode, 0–1. */
  opacity: number
  onOpacityChange: (v: number) => void
}) {
  const root = useRef<HTMLDivElement>(null)
  const drag = useRef<{ x: number; y: number } | null>(null)
  // Latest values for the native wheel listener.
  const latest = useRef({ view, onViewChange })
  useEffect(() => {
    latest.current = { view, onViewChange }
  })

  // React registers wheel listeners as passive; zooming must stop the page
  // from scrolling, so listen natively.
  useEffect(() => {
    const el = root.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      const pane = (e.target as HTMLElement).closest('[data-pane]')
      if (!pane) return
      e.preventDefault()
      const r = pane.getBoundingClientRect()
      const { view: v, onViewChange: set } = latest.current
      set(zoomAt(v, e.deltaY < 0 ? 1.15 : 1 / 1.15, e.clientX - r.left, e.clientY - r.top))
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [])

  function onPointerDown(e: PointerEvent<HTMLDivElement>) {
    drag.current = { x: e.clientX, y: e.clientY }
    e.currentTarget.setPointerCapture(e.pointerId)
  }
  function onPointerMove(e: PointerEvent<HTMLDivElement>) {
    if (!drag.current) return
    const dx = e.clientX - drag.current.x
    const dy = e.clientY - drag.current.y
    drag.current = { x: e.clientX, y: e.clientY }
    onViewChange(pan(view, dx, dy))
  }
  function onPointerUp() {
    drag.current = null
  }

  const layer = (img: Img, style?: CSSProperties) => (
    <div className="absolute inset-0" style={style}>
      <div
        className="absolute inset-0 origin-top-left"
        style={{ transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }}
      >
        <img src={img.src} alt={img.alt} draggable={false} className="h-full w-full select-none object-contain" />
      </div>
    </div>
  )

  const pane = (children: ReactNode, label: string, testId: string) => (
    <div
      data-pane
      data-testid={testId}
      aria-label={label}
      role="img"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerUp}
      className="relative h-[70vh] min-h-80 flex-1 cursor-grab touch-none overflow-hidden rounded border border-stone-200 bg-stone-100 active:cursor-grabbing"
    >
      {children}
    </div>
  )

  let body: ReactNode
  if (mode === 'side' || !reference) {
    body = (
      <div className="flex gap-2">
        {pane(layer(generated), 'Generated image', 'pane-generated')}
        {reference && pane(layer(reference), 'Reference photograph', 'pane-reference')}
      </div>
    )
  } else if (mode === 'swipe') {
    body = (
      <div className="space-y-2">
        {pane(
          <>
            {layer(reference)}
            {layer(generated, { clipPath: `inset(0 ${(1 - swipe) * 100}% 0 0)` })}
            <div className="pointer-events-none absolute inset-y-0 w-0.5 bg-white shadow" style={{ left: `${swipe * 100}%` }} />
          </>,
          'Swipe comparison',
          'pane-swipe',
        )}
        <label className="flex items-center gap-3 text-xs text-stone-600">
          Generated ← → Reference
          <input type="range" min={0} max={1} step={0.01} value={swipe} onChange={(e) => onSwipeChange(Number(e.target.value))} className="flex-1" aria-label="Swipe position" />
        </label>
      </div>
    )
  } else {
    body = (
      <div className="space-y-2">
        {pane(
          <>
            {layer(generated)}
            {layer(reference, { opacity })}
          </>,
          'Overlay comparison',
          'pane-overlay',
        )}
        <label className="flex items-center gap-3 text-xs text-stone-600">
          Reference opacity
          <input type="range" min={0} max={1} step={0.01} value={opacity} onChange={(e) => onOpacityChange(Number(e.target.value))} className="flex-1" aria-label="Reference opacity" />
        </label>
      </div>
    )
  }

  return <div ref={root}>{body}</div>
}

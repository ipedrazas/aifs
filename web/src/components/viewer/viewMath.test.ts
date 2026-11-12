import { describe, expect, it } from 'vitest'
import { INITIAL_VIEW, MAX_SCALE, pan, zoomAt } from './viewMath'

describe('zoomAt', () => {
  it('keeps the point under the cursor fixed', () => {
    const v = zoomAt(INITIAL_VIEW, 2, 100, 50)
    expect(v.scale).toBe(2)
    // A content point at (100, 50) maps to 100*2 + x = 100.
    expect(100 * v.scale + v.x).toBeCloseTo(100)
    expect(50 * v.scale + v.y).toBeCloseTo(50)
  })

  it('clamps to the allowed range and resets at 1x', () => {
    let v = INITIAL_VIEW
    for (let i = 0; i < 50; i++) v = zoomAt(v, 2, 0, 0)
    expect(v.scale).toBe(MAX_SCALE)
    expect(zoomAt(zoomAt(INITIAL_VIEW, 2, 30, 30), 0.1, 0, 0)).toEqual(INITIAL_VIEW)
  })
})

describe('pan', () => {
  it('moves only when zoomed in', () => {
    expect(pan(INITIAL_VIEW, 10, 10)).toEqual(INITIAL_VIEW)
    expect(pan({ scale: 2, x: 0, y: 0 }, 10, -5)).toEqual({ scale: 2, x: 10, y: -5 })
  })
})

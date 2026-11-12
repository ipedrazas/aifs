import { describe, expect, it } from 'vitest'
import { formatCost, formatDuration } from './format'

describe('formatCost', () => {
  it.each([
    [0, '$0.00'],
    [10_000, '$0.01'],
    [1_234_567, '$1.23'],
    [500, '$0.0005'],
  ])('%d → %s', (micros, out) => expect(formatCost(micros)).toBe(out))
})

describe('formatDuration', () => {
  it('formats seconds and minutes', () => {
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:00:42Z')).toBe('42s')
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:01:05Z')).toBe('1m 05s')
    expect(formatDuration(null, null)).toBe('–')
  })
})

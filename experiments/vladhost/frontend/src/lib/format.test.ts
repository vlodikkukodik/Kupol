import { describe, expect, it } from 'vitest'
import { formatBytes } from './format'

describe('formatBytes', () => {
  it.each([
    [0, '0 Б'],
    [1023, '1023 Б'],
    [1024, '1 КБ'],
    [1536, '1.5 КБ'],
    [10 * 1024, '10 КБ'],
    [500 * 1024 * 1024, '500 МБ'],
    [3 * 1024 ** 3, '3 ГБ'],
  ])('%d → %s', (n, want) => expect(formatBytes(n)).toBe(want))
})

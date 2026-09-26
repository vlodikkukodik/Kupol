import { describe, expect, it } from 'vitest'
import { dayDate, labelIndexes, niceMax, percent, ticks } from './chart'

describe('niceMax', () => {
  it('округляет вверх до 1, 2, 2.5, 5 × 10^n', () => {
    const cases: [number, number][] = [[0, 1], [-5, 1], [1, 1], [1.2, 2], [3, 5], [7, 10], [11, 20], [23, 25], [26, 50], [99, 100], [101, 200], [1234, 2000], [2100, 2500], [4900, 5000]]
    for (const [v, want] of cases) expect(niceMax(v), String(v)).toBe(want)
  })
  it('не даёт NaN и Infinity', () => {
    expect(niceMax(NaN)).toBe(1)
    expect(niceMax(Infinity)).toBe(1)
  })
  it('граница никогда не меньше значения', () => {
    for (let v = 1; v < 5000; v += 7) expect(niceMax(v)).toBeGreaterThanOrEqual(v)
  })
})

describe('ticks', () => {
  it('ноль, середина, максимум', () => expect(ticks(50)).toEqual([0, 25, 50]))
})

describe('dayDate', () => {
  it('сутки остаются теми же независимо от часового пояса', () => {
    expect(dayDate('2026-09-20').getUTCDate()).toBe(20)
    expect(dayDate('2026-01-01').getUTCFullYear()).toBe(2026)
  })
})

describe('percent', () => {
  it('ноль при нулевом целом', () => expect(percent(5, 0)).toBe(0))
  it('целые проценты от 10, десятые ниже', () => {
    expect(percent(1, 3)).toBe(33)
    expect(percent(1, 50)).toBe(2)
    expect(percent(1, 300)).toBe(0.3)
    expect(percent(5, 5)).toBe(100)
  })
})

describe('labelIndexes', () => {
  it('мало столбцов — подписи у всех', () => expect(labelIndexes(4, 6)).toEqual([0, 1, 2, 3]))
  it('много столбцов — первый и последний обязательно, без повторов', () => {
    const idx = labelIndexes(90, 6)
    expect(idx).toHaveLength(6)
    expect(idx[0]).toBe(0)
    expect(idx.at(-1)).toBe(89)
    expect(new Set(idx).size).toBe(6)
  })
  it('пусто для нуля', () => expect(labelIndexes(0, 6)).toEqual([]))
})

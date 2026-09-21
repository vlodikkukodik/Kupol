import { describe, expect, it } from 'vitest'
import type { OutBlock } from '@/api/generated/documents'
import { CLASSIFICATION_LEVELS, REDACTION_MAX, REDACTION_MIN, archiveMark, archiveMarkText, classification, copyText, redactionWidth, sheetCount } from '@/lib/realism'

const para = (text: string): OutBlock => ({ id: 'p', type: 'paragraph', data: { text: [{ text }] } }) as OutBlock
const redacted: OutBlock = { type: 'redacted', data: { level: 4 } } as OutBlock

describe('гриф секретности', () => {
  it('по уровню от «Несекретно» до «Особой важности»; вне шкалы — ближайший край', () => {
    expect(classification(0)).toBe('Несекретно')
    expect(classification(3)).toBe('Секретно')
    expect(classification(5)).toBe('Особой важности')
    expect(classification(7)).toBe('Особой важности · только Директорат')
    expect(classification(-1)).toBe('Несекретно')
    expect(classification(99)).toBe(classification(CLASSIFICATION_LEVELS - 1))
    expect(classification(Number.NaN)).toBe('Несекретно')
    expect(new Set(Array.from({ length: CLASSIFICATION_LEVELS }, (_, n) => classification(n))).size).toBe(8)
  })
})

describe('архивный шифр', () => {
  it('фонд по типу, опись — год, дело — последнее число шифра', () => {
    const base = { composed: { year: 1979 }, blocks: [para('текст')] }
    expect(archiveMark({ ...base, type: 'object', code: 'О-041' })).toMatchObject({ fond: 1, inventory: 1979, file: 41 })
    expect(archiveMark({ ...base, type: 'order', code: 'ПРИКАЗ-1978-12' })).toMatchObject({ fond: 2, file: 12 })
    expect(archiveMark({ ...base, type: 'personnel', code: 'ЛД-0157' })).toMatchObject({ fond: 4, file: 157 })
    expect(archiveMark({ ...base, type: 'memo', code: 'МЕМО-5' })).toMatchObject({ fond: 8, file: 5 })
    expect(archiveMark({ ...base, type: 'нечто', code: 'ХХ' })).toMatchObject({ fond: 9, file: 0 })
    expect(archiveMarkText({ fond: 1, inventory: 1979, file: 41, sheets: 3 })).toBe('Фонд 1 · Опись 1979 · Дело 41 · Листов 3')
  })

  it('листы: минимум один; текст и разрывы страниц добавляют; закрытый блок — фиксированный вклад, а не его настоящий объём', () => {
    expect(sheetCount([])).toBe(1)
    expect(sheetCount([para('коротко')])).toBe(1)
    expect(sheetCount([para('я'.repeat(2800))])).toBe(2)
    expect(sheetCount([para('я'.repeat(5700))])).toBe(3)
    expect(sheetCount([{ type: 'page', data: {} } as OutBlock, { type: 'page', data: {} } as OutBlock])).toBe(3)
    // закрытые блоки: их число влияет на объём, но не скрытый текст — тот сервер не присылает
    expect(sheetCount([redacted, redacted, redacted])).toBe(1) // 3 × 300 < 2800
    expect(sheetCount(Array.from({ length: 10 }, () => redacted))).toBe(2)
  })

  it('экземпляр: номер или «б/н» (у Гражданина сервер номера не присылает)', () => {
    expect(copyText('0042')).toBe('экз. № 0042')
    expect(copyText('')).toBe('экз. б/н')
  })
})

describe('ширина зачернения', () => {
  it('в границах, детерминирована и не зависит от порядка вызовов', () => {
    const a = redactionWidth('О-041|3|12|4')
    expect(a).toBeGreaterThanOrEqual(REDACTION_MIN)
    expect(a).toBeLessThanOrEqual(REDACTION_MAX)
    expect(redactionWidth('О-041|3|12|4')).toBe(a)
  })

  it('разным местам — разная ширина: полосы выглядят живыми, а не одинаковыми', () => {
    const widths = new Set<number>()
    for (let i = 0; i < 60; i++) widths.add(redactionWidth(`О-041|${i}|${i * 7}|3`))
    expect(widths.size).toBeGreaterThanOrEqual(8)
    for (const w of widths) {
      expect(w).toBeGreaterThanOrEqual(REDACTION_MIN)
      expect(w).toBeLessThanOrEqual(REDACTION_MAX)
    }
  })

  it('ширина не зависит от длины скрытого текста: в её аргументах его просто нет', () => {
    // положение: шифр, номер фрагмента, длина видимого текста до него, уровень — скрытый текст сервер не присылает вовсе
    expect(redactionWidth('О-041|1|10|3')).toBe(redactionWidth('О-041|1|10|3'))
  })
})

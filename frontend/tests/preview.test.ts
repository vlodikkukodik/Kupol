import { describe, expect, it } from 'vitest'
import type { OutDocument } from '@/api/generated/documents'
import { describeRedactions, redactionStats } from '@/lib/preview'

const doc = (blocks: unknown[]) => ({ blocks }) as Pick<OutDocument, 'blocks'>

describe('подсчёт закрытого в предпросмотре', () => {
  it('считает закрытые блоки и фрагменты в любой глубине', () => {
    const stats = redactionStats(
      doc([
        { id: 'a', type: 'paragraph', data: { text: [{ text: 'открыто' }, { redacted: true, level: 3 }] } },
        { type: 'redacted', data: { level: 4 } },
        { id: 't', type: 'table', data: { columns: ['А'], rows: [[[{ redacted: true, level: 2 }]], [[{ text: 'x' }]]] } },
        { type: 'redacted', data: { level: 6 } },
        { id: 'm', type: 'memo', data: { body: [[{ redacted: true, level: 1 }, { redacted: true, level: 5 }]] } },
      ]),
    )
    expect(stats).toEqual({ blocks: 2, fragments: 4 })
  })

  it('пустой и отсутствующий документ — нули', () => {
    expect(redactionStats(doc([]))).toEqual({ blocks: 0, fragments: 0 })
    expect(redactionStats(null)).toEqual({ blocks: 0, fragments: 0 })
  })

  it('открытые данные без метки redacted ничего не добавляют', () => {
    expect(redactionStats(doc([{ id: 'p', type: 'paragraph', data: { text: [{ text: 'redacted' }], redacted: false } }]))).toEqual({ blocks: 0, fragments: 0 })
  })

  it('подпись со склонением', () => {
    expect(describeRedactions({ blocks: 0, fragments: 0 })).toBe('ничего не закрыто')
    expect(describeRedactions({ blocks: 1, fragments: 0 })).toBe('1 блок')
    expect(describeRedactions({ blocks: 2, fragments: 1 })).toBe('2 блока и 1 фрагмент')
    expect(describeRedactions({ blocks: 5, fragments: 3 })).toBe('5 блоков и 3 фрагмента')
    expect(describeRedactions({ blocks: 11, fragments: 21 })).toBe('11 блоков и 21 фрагмент')
    expect(describeRedactions({ blocks: 0, fragments: 12 })).toBe('12 фрагментов')
  })
})

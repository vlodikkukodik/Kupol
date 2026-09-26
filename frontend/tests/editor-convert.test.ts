import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import type { InputBlock } from '@/api/generated/documents'
import { block, canonical } from './helpers/blocks'
import { BLOCK_NODE_NAMES, BLOCK_TYPES, blocksToDoc, canonicalBlocks, docToBlocks, inlineToRuns, nodeNameOf, richToInline } from '@/editor/convert'

describe('блоки ⇄ дерево редактора', () => {
  it('каноническая форма проходит туда и обратно без единого изменения', () => {
    expect(docToBlocks(blocksToDoc(canonical))).toEqual(canonical)
  })

  it('покрыты все 17 видов блоков', () => {
    expect(new Set(canonical.map((b) => b.type))).toEqual(new Set(BLOCK_TYPES))
    expect(BLOCK_TYPES).toHaveLength(17)
    expect(BLOCK_NODE_NAMES).toHaveLength(18) // + «коробка» для неизвестных
  })

  it('идентификатор и уровень блока сохраняются; отсутствие уровня остаётся отсутствием', () => {
    const doc = blocksToDoc(canonical)
    const p = doc.content?.find((n) => n.type === 'paragraph')
    expect(p?.attrs).toMatchObject({ blockId: 'paragraph1', level: 2 })
    expect(docToBlocks(doc).find((b) => b.type === 'heading')).not.toHaveProperty('level')
  })

  it('уровень 0, заданный явно, не превращается в отсутствие', () => {
    const out = docToBlocks(blocksToDoc([block('paragraph', { text: [{ text: 'а' }] }, { level: 0 })]))
    expect(out[0]?.level).toBe(0)
  })

  it('идемпотентно: повторное преобразование ничего не меняет', () => {
    const once = canonicalBlocks(canonical)
    expect(canonicalBlocks(once)).toEqual(once)
  })

  it('пустой список блоков — один пустой абзац, который наружу не уходит', () => {
    const doc = blocksToDoc([])
    expect(doc.content).toHaveLength(1)
    expect(docToBlocks(doc)).toEqual([])
  })

  it('фикстура e2e со всеми видами блоков: приведение к канону устойчиво и ничего не теряет', () => {
    const pack = JSON.parse(readFileSync(resolve(import.meta.dirname, '../e2e/fixtures/pack.json'), 'utf8')) as {
      documents: { code: string; blocks: InputBlock[] }[]
    }
    const blocks = pack.documents.find((d) => d.code === 'О-9001')!.blocks
    const canon = canonicalBlocks(blocks)
    expect(canon.map((b) => b.id)).toEqual(blocks.map((b) => b.id)) // ни один блок не пропал
    expect(canon.map((b) => b.type)).toEqual(blocks.map((b) => b.type))
    expect(canon.map((b) => b.level)).toEqual(blocks.map((b) => b.level))
    expect(canonicalBlocks(canon)).toEqual(canon)
    // закрытые фрагменты на месте, с уровнями
    const intro = canon.find((b) => b.id === 'intro')!.data as { text: { text: string; level?: number }[] }
    expect(intro.text.find((r) => r.level === 3)?.text).toBe('СЕКРЕТ-ФРАГМЕНТ-УР3')
    // строки вместо массивов фрагментов стали массивами (как хранит сервер)
    const quote = canon.find((b) => b.id === 'cite')!.data as { text: unknown }
    expect(quote.text).toEqual([{ text: 'Оно не приближается, пока на него смотрят.' }])
  })
})

describe('текст: фрагменты ⇄ вставки', () => {
  it('соседние фрагменты с одним оформлением склеиваются', () => {
    const inline = richToInline([{ text: 'а' }, { text: 'б' }, { text: 'в', bold: true }, { text: 'г', bold: true }])
    expect(inlineToRuns(inline)).toEqual([{ text: 'аб' }, { text: 'вг', bold: true }])
  })

  it('оформление и уровень независимы: жирный закрытый фрагмент', () => {
    const inline = richToInline([{ text: 'икс', bold: true, italic: true, level: 5 }])
    expect(inline[0]?.marks?.map((m) => m.type).sort()).toEqual(['bold', 'italic', 'redact'])
    expect(inlineToRuns(inline)).toEqual([{ text: 'икс', bold: true, italic: true, level: 5 }])
  })

  it('перевод строки — узел hardBreak и возвращается переводом строки', () => {
    const inline = richToInline([{ text: 'первая\nвторая' }])
    expect(inline.map((n) => n.type)).toEqual(['text', 'hardBreak', 'text'])
    expect(inlineToRuns(inline)).toEqual([{ text: 'первая\nвторая' }])
  })

  it('строка вместо массива принимается; пустой текст даёт пустой результат', () => {
    expect(inlineToRuns(richToInline('просто строка'))).toEqual([{ text: 'просто строка' }])
    expect(richToInline('')).toEqual([])
    expect(richToInline(null)).toEqual([])
    expect(richToInline([{ text: '' }, 5, null])).toEqual([])
  })

  it('уровень 0 и мусорные значения не дают метки закрытия', () => {
    const inline = richToInline([{ text: 'а', level: 0 }, { text: 'б', level: -2 }, { text: 'в', bold: false }])
    expect(inline.every((n) => !n.marks)).toBe(true)
  })
})

describe('пустое не отправляется, но и не теряется в редакторе', () => {
  const docWith = (...content: object[]) => ({ type: 'doc', content })

  it('пустой и пробельный абзацы пропускаются', () => {
    const out = docToBlocks(
      docWith(
        { type: 'paragraph', attrs: { blockId: 'a', level: null } },
        { type: 'paragraph', attrs: { blockId: 'b', level: null }, content: [{ type: 'text', text: '   ' }] },
        { type: 'paragraph', attrs: { blockId: 'c', level: null }, content: [{ type: 'text', text: 'есть' }] },
      ),
    )
    expect(out.map((b) => b.id)).toEqual(['c'])
  })

  it('пустые пункты списка пропускаются, список без пунктов — тоже', () => {
    const list = (items: object[]) => ({ type: 'list', attrs: { blockId: 'l', level: null, ordered: false }, content: items })
    const item = (text: string) => ({ type: 'listItem', content: text ? [{ type: 'text', text }] : [] })
    expect((docToBlocks(docWith(list([item('а'), item(''), item('б')])))[0]?.data as { items: unknown[] }).items).toHaveLength(2)
    expect(docToBlocks(docWith(list([item('')])))).toEqual([])
  })

  it('пустая ячейка таблицы записывается фрагментом с пустым текстом (так её принимает сервер)', () => {
    const doc = blocksToDoc([block('table', { columns: ['А', 'Б'], rows: [[[{ text: '' }], [{ text: 'x' }]]] })])
    const rows = (docToBlocks(doc)[0]?.data as { rows: unknown[][][] }).rows
    expect(rows[0]?.[0]).toEqual([{ text: '' }])
  })

  it('пустые абзацы записки и статьи пропускаются', () => {
    const memo = blocksToDoc([block('memo', { kind: 'memo', body: ['а', '', 'б'] })])
    expect((docToBlocks(memo)[0]?.data as { body: unknown[] }).body).toHaveLength(2)
    const clip = blocksToDoc([block('clipping', { kind: 'newspaper', paragraphs: ['а', ''] })])
    expect((docToBlocks(clip)[0]?.data as { paragraphs: unknown[] }).paragraphs).toHaveLength(1)
  })
})

describe('устойчивость к неполным и неверным данным (черновики)', () => {
  it('таблица с короткими строками дополняется пустыми ячейками: ничего не теряется', () => {
    const doc = blocksToDoc([block('table', { columns: ['А'], rows: [['a', 'b', 'c']] })])
    const header = doc.content?.[0]?.content?.[0]?.content
    expect(header).toHaveLength(3)
    const data = docToBlocks(doc)[0]?.data as { columns: string[]; rows: { text: string }[][][] }
    expect(data.columns).toEqual(['А', '', ''])
    expect(data.rows[0]?.map((c) => c[0]?.text)).toEqual(['a', 'b', 'c'])
  })

  it('блок неизвестного вида возвращается без изменений', () => {
    const unknown = block('hologram', { any: ['thing', 1] }, { level: 3 })
    const doc = blocksToDoc([unknown])
    expect(doc.content?.[0]?.type).toBe('unknownBlock')
    expect(docToBlocks(doc)).toEqual([unknown])
    expect(nodeNameOf('hologram')).toBe('unknownBlock')
  })

  it('данные не того типа не роняют преобразование', () => {
    const junk = [
      block('heading', 'строка'),
      block('list', { items: 'не список' }),
      block('table', { columns: 5, rows: 'x' }),
      block('experiment_log', { entries: [null, 3] }),
      block('memo', null),
      { id: 'x', type: 'stamp' } as InputBlock,
      null as unknown as InputBlock,
    ]
    expect(() => docToBlocks(blocksToDoc(junk))).not.toThrow()
  })

  it('глубина заголовка приводится к 1–3, по умолчанию 2', () => {
    const depths = [{}, { depth: 0 }, { depth: 9 }, { depth: 1 }].map(
      (d) => (docToBlocks(blocksToDoc([block('heading', { ...d, text: 'т' })]))[0]?.data as { depth: number }).depth,
    )
    expect(depths).toEqual([2, 2, 3, 1])
  })

  it('идентификатор блока без id остаётся пустым: положение присвоит сервер', () => {
    const out = docToBlocks(blocksToDoc([{ type: 'paragraph', data: { text: 'а' } } as InputBlock]))
    expect(out[0]?.id).toBe('')
  })
})

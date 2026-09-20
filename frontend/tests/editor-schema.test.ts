import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { Editor } from '@tiptap/core'
import { UndoRedo } from '@tiptap/extensions'
import { NodeSelection, TextSelection } from '@tiptap/pm/state'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import type { InputBlock } from '@/api/generated/documents'
import { blockAt, canInsert, deleteTableRow, exitEmptyListItem, insertBlock, insertBlocks, moveBlock, removeBlock, setBlockLevel, setRedact, situation } from '@/editor/commands'
import { BLOCK_TYPES, blocksToDoc, canonicalBlocks, docToBlocks } from '@/editor/convert'
import { INSERTABLE } from '@/editor/fields'
import { KupolKeys } from '@/editor/keys'
import { BLOCK_ID_RE, schemaExtensions } from '@/editor/schema'
import { block, canonical } from './helpers/blocks'

// jsdom не умеет измерять текст; ProseMirror при прокрутке к выделению спрашивает размеры — отвечаем нулями.
beforeAll(() => {
  const rect = { x: 0, y: 0, top: 0, left: 0, bottom: 0, right: 0, width: 0, height: 0, toJSON: () => ({}) }
  Range.prototype.getBoundingClientRect = () => rect as DOMRect
  Range.prototype.getClientRects = () => ({ length: 0, item: () => null, [Symbol.iterator]: function* () {} }) as unknown as DOMRectList
  Element.prototype.getClientRects = () => ({ length: 0, item: () => null, [Symbol.iterator]: function* () {} }) as unknown as DOMRectList
  document.elementFromPoint = () => null
})

const editors: Editor[] = []
function create(blocks: InputBlock[] = []): Editor {
  const editor = new Editor({ element: document.createElement('div'), extensions: [...schemaExtensions, KupolKeys, UndoRedo], content: blocksToDoc(blocks) })
  editors.push(editor)
  editor.commands.normalizeBlockIds()
  return editor
}
afterEach(() => editors.splice(0).forEach((e) => e.destroy()))

const out = (e: Editor) => docToBlocks(e.getJSON())
/** Поставить курсор внутрь блока с номером index (в первый текст). */
function cursorIn(e: Editor, index: number, offset = 1) {
  let pos = 0
  e.state.doc.forEach((n, off, i) => {
    if (i === index) pos = off
  })
  e.view.dispatch(e.state.tr.setSelection(TextSelection.near(e.state.doc.resolve(pos + offset))))
}

describe('схема сохраняет каждый знак и атрибут', () => {
  it('каноническая форма проходит через настоящий редактор без изменений', () => {
    const e = create(canonical)
    e.state.doc.check()
    expect(out(e)).toEqual(canonical)
  })

  it('все 15 видов блоков присутствуют в схеме', () => {
    const e = create(canonical)
    const names = new Set<string>()
    e.state.doc.forEach((n) => names.add(n.type.name))
    expect(names.size).toBe(15)
    expect(BLOCK_TYPES).toHaveLength(15)
  })

  it('фикстура e2e со всеми видами блоков не теряет ни блока, ни закрытого фрагмента', () => {
    const pack = JSON.parse(readFileSync(resolve(import.meta.dirname, '../e2e/fixtures/pack.json'), 'utf8')) as { documents: { code: string; blocks: InputBlock[] }[] }
    const blocks = pack.documents.find((d) => d.code === 'О-9001')!.blocks
    const e = create(blocks)
    expect(out(e)).toEqual(canonicalBlocks(blocks))
  })

  it('блок неизвестного вида проходит через редактор нетронутым', () => {
    const unknown = block('hologram', { any: ['thing', 1] }, { level: 3 })
    const e = create([unknown, block('paragraph', { text: [{ text: 'а' }] })])
    expect(out(e)[0]).toEqual(unknown)
  })

  it('внутри редактора блоки переживают копирование в HTML и обратно', () => {
    const e = create(canonical)
    const html = e.getHTML()
    const copy = create()
    copy.commands.setContent(html)
    // идентификаторы уникальны и в копии; сравниваем всё остальное
    const strip = (bs: InputBlock[]) => bs.map((b) => ({ ...b, id: '' }))
    expect(strip(out(copy))).toEqual(strip(canonical))
  })

  it('формат и закрытие фрагмента — метки: жирный закрытый текст возвращается жирным и закрытым', () => {
    const blocks = [block('paragraph', { text: [{ text: 'икс', bold: true, italic: true, level: 5 }, { text: ' и ' }, { text: 'игрек', level: 2 }] })]
    expect(out(create(blocks))).toEqual(blocks)
  })

  it('перенос строки внутри абзаца сохраняется', () => {
    const blocks = [block('paragraph', { text: [{ text: 'первая\nвторая' }] })]
    expect(out(create(blocks))).toEqual(blocks)
  })
})

describe('идентификаторы блоков', () => {
  it('блокам без идентификатора выдаются новые: подходящего вида и все разные', () => {
    const e = create([1, 2, 3, 4].map((n) => ({ id: '', type: 'paragraph', data: { text: [{ text: `п${n}` }] } })))
    const ids = out(e).map((b) => b.id)
    expect(new Set(ids).size).toBe(4)
    expect(ids.every((id) => BLOCK_ID_RE.test(id))).toBe(true)
  })

  it('существующие идентификаторы не трогаются', () => {
    const e = create(canonical)
    expect(out(e).map((b) => b.id)).toEqual(canonical.map((b) => b.id))
  })

  it('повторный идентификатор получает новый: у первого блока остаётся прежний', () => {
    const e = create([
      block('paragraph', { text: [{ text: 'а' }] }, { id: 'same' }),
      block('paragraph', { text: [{ text: 'б' }] }, { id: 'same' }),
    ])
    const ids = out(e).map((b) => b.id)
    expect(ids[0]).toBe('same')
    expect(ids[1]).not.toBe('same')
    expect(BLOCK_ID_RE.test(ids[1]!)).toBe(true)
  })

  it('идентификатор неверного вида заменяется (сервер такой не примет)', () => {
    const e = create([block('paragraph', { text: [{ text: 'а' }] }, { id: 'Верхний Регистр!' })])
    expect(BLOCK_ID_RE.test(out(e)[0]!.id)).toBe(true)
  })

  it('деление абзаца Enter-ом: у второй половины новый идентификатор, допуск блока сохраняется', () => {
    const e = create([block('paragraph', { text: [{ text: 'первая вторая' }] }, { id: 'p', level: 3 })])
    cursorIn(e, 0, 7)
    e.commands.splitBlock()
    const blocks = out(e)
    expect(blocks).toHaveLength(2)
    expect(blocks[0]!.id).toBe('p')
    expect(blocks[1]!.id).not.toBe('p')
    expect(blocks.map((b) => b.level)).toEqual([3, 3])
  })

  it('копия блока рядом с оригиналом получает свой идентификатор', () => {
    const e = create([block('stamp', { text: 'Копия', tone: 'ink' }, { id: 'st' })])
    e.view.dispatch(e.state.tr.insert(e.state.doc.content.size, e.state.doc.child(0).copy(e.state.doc.child(0).content)))
    const ids = out(e).map((b) => b.id)
    expect(new Set(ids).size).toBe(2)
  })
})

describe('вставка блоков', () => {
  it('каждый из 15 видов вставляется и уходит в данные своим видом', () => {
    for (const item of INSERTABLE) {
      const e = create([block('paragraph', { text: [{ text: 'опорный' }] })])
      cursorIn(e, 0)
      const id = insertBlock(e, item.type)
      expect(id, item.type).toMatch(BLOCK_ID_RE)
      e.state.doc.check()
      // пустой блок «не отправляется» только у абзаца; у остальных он на месте и сервер потребует заполнить
      const types = out(e).map((b) => b.type)
      if (item.type !== 'paragraph' && item.type !== 'list' && item.type !== 'clipping') expect(types, item.type).toContain(item.type)
    }
    expect(INSERTABLE.map((i) => i.type).sort()).toEqual([...BLOCK_TYPES].sort())
  })

  it('пустой абзац заменяется вставленным блоком, а не остаётся рядом', () => {
    const e = create([block('paragraph', { text: [{ text: 'первый' }] })])
    e.commands.insertContentAt(e.state.doc.content.size, { type: 'paragraph' })
    cursorIn(e, 1, 0)
    insertBlock(e, 'divider')
    expect(out(e).map((b) => b.type)).toEqual(['paragraph', 'divider'])
  })

  it('блок вставляется после текущего, а не в конец', () => {
    const e = create([block('paragraph', { text: [{ text: 'один' }] }, { id: 'a' }), block('paragraph', { text: [{ text: 'два' }] }, { id: 'b' })])
    cursorIn(e, 0)
    insertBlock(e, 'divider')
    expect(out(e).map((b) => b.type)).toEqual(['paragraph', 'divider', 'paragraph'])
  })

  it('шапка досье — только одна', () => {
    const e = create([block('dossier_header', {})])
    expect(canInsert(e.state, 'dossier_header')).toBe(false)
    expect(insertBlock(e, 'dossier_header')).toBeNull()
    expect(out(e).filter((b) => b.type === 'dossier_header')).toHaveLength(1)
  })

  it('новый блок-«атом» выделяется целиком, у остальных курсор внутри', () => {
    const e = create([block('paragraph', { text: [{ text: 'а' }] })])
    insertBlock(e, 'stamp')
    expect(e.state.selection instanceof NodeSelection).toBe(true)
    insertBlock(e, 'quote')
    expect(e.state.selection instanceof TextSelection).toBe(true)
    expect(blockAt(e.state)?.node.type.name).toBe('quote')
  })

  it('новая таблица: два столбца заголовков и строка ячеек, прямоугольная', () => {
    const e = create([block('paragraph', { text: [{ text: 'а' }] })])
    insertBlock(e, 'table')
    const t = out(e).find((b) => b.type === 'table')!.data as { columns: string[]; rows: unknown[][] }
    expect(t.columns).toEqual(['', ''])
    expect(t.rows).toHaveLength(1)
    expect(t.rows[0]).toHaveLength(2)
  })
})

describe('вставка набора блоков', () => {
  const set = (): InputBlock[] => [
    block('dossier_header', {}, { id: 's1' }),
    block('heading', { depth: 2, text: 'Общие сведения' }, { id: 's2' }),
    block('paragraph', { text: [{ text: 'Закрытый абзац.' }] }, { id: 's3', level: 4 }),
  ]

  it('блоки встают после текущего с новыми идентификаторами; допуск и содержимое сохраняются', () => {
    const e = create([block('paragraph', { text: [{ text: 'один' }] }, { id: 'a' }), block('paragraph', { text: [{ text: 'два' }] }, { id: 'b' })])
    cursorIn(e, 0)
    const res = insertBlocks(e, set())
    expect(res).toMatchObject({ skipped: 0 })
    expect(res.ids).toHaveLength(3)
    const blocks = out(e)
    expect(blocks.map((b) => b.type)).toEqual(['paragraph', 'dossier_header', 'heading', 'paragraph', 'paragraph'])
    expect(blocks.map((b) => b.id).slice(1, 4)).toEqual(res.ids)
    expect(res.ids.every((id) => BLOCK_ID_RE.test(id) && !['s1', 's2', 's3', 'a', 'b'].includes(id))).toBe(true)
    expect(blocks[3]).toMatchObject({ level: 4, data: { text: [{ text: 'Закрытый абзац.' }] } })
    expect(blocks[0]!.id).toBe('a')
  })

  it('дважды один и тот же набор: идентификаторы не повторяются, вторая шапка досье пропускается', () => {
    const e = create([block('paragraph', { text: [{ text: 'один' }] }, { id: 'a' })])
    insertBlocks(e, set())
    const second = insertBlocks(e, set())
    expect(second.skipped).toBe(1)
    expect(second.ids).toHaveLength(2)
    const blocks = out(e)
    expect(blocks.filter((b) => b.type === 'dossier_header')).toHaveLength(1)
    expect(new Set(blocks.map((b) => b.id)).size).toBe(blocks.length)
  })

  it('пустой абзац заменяется набором, а набор из одной пропущенной шапки ничего не меняет', () => {
    const e = create([block('dossier_header', {}, { id: 'h' })])
    e.commands.insertContentAt(e.state.doc.content.size, { type: 'paragraph' })
    cursorIn(e, 1, 0)
    const before = JSON.stringify(out(e))
    expect(insertBlocks(e, [block('dossier_header', {})])).toEqual({ ids: [], skipped: 1 })
    expect(JSON.stringify(out(e))).toBe(before)
    insertBlocks(e, set().slice(1))
    expect(out(e).map((b) => b.type)).toEqual(['dossier_header', 'heading', 'paragraph'])
  })

  it('без выделения набор встаёт в конец; вставка одним шагом отменяется одним Ctrl+Z', () => {
    const e = create([block('paragraph', { text: [{ text: 'один' }] }, { id: 'a' })])
    insertBlocks(e, set().slice(1))
    expect(out(e)).toHaveLength(3)
    e.commands.undo()
    expect(out(e).map((b) => b.id)).toEqual(['a'])
  })
})

describe('перемещение, удаление, допуск блока', () => {
  const three = () =>
    create([1, 2, 3].map((n) => block('paragraph', { text: [{ text: `п${n}` }] }, { id: `p${n}` })))

  it('вверх и вниз: блок меняется местами с соседом, курсор остаётся в нём', () => {
    const e = three()
    cursorIn(e, 1)
    expect(moveBlock(e, -1)).toBe(true)
    expect(out(e).map((b) => b.id)).toEqual(['p2', 'p1', 'p3'])
    expect(blockAt(e.state)?.node.attrs.blockId).toBe('p2')
    expect(moveBlock(e, 1)).toBe(true)
    expect(moveBlock(e, 1)).toBe(true)
    expect(out(e).map((b) => b.id)).toEqual(['p1', 'p3', 'p2'])
  })

  it('крайний блок не двигается за край', () => {
    const e = three()
    cursorIn(e, 0)
    expect(moveBlock(e, -1)).toBe(false)
    cursorIn(e, 2)
    expect(moveBlock(e, 1)).toBe(false)
    expect(out(e).map((b) => b.id)).toEqual(['p1', 'p2', 'p3'])
  })

  it('перемещение блока-«атома» тоже работает', () => {
    const e = create([block('divider', { style: 'line' }, { id: 'd' }), block('paragraph', { text: [{ text: 'а' }] }, { id: 'p' })])
    e.view.dispatch(e.state.tr.setSelection(NodeSelection.create(e.state.doc, 0)))
    expect(moveBlock(e, 1)).toBe(true)
    expect(out(e).map((b) => b.id)).toEqual(['p', 'd'])
  })

  it('удаление блока; последний блок заменяется пустым абзацем', () => {
    const e = three()
    cursorIn(e, 1)
    expect(removeBlock(e)).toBe(true)
    expect(out(e).map((b) => b.id)).toEqual(['p1', 'p3'])
    cursorIn(e, 0)
    removeBlock(e)
    cursorIn(e, 0)
    removeBlock(e)
    expect(e.state.doc.childCount).toBe(1)
    expect(out(e)).toEqual([])
  })

  it('допуск блока ставится и снимается', () => {
    const e = three()
    cursorIn(e, 1)
    setBlockLevel(e, 4)
    expect(out(e).map((b) => b.level)).toEqual([undefined, 4, undefined])
    setBlockLevel(e, null)
    expect(out(e)[1]).not.toHaveProperty('level')
  })
})

describe('закрытие фрагмента', () => {
  it('выделенный текст закрывается до уровня и открывается обратно', () => {
    const e = create([block('paragraph', { text: [{ text: 'открыто секрет открыто' }] })])
    e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, 9, 15)))
    setRedact(e, 4)
    expect(out(e)[0]!.data).toEqual({ text: [{ text: 'открыто ' }, { text: 'секрет', level: 4 }, { text: ' открыто' }] })
    expect(situation(e).redact).toBe(4)
    setRedact(e, 0)
    expect(out(e)[0]!.data).toEqual({ text: [{ text: 'открыто секрет открыто' }] })
  })

  it('повторное закрытие другим уровнем заменяет прежнее, а не накладывается', () => {
    const e = create([block('paragraph', { text: [{ text: 'секрет' }] })])
    e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, 1, 7)))
    setRedact(e, 2)
    setRedact(e, 5)
    expect(out(e)[0]!.data).toEqual({ text: [{ text: 'секрет', level: 5 }] })
  })

  it('закрытие сочетается с жирным и курсивом', () => {
    const e = create([block('paragraph', { text: [{ text: 'секрет' }] })])
    e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, 1, 7)))
    e.commands.toggleBold()
    setRedact(e, 3)
    expect(out(e)[0]!.data).toEqual({ text: [{ text: 'секрет', bold: true, level: 3 }] })
  })

  it('в заголовке текст простой: формат и закрытие не применяются', () => {
    const e = create([block('heading', { depth: 2, text: 'Заголовок' })])
    cursorIn(e, 0)
    expect(situation(e).canFormat).toBe(false)
    e.view.dispatch(e.state.tr.setSelection(TextSelection.create(e.state.doc, 1, 6)))
    setRedact(e, 3)
    e.commands.toggleBold()
    expect(out(e)[0]!.data).toEqual({ depth: 2, text: 'Заголовок' })
  })

  it('печатать сразу после закрытого фрагмента — открытый текст', () => {
    const e = create([block('paragraph', { text: [{ text: 'секрет', level: 3 }] })])
    e.view.dispatch(e.state.tr.setSelection(TextSelection.atEnd(e.state.doc)))
    e.commands.insertContent('открыто')
    expect(out(e)[0]!.data).toEqual({ text: [{ text: 'секрет', level: 3 }, { text: 'открыто' }] })
  })
})

describe('защита блока при наборе', () => {
  it('пока блок выделен целиком, набор текста его не заменяет', () => {
    const e = create([block('stamp', { text: 'Копия', tone: 'ink' }, { id: 'st' }), block('paragraph', { text: [{ text: 'а' }] })])
    e.view.dispatch(e.state.tr.setSelection(NodeSelection.create(e.state.doc, 0)))
    const handled = e.view.someProp('handleTextInput', (f) => f(e.view, 0, 1, 'x', () => e.state.tr))
    expect(handled).toBe(true)
    expect(out(e)[0]).toMatchObject({ id: 'st', type: 'stamp' })
  })

  it('обычный набор в тексте не перехватывается', () => {
    const e = create([block('paragraph', { text: [{ text: 'а' }] })])
    cursorIn(e, 0)
    expect(e.view.someProp('handleTextInput', (f) => f(e.view, 1, 1, 'x', () => e.state.tr))).toBeFalsy()
  })
})

describe('клавиши', () => {
  const press = (e: Editor, key: string) => {
    const ev = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true })
    e.view.dom.dispatchEvent(ev)
    return ev.defaultPrevented
  }

  it('Enter в цитате и сноске — перенос строки, а не новый блок', () => {
    const e = create([block('quote', { text: [{ text: 'строка' }] }), block('footnote', { mark: '*', text: [{ text: 'сноска' }] })])
    cursorIn(e, 0, 3)
    expect(press(e, 'Enter')).toBe(true)
    expect(out(e)).toHaveLength(2)
    expect((out(e)[0]!.data as { text: { text: string }[] }).text[0]!.text).toBe('ст\nрока')
    cursorIn(e, 1, 3)
    press(e, 'Enter')
    expect(out(e)).toHaveLength(2)
  })

  it('Enter в пункте списка — новый пункт; на пустом последнем — выход из списка в абзац', () => {
    const e = create([block('list', { items: [[{ text: 'один' }]] }, { id: 'l' })])
    e.view.dispatch(e.state.tr.setSelection(TextSelection.atEnd(e.state.doc)))
    e.commands.splitBlock() // второй, пустой пункт
    expect(e.state.doc.child(0).childCount).toBe(2)
    expect(exitEmptyListItem(e)).toBe(true)
    expect(e.state.doc.childCount).toBe(2)
    expect(e.state.doc.child(0).childCount).toBe(1)
    expect(e.state.doc.child(1).type.name).toBe('paragraph')
    expect(blockAt(e.state)?.node.type.name).toBe('paragraph')
  })

  it('единственный пустой пункт по Enter/Backspace превращается в абзац', () => {
    const e = create()
    insertBlock(e, 'list')
    expect(e.state.doc.child(0).type.name).toBe('list')
    expect(exitEmptyListItem(e, { onlySingle: true })).toBe(true)
    expect(e.state.doc.child(0).type.name).toBe('paragraph')
  })

  it('на непустом пункте выход из списка не срабатывает', () => {
    const e = create([block('list', { items: [[{ text: 'один' }]] })])
    cursorIn(e, 0, 3)
    expect(exitEmptyListItem(e)).toBe(false)
  })
})

describe('таблица', () => {
  const table = () =>
    create([
      block('table', {
        columns: ['А', 'Б'],
        rows: [[[{ text: '1' }], [{ text: '2' }]], [[{ text: '3' }], [{ text: '4' }]]],
      }),
    ])
  const cell = (e: Editor, row: number, col: number) => {
    let pos = -1
    e.state.doc.descendants((n, p) => {
      if (n.type.name === 'tableRow') {
        const r = e.state.doc.resolve(p)
        if (r.index() === row) {
          let off = p + 1
          n.forEach((c, _o, i) => {
            if (i === col) pos = off + 1
            off += c.nodeSize
          })
        }
      }
      return true
    })
    return pos
  }
  const into = (e: Editor, row: number, col: number) => e.view.dispatch(e.state.tr.setSelection(TextSelection.near(e.state.doc.resolve(cell(e, row, col)))))

  it('строку заголовков удалить нельзя, обычную — можно', () => {
    const e = table()
    into(e, 0, 0)
    expect(situation(e).onTableHeader).toBe(true)
    expect(deleteTableRow(e)).toBe(false)
    into(e, 1, 0)
    expect(deleteTableRow(e)).toBe(true)
    const data = out(e)[0]!.data as { columns: string[]; rows: unknown[][] }
    expect(data.columns).toEqual(['А', 'Б'])
    expect(data.rows).toHaveLength(1)
  })

  it('строка и столбец добавляются, таблица остаётся прямоугольной', () => {
    const e = table()
    into(e, 1, 0)
    expect(e.commands.addRowAfter()).toBe(true)
    into(e, 0, 1)
    expect(e.commands.addColumnAfter()).toBe(true)
    const data = out(e)[0]!.data as { columns: string[]; rows: unknown[][] }
    expect(data.columns).toHaveLength(3)
    expect(data.rows).toHaveLength(3)
    expect(data.rows.every((r) => r.length === 3)).toBe(true)
  })

  it('Enter в ячейке — перенос строки; в заголовке столбца ничего не делает', () => {
    const e = table()
    into(e, 1, 0)
    e.view.dom.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    expect((out(e)[0]!.data as { rows: { text: string }[][][] }).rows[0]![0]![0]!.text).toBe('\n1')
    into(e, 0, 0)
    e.view.dom.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    expect((out(e)[0]!.data as { columns: string[] }).columns).toEqual(['А', 'Б'])
  })

  it('форматирование в заголовке столбца не применяется, в ячейке — применяется', () => {
    const e = table()
    into(e, 0, 0)
    expect(situation(e).canFormat).toBe(false)
    into(e, 1, 1)
    expect(situation(e).canFormat).toBe(true)
  })
})

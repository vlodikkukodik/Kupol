// Команды редактора над блоками документа. Всё, что панель инструментов делает с «текущим блоком», — здесь,
// чтобы проверяться отдельно от интерфейса.
import type { Editor } from '@tiptap/core'
import type { InputBlock } from '@/api/generated/documents'
import { Fragment, type Node as PMNode } from '@tiptap/pm/model'
import { NodeSelection, TextSelection, type EditorState, type Transaction } from '@tiptap/pm/state'
import { blocksToDoc } from './convert'
import { INSERTABLE } from './fields'
import { newBlockId } from './schema'

export interface BlockRef {
  node: PMNode
  /** Позиция перед блоком */
  pos: number
  /** Номер блока среди блоков документа */
  index: number
}

/** Блок верхнего уровня, в котором стоит выделение (или который выделен целиком). */
export function blockAt(state: EditorState): BlockRef | null {
  const { selection } = state
  const { $from } = selection
  if (selection instanceof NodeSelection && $from.depth === 0) return { node: selection.node, pos: $from.pos, index: $from.index(0) }
  if ($from.depth >= 1) return { node: $from.node(1), pos: $from.before(1), index: $from.index(0) }
  return null
}

/** Начальные данные нового блока: всё пустое, чтобы забытое поле сервер заметил, а не опубликовал случайный текст. */
const EMPTY_DATA: Record<string, unknown> = {
  paragraph: { text: [] },
  heading: { depth: 2, text: '' },
  list: { items: [[]] },
  quote: { text: [] },
  dossier_header: {},
  experiment_log: { entries: [{ text: [] }] },
  stamp: { text: '', tone: 'red' },
  memo: { kind: 'memo', body: [[]] },
  clipping: { kind: 'newspaper', paragraphs: [[]] },
  table: { columns: ['', ''], rows: [[[{ text: '' }], [{ text: '' }]]] },
  doc_link: { code: '' },
  divider: { style: 'line' },
  page: {},
  footnote: { mark: '', text: [] },
  appendix: { title: '' },
}

const ids = (doc: PMNode): Set<string> => {
  const taken = new Set<string>()
  doc.forEach((n) => {
    if (typeof n.attrs.blockId === 'string' && n.attrs.blockId) taken.add(n.attrs.blockId)
  })
  return taken
}

/** Есть ли в документе блок этого узла (шапка досье — одна на документ). */
export const hasNode = (state: EditorState, name: string): boolean => {
  let found = false
  state.doc.forEach((n) => {
    if (n.type.name === name) found = true
  })
  return found
}

/** Можно ли вставить блок вида type сейчас. */
export function canInsert(state: EditorState, type: string): boolean {
  const item = INSERTABLE.find((i) => i.type === type)
  if (!item) return false
  return item.node !== 'dossierHeader' || !hasNode(state, 'dossierHeader')
}

const isEmptyParagraph = (n: PMNode) => n.type.name === 'paragraph' && n.content.size === 0

/** Курсор внутрь нового блока: в первый текст либо, у блока-«атома», выделение самого блока. */
function placeCursor(tr: Transaction, node: PMNode, pos: number): Transaction {
  if (node.isAtom) return tr.setSelection(NodeSelection.create(tr.doc, pos))
  return tr.setSelection(TextSelection.near(tr.doc.resolve(pos + 1)))
}

/**
 * Вставить пустой блок вида type после текущего (пустой абзац — заменить). Возвращает идентификатор нового блока
 * или null, если вставить нельзя.
 */
export function insertBlock(editor: Editor, type: string): string | null {
  const { state } = editor
  if (!canInsert(state, type)) return null
  const json = blocksToDoc([{ id: '', type, data: EMPTY_DATA[type] ?? {} }]).content?.[0]
  if (!json) return null
  const id = newBlockId(ids(state.doc))
  const node = editor.schema.nodeFromJSON({ ...json, attrs: { ...json.attrs, blockId: id } })

  const ref = blockAt(state)
  const tr = state.tr
  let pos: number
  if (!ref) pos = state.doc.content.size
  else if (isEmptyParagraph(ref.node)) {
    tr.delete(ref.pos, ref.pos + ref.node.nodeSize)
    pos = ref.pos
  } else pos = ref.pos + ref.node.nodeSize
  tr.insert(pos, node)
  placeCursor(tr, node, pos).scrollIntoView()
  editor.view.dispatch(tr)
  return id
}

/**
 * Вставить набор блоков (из шаблона) после текущего блока; пустой абзац заменяется. Все блоки получают новые
 * идентификаторы (повторов не будет, даже если набор вставляют дважды), допуск блоков сохраняется. Шапка досье в документе
 * одна: если она уже есть, эта из набора пропускается. Возвращает идентификаторы вставленных блоков и число пропущенных.
 */
export function insertBlocks(editor: Editor, blocks: InputBlock[]): { ids: string[]; skipped: number } {
  const { state } = editor
  let skipped = 0
  const wanted = blocks.filter((b) => {
    if (b.type === 'dossier_header' && hasNode(state, 'dossierHeader')) {
      skipped++
      return false
    }
    return true
  })
  if (wanted.length === 0) return { ids: [], skipped }
  const taken = ids(state.doc)
  const nodes = (blocksToDoc(wanted.map((b) => ({ ...b, id: '' }))).content ?? []).map((json) => {
    const id = newBlockId(taken)
    taken.add(id)
    return editor.schema.nodeFromJSON({ ...json, attrs: { ...json.attrs, blockId: id } })
  })

  const ref = blockAt(state)
  const tr = state.tr
  let pos: number
  if (!ref) pos = state.doc.content.size
  else if (isEmptyParagraph(ref.node)) {
    tr.delete(ref.pos, ref.pos + ref.node.nodeSize)
    pos = ref.pos
  } else pos = ref.pos + ref.node.nodeSize
  tr.insert(pos, Fragment.fromArray(nodes))
  placeCursor(tr, nodes[0]!, pos).scrollIntoView()
  editor.view.dispatch(tr)
  return { ids: nodes.map((n) => String(n.attrs.blockId)), skipped }
}

/** Поменять блок местами с соседним (dir: -1 — вверх, 1 — вниз). */
export function moveBlock(editor: Editor, dir: -1 | 1): boolean {
  const { state } = editor
  const ref = blockAt(state)
  if (!ref) return false
  const other = ref.index + dir
  if (other < 0 || other >= state.doc.childCount) return false
  const tr = state.tr
  const size = ref.node.nodeSize
  let pos: number
  if (dir < 0) {
    pos = ref.pos - state.doc.child(other).nodeSize
    tr.delete(ref.pos, ref.pos + size).insert(pos, ref.node)
  } else {
    pos = ref.pos + state.doc.child(other).nodeSize
    tr.insert(ref.pos + size + state.doc.child(other).nodeSize, ref.node).delete(ref.pos, ref.pos + size)
  }
  placeCursor(tr, ref.node, pos).scrollIntoView()
  editor.view.dispatch(tr)
  return true
}

/** Удалить текущий блок. Документ без блоков не бывает: остаётся пустой абзац. */
export function removeBlock(editor: Editor): boolean {
  const { state } = editor
  const ref = blockAt(state)
  if (!ref) return false
  const tr = state.tr.delete(ref.pos, ref.pos + ref.node.nodeSize)
  if (tr.doc.childCount === 0) tr.insert(0, editor.schema.nodes.paragraph!.create())
  tr.setSelection(TextSelection.near(tr.doc.resolve(Math.min(ref.pos, tr.doc.content.size)), -1)).scrollIntoView()
  editor.view.dispatch(tr)
  return true
}

/**
 * Перенос строки внутри текста. Встроенный setHardBreak Tiptap отказывается работать в «изолированных» узлах
 * (цитата, сноска, ячейка), а нужен он именно там; оформление (жирный, закрытие) переносом не прерывается.
 */
export function insertLineBreak(editor: Editor): boolean {
  const { state } = editor
  const { $from } = state.selection
  const type = state.schema.nodes.hardBreak
  if (!type || !$from.parent.canReplaceWith($from.index(), $from.indexAfter(), type)) return false
  const marks = state.storedMarks ?? $from.marks()
  editor.view.dispatch(state.tr.replaceSelectionWith(type.create(null, null, marks), false).scrollIntoView())
  return true
}

/** Допуск блока: «не ниже уровня N»; null — как у документа. */
export function setBlockLevel(editor: Editor, level: number | null): boolean {
  const ref = blockAt(editor.state)
  if (!ref) return false
  editor.view.dispatch(editor.state.tr.setNodeAttribute(ref.pos, 'level', level))
  return true
}

/** Закрыть выделенный текст до уровня level (0 или меньше — открыть). */
export function setRedact(editor: Editor, level: number): boolean {
  if (level <= 0) return editor.chain().focus().unsetMark('redact').run()
  return editor.chain().focus().setMark('redact', { level }).run()
}

/** Номер строки таблицы, в которой стоит выделение (0 — строка заголовков); null — вне таблицы. */
export function tableRowIndex(state: EditorState): number | null {
  const { $from } = state.selection
  for (let d = $from.depth; d >= 1; d--) {
    if ($from.node(d).type.name === 'tableRow') return $from.index(d - 1)
  }
  return null
}

/** Удалить строку таблицы. Строку заголовков (первую) удалять нельзя: из неё сервер берёт названия столбцов. */
export function deleteTableRow(editor: Editor): boolean {
  const row = tableRowIndex(editor.state)
  if (row === null || row === 0) return false
  return editor.chain().focus().deleteRow().run()
}

/**
 * Пустой пункт списка + Enter: выйти из списка (последний пункт) или превратить единственный пункт в абзац.
 * onlySingle — для Backspace: только единственный пункт (в длинном списке Backspace убирает лишь пустой пункт как обычно).
 */
export function exitEmptyListItem(editor: Editor, { onlySingle = false } = {}): boolean {
  const { state } = editor
  const { $from, empty } = state.selection
  if (!empty || $from.parent.type.name !== 'listItem' || $from.parent.content.size !== 0) return false
  const list = $from.node(-1)
  if (onlySingle && list.childCount > 1) return false
  const isLast = $from.index(-1) === list.childCount - 1
  if (!isLast && list.childCount > 1) return false // пустой пункт в середине: Enter делит список, как обычно
  const listPos = $from.before(-1)
  const tr = state.tr
  const paragraph = editor.schema.nodes.paragraph!.create()
  if (list.childCount === 1) {
    tr.replaceWith(listPos, listPos + list.nodeSize, paragraph)
    tr.setSelection(TextSelection.near(tr.doc.resolve(listPos + 1)))
  } else {
    tr.delete($from.before(), $from.after())
    const after = listPos + tr.doc.nodeAt(listPos)!.nodeSize
    tr.insert(after, paragraph)
    tr.setSelection(TextSelection.near(tr.doc.resolve(after + 1)))
  }
  editor.view.dispatch(tr.scrollIntoView())
  return true
}

/** Состояние для панели инструментов: где стоит выделение и что можно сделать. */
export interface Situation {
  /** Узел текущего блока и его номер */
  block: { node: string; index: number; level: number | null; id: string } | null
  count: number
  canMoveUp: boolean
  canMoveDown: boolean
  bold: boolean
  italic: boolean
  /** Уровень закрытия выделенного текста; 0 — не закрыт */
  redact: number
  /** Можно ли форматировать выделенное (в заголовке и шапке таблицы текст простой) */
  canFormat: boolean
  canUndo: boolean
  canRedo: boolean
  inTable: boolean
  /** Строка заголовков таблицы: её удалять нельзя */
  onTableHeader: boolean
}

export function situation(editor: Editor): Situation {
  const { state } = editor
  const ref = blockAt(state)
  const row = tableRowIndex(state)
  const parent = state.selection.$from.parent
  return {
    block: ref ? { node: ref.node.type.name, index: ref.index, level: typeof ref.node.attrs.level === 'number' ? ref.node.attrs.level : null, id: String(ref.node.attrs.blockId ?? '') } : null,
    count: state.doc.childCount,
    canMoveUp: Boolean(ref && ref.index > 0),
    canMoveDown: Boolean(ref && ref.index < state.doc.childCount - 1),
    bold: editor.isActive('bold'),
    italic: editor.isActive('italic'),
    redact: Number(editor.getAttributes('redact').level ?? 0) || 0,
    canFormat: parent.inlineContent && parent.type.spec.marks !== '',
    canUndo: editor.can().undo(),
    canRedo: editor.can().redo(),
    inTable: row !== null,
    onTableHeader: row === 0,
  }
}

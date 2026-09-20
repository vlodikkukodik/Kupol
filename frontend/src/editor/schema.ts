// Схема редактора: узлы для всех 15 видов блоков, метка «закрыть до уровня N» и служебные атрибуты блока.
// Здесь только описание документа (без Vue и без представлений узлов): по нему же проверяются преобразования и команды.
// Представления узлов (поля в шапке блока) подключает kit.ts.
import { Extension, Mark, mergeAttributes, Node, type AnyExtension } from '@tiptap/core'
import Bold from '@tiptap/extension-bold'
import Document from '@tiptap/extension-document'
import HardBreak from '@tiptap/extension-hard-break'
import Italic from '@tiptap/extension-italic'
import Paragraph from '@tiptap/extension-paragraph'
import { Table, TableCell, TableHeader, TableRow } from '@tiptap/extension-table'
import Text from '@tiptap/extension-text'
import { Plugin, PluginKey, type Transaction } from '@tiptap/pm/state'
import type { Node as PMNode } from '@tiptap/pm/model'
import { BLOCK_NODE_NAMES } from './convert'

/** Допустимый вид идентификатора блока — тот же, что проверяет сервер (documents.blockIDRe). */
export const BLOCK_ID_RE = /^[a-z0-9][a-z0-9_-]{0,31}$/

// ————— атрибуты —————

const kebab = (s: string) => s.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`)

/** Значение атрибута → строка для HTML (строки как есть, остальное — JSON). */
function encode(v: unknown): string {
  return typeof v === 'string' ? v : JSON.stringify(v)
}

/** Строка из HTML → значение того же вида, что и значение по умолчанию; неподходящее — по умолчанию. */
function decode<T>(raw: string | null, fallback: T): T {
  if (raw === null) return fallback
  if (typeof fallback === 'string') return raw as T
  try {
    const v: unknown = JSON.parse(raw)
    if (Array.isArray(fallback) ? Array.isArray(v) : typeof v === typeof fallback) return v as T
  } catch {
    // не JSON — берём значение по умолчанию
  }
  return fallback
}

/** Атрибут узла, который переживает копирование и вставку внутри редактора (хранится в data-атрибуте). */
function attr<T>(name: string, fallback: T) {
  const dataName = `data-${kebab(name)}`
  return {
    default: fallback,
    parseHTML: (el: HTMLElement) => decode(el.getAttribute(dataName), fallback),
    renderHTML: (attrs: Record<string, unknown>) => ({ [dataName]: encode(attrs[name]) }),
  }
}

interface Spec {
  name: string
  /** Выражение содержимого ProseMirror; без него узел — «атом» (правится полями в шапке) */
  content?: string
  attrs?: Record<string, unknown>
  /** Верхнего уровня (блок документа) или вложенный (пункт, запись, реплика) */
  nested?: boolean
  parse?: { tag: string; attrs?: Record<string, unknown> }[]
  marks?: string
}

/** Узел редактора по описанию: одинаковая разметка для копирования и вставки внутри редактора. */
function kupolNode({ name, content, attrs = {}, nested = false, parse = [], marks }: Spec) {
  return Node.create({
    name,
    ...(nested ? {} : { group: 'block' }),
    ...(content ? { content } : { atom: true }),
    ...(marks !== undefined ? { marks } : {}),
    selectable: true,
    defining: true,
    // Блок верхнего уровня — отдельная единица: Backspace на его границе не сливает текст соседних блоков.
    // Пункты, записи и реплики внутри блока изолированными не бывают: их соединяют и разделяют как обычные строки.
    isolating: !nested,
    addAttributes() {
      return Object.fromEntries(Object.entries(attrs).map(([key, fallback]) => [key, attr(key, fallback)]))
    },
    parseHTML() {
      return [{ tag: `[data-kupol="${name}"]` }, ...parse]
    },
    renderHTML({ HTMLAttributes }) {
      const attributes = mergeAttributes({ 'data-kupol': name }, HTMLAttributes)
      return content ? ['div', attributes, 0] : ['div', attributes]
    },
  })
}

// ————— метка закрытия —————

/** «Закрыть до уровня N»: фрагмент текста, который читатели с допуском ниже N не увидят (Run.Level на сервере). */
export const Redact = Mark.create({
  name: 'redact',
  // Печатать сразу после закрытого фрагмента — значит писать открытый текст: закрытие ставится только явно.
  inclusive: false,
  addAttributes() {
    return {
      level: {
        default: 1,
        parseHTML: (el: HTMLElement) => Number(el.getAttribute('data-redact')) || 1,
        renderHTML: (attrs: Record<string, unknown>) => ({ 'data-redact': String(attrs.level) }),
      },
    }
  },
  parseHTML() {
    return [{ tag: 'span[data-redact]' }]
  },
  renderHTML({ HTMLAttributes }) {
    return ['span', mergeAttributes({ class: 'pm-redact' }, HTMLAttributes), 0]
  },
})

// ————— идентификатор и уровень блока —————

const ID_ALPHABET = '0123456789abcdefghijklmnopqrstuvwxyz'

/** Новый идентификатор блока, которого нет среди занятых. */
export function newBlockId(taken: ReadonlySet<string>): string {
  for (;;) {
    let id = 'b'
    for (let i = 0; i < 7; i++) id += ID_ALPHABET[Math.floor(Math.random() * ID_ALPHABET.length)]
    if (!taken.has(id)) return id
  }
}

/**
 * Приводит идентификаторы блоков верхнего уровня к порядку: у каждого блока он есть, подходит по виду и не повторяется.
 * Первый блок с идентификатором его сохраняет; повтор (разделили абзац, вставили копию) получает новый.
 * Нужен всегда: ссылки и якоря держатся на идентификаторах, а сервер повторы отклоняет.
 */
export function fixBlockIds(tr: Transaction, doc: PMNode): Transaction | null {
  const taken = new Set<string>()
  doc.forEach((node) => {
    const id: unknown = node.attrs.blockId
    if (typeof id === 'string' && id) taken.add(id)
  })
  const seen = new Set<string>()
  let changed = false
  doc.forEach((node, offset) => {
    if (!('blockId' in node.attrs)) return
    const id: unknown = node.attrs.blockId
    if (typeof id === 'string' && BLOCK_ID_RE.test(id) && !seen.has(id)) {
      seen.add(id)
      return
    }
    const fresh = newBlockId(taken)
    taken.add(fresh)
    seen.add(fresh)
    tr.setNodeAttribute(offset, 'blockId', fresh)
    changed = true
  })
  return changed ? tr.setMeta('addToHistory', false) : null
}

/** Общие атрибуты всех блоков верхнего уровня: blockId и level («не ниже уровня N»; пусто — как у документа). */
export const BlockMeta = Extension.create({
  name: 'blockMeta',
  addGlobalAttributes() {
    return [
      {
        types: [...BLOCK_NODE_NAMES, 'paragraph', 'table'],
        attributes: {
          blockId: {
            default: '',
            parseHTML: (el: HTMLElement) => el.getAttribute('data-block-id') ?? '',
            renderHTML: (attrs: Record<string, unknown>) => (attrs.blockId ? { 'data-block-id': String(attrs.blockId) } : {}),
          },
          level: {
            default: null,
            parseHTML: (el: HTMLElement) => {
              const raw = el.getAttribute('data-block-level')
              return raw === null || raw === '' ? null : Number(raw)
            },
            renderHTML: (attrs: Record<string, unknown>) => (attrs.level === null || attrs.level === undefined ? {} : { 'data-block-level': String(attrs.level) }),
          },
        },
      },
    ]
  },
  addCommands() {
    return {
      normalizeBlockIds:
        () =>
        ({ state, dispatch }) => {
          const tr = fixBlockIds(state.tr, state.doc)
          if (tr && dispatch) dispatch(tr)
          return Boolean(tr)
        },
    }
  },
  addProseMirrorPlugins() {
    return [
      new Plugin({
        key: new PluginKey('kupolBlockIds'),
        appendTransaction: (transactions, _old, state) => (transactions.some((t) => t.docChanged) ? fixBlockIds(state.tr, state.doc) : null),
      }),
    ]
  },
})

declare module '@tiptap/core' {
  interface Commands<ReturnType> {
    blockMeta: {
      /** Проставить блокам без идентификатора (и с повторным) новые */
      normalizeBlockIds: () => ReturnType
    }
  }
}

// ————— узлы —————

const heading = kupolNode({
  name: 'heading',
  content: 'text*',
  marks: '', // заголовок — простая строка: без жирного, курсива и закрытия
  attrs: { depth: 2 },
  parse: [
    { tag: 'h1', attrs: { depth: 1 } },
    { tag: 'h2', attrs: { depth: 2 } },
    { tag: 'h3', attrs: { depth: 3 } },
    { tag: 'h4', attrs: { depth: 3 } },
  ],
})

const list = kupolNode({ name: 'list', content: 'listItem+', attrs: { ordered: false } })
const listItem = kupolNode({ name: 'listItem', content: 'inline*', nested: true })
const quote = kupolNode({ name: 'quote', content: 'inline*', attrs: { source: '' } })
const dossierHeader = kupolNode({ name: 'dossierHeader' })
const experimentLog = kupolNode({ name: 'experimentLog', content: 'logEntry+', attrs: { title: '' } })
const logEntry = kupolNode({ name: 'logEntry', content: 'inline*', nested: true, attrs: { date: '', participants: [] as string[] } })
const stamp = kupolNode({ name: 'stamp', attrs: { text: '', tone: 'red', tilt: 0 } })
const memo = kupolNode({
  name: 'memo',
  content: 'memoParagraph+',
  attrs: { kind: 'memo', number: '', date: '', from: '', to: [] as string[], subject: '', signature: '' },
})
const memoParagraph = kupolNode({ name: 'memoParagraph', content: 'inline*', nested: true })
const clipping = kupolNode({ name: 'clipping', content: 'clipLine+', attrs: { kind: 'newspaper', title: '', source: '', date: '' } })
const clipLine = kupolNode({ name: 'clipLine', content: 'inline*', nested: true, attrs: { speaker: '' } })
const docLink = kupolNode({ name: 'docLink', attrs: { code: '', note: '' } })
const divider = kupolNode({ name: 'divider', attrs: { style: 'line' } })
const pageBreak = kupolNode({ name: 'pageBreak', attrs: { number: '' } })
const footnote = kupolNode({ name: 'footnote', content: 'inline*', attrs: { mark: '' } })
const appendix = kupolNode({ name: 'appendix', attrs: { number: '', title: '' } })
/** Блок вида, которого этот редактор не знает (пришёл от более новой версии сервера): хранится как есть. */
const unknownBlock = kupolNode({ name: 'unknownBlock', attrs: { kind: '', data: null as unknown } })

// Таблица: своя подпись, ячейки с текстом в строку (не абзацы), заголовки столбцов — простой текст. Объединять ячейки нельзя:
// сервер хранит таблицу прямоугольной, поэтому в интерфейсе команд объединения нет.
const kupolTable = Table.extend({
  addAttributes() {
    return { ...this.parent?.(), caption: attr('caption', '') }
  },
})
const kupolCell = TableCell.extend({ content: 'inline*' })
const kupolHeaderCell = TableHeader.extend({ content: 'text*', marks: '' })

/** Расширения, задающие схему документа (без представлений узлов, истории и подсказок). */
export const schemaExtensions: AnyExtension[] = [
  Document,
  Text,
  Paragraph,
  Bold,
  Italic,
  HardBreak,
  Redact,
  BlockMeta,
  heading,
  list,
  listItem,
  quote,
  dossierHeader,
  experimentLog,
  logEntry,
  stamp,
  memo,
  memoParagraph,
  clipping,
  clipLine,
  kupolTable,
  TableRow,
  kupolCell,
  kupolHeaderCell,
  docLink,
  divider,
  pageBreak,
  footnote,
  appendix,
  unknownBlock,
]

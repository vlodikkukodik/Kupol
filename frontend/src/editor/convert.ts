// Блоки документа (как их хранит сервер) ⇄ дерево редактора (ProseMirror JSON).
//
// Редактор — только представление: серверный формат `{id, type, level, data}` и форматированный текст
// «фрагментами» `{text, level, bold, italic}` остаются единственной моделью, а вся проверка и серверная
// фильтрация по допуску — на сервере. Преобразование чистое (без Tiptap и DOM), поэтому проверяется точными тестами.
//
// Правила, на которых держится «нет ложных правок»:
//  • обратное преобразование выдаёт ту же каноническую форму, что хранит сервер (пустые необязательные поля не пишутся,
//    значения по умолчанию — заголовок 2, штамп «red», разделитель «line» — пишутся), поэтому открыть документ и ничего
//    не тронуть — не изменение;
//  • соседние фрагменты с одинаковым оформлением склеиваются;
//  • полностью пустые абзацы, пункты списка, абзацы записки и статьи не отправляются (сервер отклонил бы их как «пустые»):
//    в редакторе они остаются, пока автор не начнёт писать;
//  • блок неизвестного вида (из более новой версии сервера) хранится узлом-«коробкой» и возвращается без изменений.
import type { JSONContent } from '@tiptap/core'
import type { InputBlock } from '@/api/generated/documents'

type Node = JSONContent
type Attrs = Record<string, unknown>

/** Фрагмент текста в формате сервера. */
export interface RunData {
  text: string
  level?: number
  bold?: boolean
  italic?: boolean
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)
const str = (v: unknown): string => (typeof v === 'string' ? v : '')
const strList = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : [])
const int = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? Math.trunc(v) : 0)
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : [])

// ————— текст —————

/** Разметка одного фрагмента для редактора. */
function marksOf(run: Record<string, unknown>): NonNullable<Node['marks']> {
  const marks: NonNullable<Node['marks']> = []
  if (run.bold === true) marks.push({ type: 'bold' })
  if (run.italic === true) marks.push({ type: 'italic' })
  const level = int(run.level)
  if (level > 0) marks.push({ type: 'redact', attrs: { level } })
  return marks
}

/** Форматированный текст сервера (строка или массив фрагментов) → вставки редактора. Перевод строки — узел hardBreak. */
export function richToInline(rich: unknown): Node[] {
  const runs: unknown[] = typeof rich === 'string' ? [{ text: rich }] : list(rich)
  const out: Node[] = []
  for (const run of runs) {
    if (!isRecord(run)) continue
    const text = str(run.text)
    if (!text) continue
    const marks = marksOf(run)
    const withMarks = marks.length > 0 ? { marks } : {}
    text.split('\n').forEach((part, i) => {
      if (i > 0) out.push({ type: 'hardBreak', ...withMarks })
      if (part) out.push({ type: 'text', text: part, ...withMarks })
    })
  }
  return out
}

function styleOf(marks: Node['marks']): Omit<RunData, 'text'> {
  const style: Omit<RunData, 'text'> = {}
  for (const m of marks ?? []) {
    if (m.type === 'bold') style.bold = true
    else if (m.type === 'italic') style.italic = true
    else if (m.type === 'redact') {
      const level = int(m.attrs?.level)
      if (level > 0) style.level = level
    }
  }
  return style
}

const sameStyle = (a: Omit<RunData, 'text'>, b: Omit<RunData, 'text'>) =>
  (a.level ?? 0) === (b.level ?? 0) && Boolean(a.bold) === Boolean(b.bold) && Boolean(a.italic) === Boolean(b.italic)

/** Вставки редактора → фрагменты сервера. Соседние фрагменты с одним оформлением склеиваются. */
export function inlineToRuns(content: Node[] | undefined): RunData[] {
  const runs: RunData[] = []
  for (const n of content ?? []) {
    const text = n.type === 'hardBreak' ? '\n' : n.type === 'text' ? (n.text ?? '') : ''
    if (!text) continue
    const style = styleOf(n.marks)
    const prev = runs[runs.length - 1]
    if (prev && sameStyle(prev, style)) prev.text += text
    else runs.push({ text, ...style })
  }
  return runs
}

/** Текст без оформления (заголовки и шапки таблиц — простые строки). */
function plain(content: Node[] | undefined): string {
  return (content ?? []).map((n) => (n.type === 'text' ? (n.text ?? '') : '')).join('')
}

/** Есть ли в тексте хоть один непробельный знак (сервер пустое не принимает). */
export function hasText(runs: RunData[]): boolean {
  return runs.some((r) => r.text.trim() !== '')
}

const withText = (text: string): Node[] => (text ? [{ type: 'text', text }] : [])

/** Необязательная строка: пустая не пишется. */
function optional<T extends object>(key: string, value: string): Partial<T> {
  return (value === '' ? {} : { [key]: value }) as Partial<T>
}

// ————— виды блоков —————

interface Kind {
  /** Имя узла в редакторе */
  node: string
  /** Данные блока сервера → узел редактора (без общих полей id и level) */
  toNode: (data: Record<string, unknown>) => Pick<Node, 'attrs' | 'content'>
  /** Узел редактора → данные блока; null — блок не отправляется (пустой) */
  toData: (node: Node) => Record<string, unknown> | null
}

const children = (node: Node): Node[] => node.content ?? []
const attrsOf = (node: Node): Attrs => node.attrs ?? {}

/** Строки формы с одной вставкой на строку (пункты списка, абзацы записки): пустые не отправляются. */
const nonEmpty = (nodes: Node[]): RunData[][] => nodes.map((n) => inlineToRuns(n.content)).filter(hasText)

const KINDS: Record<string, Kind> = {
  heading: {
    node: 'heading',
    toNode: (d) => ({ attrs: { depth: Math.min(3, Math.max(1, int(d.depth) || 2)) }, content: withText(str(d.text)) }),
    toData: (n) => ({ depth: int(attrsOf(n).depth) || 2, text: plain(n.content) }),
  },

  paragraph: {
    node: 'paragraph',
    toNode: (d) => ({ content: richToInline(d.text) }),
    toData: (n) => {
      const text = inlineToRuns(n.content)
      return hasText(text) ? { text } : null
    },
  },

  list: {
    node: 'list',
    toNode: (d) => {
      const items = list(d.items).map((item): Node => ({ type: 'listItem', content: richToInline(item) }))
      return { attrs: { ordered: d.ordered === true }, content: items.length > 0 ? items : [{ type: 'listItem' }] }
    },
    toData: (n) => {
      const items = nonEmpty(children(n))
      if (items.length === 0) return null
      return { ...(attrsOf(n).ordered === true ? { ordered: true } : {}), items }
    },
  },

  quote: {
    node: 'quote',
    toNode: (d) => ({ attrs: { source: str(d.source) }, content: richToInline(d.text) }),
    toData: (n) => ({ text: inlineToRuns(n.content), ...optional('source', str(attrsOf(n).source)) }),
  },

  dossier_header: {
    node: 'dossierHeader',
    toNode: () => ({}),
    toData: () => ({}),
  },

  experiment_log: {
    node: 'experimentLog',
    toNode: (d) => {
      const entries = list(d.entries).map((e): Node => {
        const entry = isRecord(e) ? e : {}
        return {
          type: 'logEntry',
          attrs: { date: str(entry.date), participants: strList(entry.participants) },
          content: richToInline(entry.text),
        }
      })
      return { attrs: { title: str(d.title) }, content: entries.length > 0 ? entries : [{ type: 'logEntry', attrs: { date: '', participants: [] } }] }
    },
    toData: (n) => ({
      ...optional('title', str(attrsOf(n).title)),
      entries: children(n).map((e) => {
        const a = attrsOf(e)
        const participants = strList(a.participants)
        return {
          ...optional('date', str(a.date)),
          ...(participants.length > 0 ? { participants } : {}),
          text: inlineToRuns(e.content),
        }
      }),
    }),
  },

  stamp: {
    node: 'stamp',
    toNode: (d) => ({ attrs: { text: str(d.text), tone: d.tone === 'ink' ? 'ink' : 'red', tilt: int(d.tilt) } }),
    toData: (n) => {
      const a = attrsOf(n)
      const tilt = int(a.tilt)
      return { text: str(a.text), tone: a.tone === 'ink' ? 'ink' : 'red', ...(tilt !== 0 ? { tilt } : {}) }
    },
  },

  memo: {
    node: 'memo',
    toNode: (d) => {
      const body = list(d.body).map((b): Node => ({ type: 'memoParagraph', content: richToInline(b) }))
      const kind = str(d.kind)
      return {
        attrs: {
          kind: kind === 'order' || kind === 'letter' ? kind : 'memo',
          number: str(d.number),
          date: str(d.date),
          from: str(d.from),
          to: strList(d.to),
          subject: str(d.subject),
          signature: str(d.signature),
        },
        content: body.length > 0 ? body : [{ type: 'memoParagraph' }],
      }
    },
    toData: (n) => {
      const a = attrsOf(n)
      const to = strList(a.to)
      return {
        kind: a.kind === 'order' || a.kind === 'letter' ? a.kind : 'memo',
        ...optional('number', str(a.number)),
        ...optional('date', str(a.date)),
        ...optional('from', str(a.from)),
        ...(to.length > 0 ? { to } : {}),
        ...optional('subject', str(a.subject)),
        body: nonEmpty(children(n)),
        ...optional('signature', str(a.signature)),
      }
    },
  },

  clipping: {
    node: 'clipping',
    toNode: (d) => {
      const kind = str(d.kind)
      const transcript = kind === 'transcript'
      const lines: Node[] = transcript
        ? list(d.lines).map((l): Node => {
            const line = isRecord(l) ? l : {}
            return { type: 'clipLine', attrs: { speaker: str(line.speaker) }, content: richToInline(line.text) }
          })
        : list(d.paragraphs).map((p): Node => ({ type: 'clipLine', attrs: { speaker: '' }, content: richToInline(p) }))
      return {
        attrs: {
          kind: transcript || kind === 'handwritten' ? kind : 'newspaper',
          title: str(d.title),
          source: str(d.source),
          date: str(d.date),
        },
        content: lines.length > 0 ? lines : [{ type: 'clipLine', attrs: { speaker: '' } }],
      }
    },
    toData: (n) => {
      const a = attrsOf(n)
      const kind = a.kind === 'transcript' || a.kind === 'handwritten' ? a.kind : 'newspaper'
      const base = {
        kind,
        ...optional('title', str(a.title)),
        ...optional('source', str(a.source)),
        ...optional('date', str(a.date)),
      }
      if (kind === 'transcript') {
        const lines = children(n)
          .map((c) => ({ ...optional('speaker', str(attrsOf(c).speaker)), text: inlineToRuns(c.content) }))
          .filter((l) => hasText(l.text))
        return { ...base, ...(lines.length > 0 ? { lines } : {}) }
      }
      const paragraphs = nonEmpty(children(n))
      return { ...base, ...(paragraphs.length > 0 ? { paragraphs } : {}) }
    },
  },

  table: {
    node: 'table',
    toNode: (d) => {
      const columns = strList(d.columns)
      const rows = list(d.rows).map((r) => list(r))
      // Ширина — по самой длинной строке: в неверном черновике ничего не теряется, недостающее дополняется пустым.
      const width = Math.max(1, columns.length, ...rows.map((r) => r.length))
      const header: Node = {
        type: 'tableRow',
        content: Array.from({ length: width }, (_, i): Node => ({ type: 'tableHeader', content: withText(columns[i] ?? '') })),
      }
      const body = rows.map((r): Node => ({
        type: 'tableRow',
        content: Array.from({ length: width }, (_, i): Node => ({ type: 'tableCell', content: richToInline(r[i]) })),
      }))
      return { attrs: { caption: str(d.caption) }, content: [header, ...body] }
    },
    toData: (n) => {
      const [head, ...rows] = children(n)
      return {
        ...optional('caption', str(attrsOf(n).caption)),
        columns: children(head ?? {}).map((c) => plain(c.content)),
        rows: rows.map((r) =>
          children(r).map((c) => {
            const runs = inlineToRuns(c.content)
            return runs.length > 0 ? runs : [{ text: '' }] // пустая ячейка допустима, но записывается фрагментом с пустым текстом
          }),
        ),
      }
    },
  },

  doc_link: {
    node: 'docLink',
    toNode: (d) => ({ attrs: { code: str(d.code), note: str(d.note) } }),
    toData: (n) => ({ code: str(attrsOf(n).code), ...optional('note', str(attrsOf(n).note)) }),
  },

  divider: {
    node: 'divider',
    toNode: (d) => ({ attrs: { style: d.style === 'stars' ? 'stars' : 'line' } }),
    toData: (n) => ({ style: attrsOf(n).style === 'stars' ? 'stars' : 'line' }),
  },

  page: {
    node: 'pageBreak',
    toNode: (d) => ({ attrs: { number: str(d.number) } }),
    toData: (n) => optional('number', str(attrsOf(n).number)),
  },

  footnote: {
    node: 'footnote',
    toNode: (d) => ({ attrs: { mark: str(d.mark) }, content: richToInline(d.text) }),
    toData: (n) => ({ mark: str(attrsOf(n).mark), text: inlineToRuns(n.content) }),
  },

  appendix: {
    node: 'appendix',
    toNode: (d) => ({ attrs: { number: str(d.number), title: str(d.title) } }),
    toData: (n) => ({ ...optional('number', str(attrsOf(n).number)), title: str(attrsOf(n).title) }),
  },
}

/** Имя узла редактора → вид блока сервера. */
const KIND_BY_NODE: Record<string, string> = Object.fromEntries(Object.entries(KINDS).map(([type, k]) => [k.node, type]))

/** Виды блоков, которые умеет редактор (порядок — как в меню вставки). */
export const BLOCK_TYPES = Object.keys(KINDS)

/** Имя узла редактора для вида блока сервера; неизвестному виду — узел-«коробка». */
export function nodeNameOf(type: string): string {
  return KINDS[type]?.node ?? 'unknownBlock'
}

/** Имена всех узлов верхнего уровня (по одному на вид блока, плюс «коробка» для неизвестных). */
export const BLOCK_NODE_NAMES = [...Object.values(KINDS).map((k) => k.node), 'unknownBlock']

// ————— документ —————

/** Блоки сервера → документ редактора. Пустой список — один пустой абзац (документ не бывает без блоков). */
export function blocksToDoc(blocks: InputBlock[] | undefined | null): JSONContent {
  const content = list(blocks).flatMap((b): Node[] => {
    if (!isRecord(b)) return []
    const type = str(b.type)
    const meta = { blockId: str(b.id), level: typeof b.level === 'number' ? b.level : null }
    const data = isRecord(b.data) ? b.data : {}
    const kind = KINDS[type]
    if (!kind) return [{ type: 'unknownBlock', attrs: { ...meta, kind: type, data: b.data ?? null } }]
    const { attrs, content: inner } = kind.toNode(data)
    return [{ type: kind.node, attrs: { ...meta, ...attrs }, ...(inner ? { content: inner } : {}) }]
  })
  return { type: 'doc', content: content.length > 0 ? content : [{ type: 'paragraph', attrs: { blockId: '', level: null } }] }
}

/** Документ редактора → блоки сервера (каноническая форма). */
export function docToBlocks(doc: JSONContent | null | undefined): InputBlock[] {
  const out: InputBlock[] = []
  for (const node of doc?.content ?? []) {
    const a = attrsOf(node)
    let type: string
    let data: unknown
    if (node.type === 'unknownBlock') {
      type = str(a.kind)
      data = a.data
    } else {
      const known = node.type ? KIND_BY_NODE[node.type] : undefined
      const kind = known ? KINDS[known] : undefined
      if (!known || !kind) continue
      const d = kind.toData(node)
      if (d === null) continue
      type = known
      data = d
    }
    const block: InputBlock = { id: str(a.blockId), type, data }
    if (typeof a.level === 'number') block.level = a.level
    out.push(block)
  }
  return out
}

/** Блоки в той форме, в какую их приводит редактор: для сравнения «есть ли правки» с сохранённым. */
export function canonicalBlocks(blocks: InputBlock[] | undefined | null): InputBlock[] {
  return docToBlocks(blocksToDoc(blocks))
}

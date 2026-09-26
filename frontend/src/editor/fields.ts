// Поля блоков, которые правятся обычными полями ввода в шапке блока (а не набираются в тексте).
// Один перечень на всё: по нему строится представление узла, по нему же проверяются значения по умолчанию.
// Здесь только устройство полей; подписи — в каталогах языков (раздел editor): editor.field.<узел>.<поле>,
// editor.opt.<узел>.<поле>.<значение>, editor.node.<узел>, editor.insert.<узел>.
import { t } from '@/i18n'

export interface Option {
  value: string
  label: string
}

interface Base {
  /** Имя атрибута узла */
  key: string
  /** Поле на всю ширину */
  wide?: boolean
}

export type FieldSpec =
  | (Base & { kind: 'text'; maxLength?: number })
  | (Base & { kind: 'select'; values: string[] })
  | (Base & { kind: 'number'; min: number; max: number })
  /** Список строк: по одному значению на строке (запятая внутри имени допустима) */
  | (Base & { kind: 'lines' })

/** Узел редактора → перечень полей его шапки. Узлов без полей здесь нет. */
export const FIELDS: Record<string, FieldSpec[]> = {
  heading: [{ key: 'depth', kind: 'select', values: ['1', '2', '3'] }],
  list: [{ key: 'ordered', kind: 'select', values: ['false', 'true'] }],
  quote: [{ key: 'source', kind: 'text', maxLength: 200, wide: true }],
  experimentLog: [{ key: 'title', kind: 'text', maxLength: 200, wide: true }],
  logEntry: [
    { key: 'date', kind: 'text', maxLength: 40 },
    { key: 'participants', kind: 'lines' },
  ],
  stamp: [
    { key: 'text', kind: 'text', maxLength: 40, wide: true },
    { key: 'tone', kind: 'select', values: ['red', 'ink'] },
    { key: 'tilt', kind: 'number', min: -15, max: 15 },
  ],
  memo: [
    { key: 'kind', kind: 'select', values: ['memo', 'order', 'letter'] },
    { key: 'number', kind: 'text', maxLength: 40 },
    { key: 'date', kind: 'text', maxLength: 40 },
    { key: 'from', kind: 'text', maxLength: 200 },
    { key: 'to', kind: 'lines' },
    { key: 'subject', kind: 'text', maxLength: 300, wide: true },
    { key: 'signature', kind: 'text', maxLength: 200, wide: true },
  ],
  clipping: [
    { key: 'kind', kind: 'select', values: ['newspaper', 'handwritten', 'transcript'] },
    { key: 'title', kind: 'text', maxLength: 300 },
    { key: 'source', kind: 'text', maxLength: 200 },
    { key: 'date', kind: 'text', maxLength: 40 },
  ],
  clipLine: [{ key: 'speaker', kind: 'text', maxLength: 100 }],
  table: [{ key: 'caption', kind: 'text', maxLength: 300, wide: true }],
  docLink: [
    { key: 'code', kind: 'text', maxLength: 60 },
    { key: 'note', kind: 'text', maxLength: 300, wide: true },
  ],
  divider: [{ key: 'style', kind: 'select', values: ['line', 'stars'] }],
  pageBreak: [{ key: 'number', kind: 'text', maxLength: 20 }],
  footnote: [{ key: 'mark', kind: 'text', maxLength: 8 }],
  image: [
    { key: 'upload', kind: 'text', maxLength: 32, wide: true },
    { key: 'caption', kind: 'text', maxLength: 300, wide: true },
    { key: 'sticker', kind: 'select', values: ['none', 'frame', 'clip', 'stamp'] },
  ],
  audio: [
    { key: 'upload', kind: 'text', maxLength: 32, wide: true },
    { key: 'title', kind: 'text', maxLength: 200, wide: true },
  ],
  appendix: [
    { key: 'number', kind: 'text', maxLength: 20 },
    { key: 'title', kind: 'text', maxLength: 200, wide: true },
  ],
}

/** Подпись поля узла на текущем языке */
export const fieldLabel = (node: string, key: string): string => t(`editor.field.${node}.${key}`)

/** Варианты выбора поля с подписями на текущем языке */
export const fieldOptions = (node: string, spec: Extract<FieldSpec, { kind: 'select' }>): Option[] =>
  spec.values.map((value) => ({ value, label: t(`editor.opt.${node}.${spec.key}.${value}`) }))

/** Названия узлов редактора (те же виды блоков, что на сервере) */
const NODES = ['heading', 'paragraph', 'list', 'quote', 'dossierHeader', 'experimentLog', 'logEntry', 'stamp', 'memo', 'clipping', 'clipLine', 'table', 'docLink', 'divider', 'pageBreak', 'footnote', 'appendix', 'image', 'audio', 'unknownBlock'] as const

/** Название узла редактора для подписей; незнакомый узел — его собственное имя. */
export const nodeLabel = (node: string): string => ((NODES as readonly string[]).includes(node) ? t(`editor.node.${node}`) : node)

/** Порядок меню «Вставить блок». type — вид блока сервера, node — узел редактора. */
export const INSERTABLE: { type: string; node: string }[] = [
  { type: 'paragraph', node: 'paragraph' },
  { type: 'heading', node: 'heading' },
  { type: 'list', node: 'list' },
  { type: 'quote', node: 'quote' },
  { type: 'dossier_header', node: 'dossierHeader' },
  { type: 'experiment_log', node: 'experimentLog' },
  { type: 'stamp', node: 'stamp' },
  { type: 'memo', node: 'memo' },
  { type: 'clipping', node: 'clipping' },
  { type: 'table', node: 'table' },
  { type: 'doc_link', node: 'docLink' },
  { type: 'divider', node: 'divider' },
  { type: 'page', node: 'pageBreak' },
  { type: 'footnote', node: 'footnote' },
  { type: 'appendix', node: 'appendix' },
  { type: 'image', node: 'image' },
  { type: 'audio', node: 'audio' },
]

/** Подпись и подсказка пункта меню «Вставить блок» */
export const insertLabel = (node: string): string => t(`editor.insert.${node}.label`)
export const insertHint = (node: string): string => t(`editor.insert.${node}.hint`)

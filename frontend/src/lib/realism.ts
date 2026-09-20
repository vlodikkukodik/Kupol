// «Реалистичность архива» (спецификация §7): гриф по уровню, архивный шифр, зачернения разной длины. Чистые функции — их проверяет vitest.
import type { OutBlock, OutDocument } from '@/api/generated/documents'

/**
 * Гриф секретности документа по его уровню допуска — верхний и нижний колонтитул листа. Шкала — от «несекретно» до «особой важности»;
 * уровни Особого Совета и Директората — та же «особая важность» с пометкой, кому открыто.
 */
export const CLASSIFICATION = [
  'Несекретно',
  'Для служебного пользования',
  'Конфиденциально',
  'Секретно',
  'Совершенно секретно',
  'Особой важности',
  'Особой важности · Особый Совет',
  'Особой важности · только Директорат',
] as const

export function classification(level: number): string {
  return CLASSIFICATION[Math.max(0, Math.min(CLASSIFICATION.length - 1, Math.trunc(level) || 0))] ?? CLASSIFICATION[0]
}

/** Фонд по типу документа: как разложены дела в архиве. */
const FONDS: Record<string, number> = { object: 1, order: 2, incident: 3, personnel: 4, unit: 5, protocol: 6, testimony: 7, memo: 8 }

/** Знаков текста на одном листе; «лист» условный — как страница машинописи. */
const CHARS_PER_SHEET = 2800
/** Закрытый блок занимает на листе примерно строку-другую: его настоящий объём читателю неизвестен и не должен утекать. */
const REDACTED_BLOCK_CHARS = 300

function textLength(value: unknown): number {
  if (typeof value === 'string') return value.length
  if (Array.isArray(value)) return value.reduce<number>((n, v) => n + textLength(v), 0)
  if (value && typeof value === 'object') return Object.values(value).reduce<number>((n, v) => n + textLength(v), 0)
  return 0
}

/** Число листов дела: считается только по тому, что читатель видит (закрытое сервер не присылает), плюс явные разрывы страниц. */
export function sheetCount(blocks: OutBlock[]): number {
  let chars = 0
  let breaks = 0
  for (const b of blocks) {
    if (b.type === 'redacted') chars += REDACTED_BLOCK_CHARS
    else if (b.type === 'page') breaks++
    else chars += textLength(b.data)
  }
  return 1 + breaks + Math.floor(chars / CHARS_PER_SHEET)
}

export interface ArchiveMark {
  fond: number
  inventory: number
  file: number
  sheets: number
}

/** Архивный шифр «Фонд · Опись · Дело · Листов»: выводится из типа, года составления, номера в шифре и объёма видимого текста. */
export function archiveMark(doc: Pick<OutDocument, 'type' | 'code' | 'composed' | 'blocks'>): ArchiveMark {
  const numbers = doc.code.match(/\d+/g)
  return {
    fond: FONDS[doc.type] ?? 9,
    inventory: doc.composed.year,
    file: numbers ? Number.parseInt(numbers[numbers.length - 1] ?? '0', 10) : 0,
    sheets: sheetCount(doc.blocks),
  }
}

export const archiveMarkText = (m: ArchiveMark): string => `Фонд ${m.fond} · Опись ${m.inventory} · Дело ${m.file} · Листов ${m.sheets}`

/** «экз. № 0042» или «экз. б/н». */
export const copyText = (copy: string): string => `экз. ${copy === 'б/н' ? copy : `№ ${copy}`}`

function hash(seed: string): number {
  let h = 2166136261
  for (let i = 0; i < seed.length; i++) {
    h ^= seed.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

export const REDACTION_MIN = 3.5
export const REDACTION_MAX = 9

/**
 * Ширина полосы закрытого фрагмента в «ch». Зависит от положения (шифр документа, номер фрагмента, текст перед ним), а не от закрытого
 * текста: полоса выглядит живой, а длина скрытого не раскрывается. Одно и то же место всегда даёт ту же ширину.
 */
export function redactionWidth(seed: string): number {
  const step = 0.5
  const steps = Math.round((REDACTION_MAX - REDACTION_MIN) / step)
  return REDACTION_MIN + (hash(seed) % (steps + 1)) * step
}

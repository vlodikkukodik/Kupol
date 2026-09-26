// Форма документа в панели команды: перевод содержимого с сервера в поля формы и обратно, замечания сервера
// по полям, сравнение «есть ли несохранённые правки». Правила проверки — на сервере; здесь только то, без чего
// запрос нельзя даже составить (год — число).
import type { Content, InputBlock, Problem, Props, Translation } from '@/api/generated/documents'
import { canonicalBlocks } from '@/editor/convert'
import { t } from '@/i18n'

export const PROP_FIELDS = ['danger_class', 'deviation_points', 'department', 'category', 'containment_status', 'discovery_place'] as const
export type PropField = (typeof PROP_FIELDS)[number]
const NUMERIC_PROPS = new Set<PropField>(['danger_class', 'deviation_points'])

export type PropsForm = Record<PropField, string>

/** Поля формы: все значения — строки, как в полях ввода. */
export interface DocForm {
  title: string
  level: string
  direct_link: string
  grif: string
  year: string
  month: string
  day: string
  /** Свойства Объекта; у других типов документов их нет */
  props: PropsForm | null
  blocks: InputBlock[]
}

const str = (v: unknown): string => (v === null || v === undefined ? '' : String(v))

/** Перевод дела в форме редактора блоков: заголовок-строка и блоки, как у основной формы. */
export interface TranslationForm {
  title: string
  blocks: InputBlock[]
}

const emptyTranslationForm = (): TranslationForm => ({ title: '', blocks: [] })

/** Поля вкладки перевода из содержимого документа. */
export function translationFormFromContent(content: Partial<Content> | null | undefined): TranslationForm {
  const it = content?.it
  if (!it) return emptyTranslationForm()
  return { title: str(it.title), blocks: canonicalBlocks(it.blocks) }
}

/** Перевод для отправки на сервер: пустая вкладка (нет заголовка и блоков) значит «перевода нет». */
export function translationFromForm(form: TranslationForm): Translation | undefined {
  const blocks = canonicalBlocks(form.blocks)
  if (form.title.trim() === '' && blocks.length === 0) return undefined
  return { title: form.title, blocks }
}

/**
 * Содержимое в той форме, в какой его ведёт редактор блоков (склеенные фрагменты, без пустых абзацев).
 * Сохранённое содержимое приводится к ней перед сравнением: иначе «есть правки» вспыхивало бы у документа,
 * который автор не трогал.
 */
export function canonicalContent(content: Content): Content {
  const out: Content = { ...content, blocks: canonicalBlocks(content.blocks) }
  if (content.it) out.it = { title: content.it.title, blocks: canonicalBlocks(content.it.blocks) }
  return out
}

/** Поля формы из содержимого документа. */
export function formFromContent(content: Partial<Content> | null | undefined, type: string): DocForm {
  const c = content ?? {}
  const composed = c.composed
  const props = Object.fromEntries(PROP_FIELDS.map((k) => [k, str(c.props?.[k])])) as PropsForm
  return {
    title: str(c.title),
    level: str(c.level ?? 0),
    direct_link: c.direct_link || 'not_found',
    grif: str(c.grif),
    year: str(composed?.year),
    month: str(composed?.month),
    day: str(composed?.day),
    props: type === 'object' ? props : null,
    blocks: canonicalBlocks(c.blocks),
  }
}

/** Целое число из строки поля: '' — нет значения; мусор — NaN. */
function toInt(s: string): number | null {
  const trimmed = String(s).trim()
  if (trimmed === '') return null
  return /^-?\d+$/.test(trimmed) ? Number(trimmed) : Number.NaN
}

/**
 * Содержимое для отправки на сервер. errors — то, что нельзя отправить (не число там, где нужно число);
 * пути — как у замечаний сервера, чтобы показать их у тех же полей.
 * strict=false (автосохранение): неверные числа просто не отправляются — человек ещё пишет.
 */
export function contentFromForm(
  form: DocForm,
  { strict = true, it }: { strict?: boolean; it?: TranslationForm } = {},
): { content: Content; errors: Record<string, string> } {
  const errors: Record<string, string> = {}
  const num = (path: string, raw: string, label: string): number | null => {
    const n = toInt(raw)
    if (Number.isNaN(n)) {
      if (strict) errors[path] = t('teamdoc.integer', { label })
      return null
    }
    return n
  }
  const content: Content = { title: form.title, blocks: form.blocks }

  const level = num('level', form.level, t('teamdoc.level'))
  if (level !== null) content.level = level
  if (form.direct_link) content.direct_link = form.direct_link
  if (form.grif.trim() !== '') content.grif = form.grif

  const year = num('composed.year', form.year, t('teamdoc.year'))
  const month = num('composed.month', form.month, t('teamdoc.month'))
  const day = num('composed.day', form.day, t('teamdoc.day'))
  if (year !== null) {
    content.composed = { year }
    if (month !== null) content.composed.month = month
    if (day !== null) content.composed.day = day
  } else if (strict && (month !== null || day !== null) && !errors['composed.year']) {
    errors['composed.year'] = t('teamdoc.yearFirst')
  }

  if (form.props) {
    const props: Props = {}
    for (const k of PROP_FIELDS) {
      const raw = form.props[k]
      if (NUMERIC_PROPS.has(k)) {
        const n = num(`props.${k}`, raw, k === 'danger_class' ? t('teamdoc.dangerClass') : t('teamdoc.deviation'))
        if (n !== null) (props as Record<string, unknown>)[k] = n
      } else if (raw.trim() !== '') {
        ;(props as Record<string, unknown>)[k] = raw
      }
    }
    if (Object.keys(props).length > 0) content.props = props
  }
  if (it) {
    const translation = translationFromForm(it)
    if (translation) content.it = translation
  }
  return { content, errors }
}

/** Пути полей формы; замечания к остальным путям (блоки и прочее) показываются общим списком. */
const FORM_PATHS = new Set<string>([
  'title', 'level', 'direct_link', 'grif', 'composed', 'composed.year', 'composed.month', 'composed.day', 'code', 'type', 'props',
  'it.title',
  ...PROP_FIELDS.map((k) => `props.${k}`),
])

/** Разложить замечания сервера: у полей формы и общим списком. Несколько замечаний к одному полю склеиваются. */
export function problemsToFields(problems: Problem[] | undefined): { byPath: Record<string, string>; other: Problem[] } {
  const byPath: Record<string, string> = {}
  const other: Problem[] = []
  for (const p of problems ?? []) {
    if (FORM_PATHS.has(p.path)) byPath[p.path] = byPath[p.path] ? `${byPath[p.path]}; ${p.message}` : p.message
    else other.push(p)
  }
  return { byPath, other }
}

/** Запись для сравнения: ключи по алфавиту, пустые значения (null, '', пустые объекты) не в счёт. */
function stable(v: unknown): unknown {
  if (Array.isArray(v)) return v.map(stable)
  if (v && typeof v === 'object') {
    const src = v as Record<string, unknown>
    const out: Record<string, unknown> = {}
    for (const k of Object.keys(src).sort()) {
      const s = stable(src[k])
      if (s === null || s === undefined || s === '') continue
      if (typeof s === 'object' && !Array.isArray(s) && Object.keys(s).length === 0) continue
      out[k] = s
    }
    return out
  }
  return v
}

/** Одинаково ли по смыслу два содержимых (для «есть несохранённые правки»). Уровень 0 и его отсутствие — одно и то же. */
export function sameContent(a: Partial<Content> | null | undefined, b: Partial<Content> | null | undefined): boolean {
  const norm = (c: Partial<Content> | null | undefined) => {
    const s = stable(c ?? {}) as Record<string, unknown>
    if (s.level === 0) delete s.level
    if (s.direct_link === 'not_found') delete s.direct_link
    return JSON.stringify(s)
  }
  return norm(a) === norm(b)
}

/** Короткий текст блока для списков и сравнения версий: все строки из данных подряд. */
export function blockPreview(block: Pick<InputBlock, 'data'> | null | undefined, max = 120): string {
  const parts: string[] = []
  const walk = (v: unknown): void => {
    if (parts.join(' ').length > max) return
    if (typeof v === 'string') {
      if (v.trim()) parts.push(v.trim())
    } else if (Array.isArray(v)) v.forEach(walk)
    else if (v && typeof v === 'object') Object.values(v).forEach(walk)
  }
  walk(block?.data)
  const text = parts.join(' ').replace(/\s+/g, ' ')
  return text.length > max ? `${text.slice(0, max - 1)}…` : text
}

/** Значение поля документа в строке сравнения версий. */
export function describeValue(v: unknown): string {
  if (v === null || v === undefined || v === '') return '—'
  return String(v)
}

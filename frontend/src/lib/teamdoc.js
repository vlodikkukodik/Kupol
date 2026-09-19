// Форма документа в панели команды: перевод содержимого с сервера в поля формы и обратно, замечания сервера
// по полям, сравнение «есть ли несохранённые правки». Правила проверки — на сервере; здесь только то, без чего
// запрос нельзя даже составить (год — число).

export const PROP_FIELDS = ['danger_class', 'deviation_points', 'department', 'category', 'containment_status', 'discovery_place']
const NUMERIC_PROPS = new Set(['danger_class', 'deviation_points'])

const str = (v) => (v === null || v === undefined ? '' : String(v))

/** Поля формы из содержимого документа (все значения — строки, как в полях ввода). */
export function formFromContent(content, type) {
  const c = content || {}
  const composed = c.composed || {}
  const form = {
    title: str(c.title),
    level: str(c.level ?? 0),
    direct_link: c.direct_link || 'not_found',
    grif: str(c.grif),
    year: str(composed.year),
    month: str(composed.month),
    day: str(composed.day),
    props: Object.fromEntries(PROP_FIELDS.map((k) => [k, str(c.props?.[k])])),
    blocks: Array.isArray(c.blocks) ? c.blocks : [],
  }
  if (type !== 'object') form.props = null
  return form
}

/** Целое число из строки поля: '' — нет значения; мусор — NaN. */
function toInt(s) {
  const t = String(s).trim()
  if (t === '') return null
  return /^-?\d+$/.test(t) ? Number(t) : Number.NaN
}

/**
 * Содержимое для отправки на сервер. errors — то, что нельзя отправить (не число там, где нужно число);
 * пути — как у замечаний сервера, чтобы показать их у тех же полей.
 * strict=false (автосохранение): неверные числа просто не отправляются — человек ещё пишет.
 */
export function contentFromForm(form, { strict = true } = {}) {
  const errors = {}
  const num = (path, raw, label) => {
    const n = toInt(raw)
    if (Number.isNaN(n)) {
      if (strict) errors[path] = `${label}: нужно целое число`
      return null
    }
    return n
  }
  const content = { title: form.title, blocks: form.blocks }

  const level = num('level', form.level, 'Допуск')
  if (level !== null) content.level = level
  if (form.direct_link) content.direct_link = form.direct_link
  if (form.grif.trim() !== '') content.grif = form.grif

  const year = num('composed.year', form.year, 'Год')
  const month = num('composed.month', form.month, 'Месяц')
  const day = num('composed.day', form.day, 'День')
  if (year !== null) {
    content.composed = { year }
    if (month !== null) content.composed.month = month
    if (day !== null) content.composed.day = day
  } else if (strict && (month !== null || day !== null) && !errors['composed.year']) {
    errors['composed.year'] = 'Укажите год: без года месяц и день не сохраняются'
  }

  if (form.props) {
    const props = {}
    for (const k of PROP_FIELDS) {
      const raw = form.props[k]
      if (NUMERIC_PROPS.has(k)) {
        const n = num(`props.${k}`, raw, k === 'danger_class' ? 'Класс опасности' : 'Пункты отклонения')
        if (n !== null) props[k] = n
      } else if (String(raw).trim() !== '') {
        props[k] = raw
      }
    }
    if (Object.keys(props).length > 0) content.props = props
  }
  return { content, errors }
}

/** Пути полей формы; замечания к остальным путям (блоки и прочее) показываются общим списком. */
const FORM_PATHS = new Set([
  'title', 'level', 'direct_link', 'grif', 'composed', 'composed.year', 'composed.month', 'composed.day', 'code', 'type', 'props',
  ...PROP_FIELDS.map((k) => `props.${k}`),
])

/** Разложить замечания сервера: у полей формы и общим списком. Несколько замечаний к одному полю склеиваются. */
export function problemsToFields(problems) {
  const byPath = {}
  const other = []
  for (const p of problems || []) {
    if (FORM_PATHS.has(p.path)) byPath[p.path] = byPath[p.path] ? `${byPath[p.path]}; ${p.message}` : p.message
    else other.push(p)
  }
  return { byPath, other }
}

/** Запись для сравнения: ключи по алфавиту, пустые значения (null, '', пустые объекты) не в счёт. */
function stable(v) {
  if (Array.isArray(v)) return v.map(stable)
  if (v && typeof v === 'object') {
    const out = {}
    for (const k of Object.keys(v).sort()) {
      const s = stable(v[k])
      if (s === null || s === undefined || s === '') continue
      if (typeof s === 'object' && !Array.isArray(s) && Object.keys(s).length === 0) continue
      out[k] = s
    }
    return out
  }
  return v
}

/** Одинаково ли по смыслу два содержимых (для «есть несохранённые правки»). Уровень 0 и его отсутствие — одно и то же. */
export function sameContent(a, b) {
  const norm = (c) => {
    const s = stable(c || {})
    if (s.level === 0) delete s.level
    if (s.direct_link === 'not_found') delete s.direct_link
    return JSON.stringify(s)
  }
  return norm(a) === norm(b)
}

/** Короткий текст блока для списков и сравнения версий: все строки из данных подряд. */
export function blockPreview(block, max = 120) {
  const parts = []
  const walk = (v) => {
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
export function describeValue(v) {
  if (v === null || v === undefined || v === '') return '—'
  return String(v)
}

import { afterEach, describe, expect, it } from 'vitest'
import {
  DEFAULT_LOCALE,
  formatBytes,
  getLocale,
  isMessageKey,
  pickLocale,
  resolveMessage,
  setLocale,
  t,
  translate,
} from './index'
import { it as itCatalog } from './it'
import { ru } from './ru'

// Плоский список «путь → текст» для сравнения каталогов.
function flatten(obj: unknown, prefix = ''): Record<string, string> {
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (typeof v === 'string') out[path] = v
    else Object.assign(out, flatten(v, path))
  }
  return out
}

const flatRu = flatten(ru)
const flatIt = flatten(itCatalog)
const placeholders = (s: string) => [...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort()
const hasCyrillic = (s: string) => /\p{Script=Cyrillic}/u.test(s)

// Строки, которые совпадают на обоих языках по смыслу: названия, технические термины, шаблоны.
const SAME_IN_BOTH = new Set([
  'auth.register.email',
  'dashboard.stats.https',
  'dashboard.account.email',
  'dashboard.account.quotasValue',
  'sites.ftp.title',
  'siteArea.ftp',
  'logs.cols.ip',
  'sites.cert.active',
  'sites.namePlaceholder',
  'sites.domains.placeholder', // пример домена не переводится
  'lang.ru', // эндонимы: название языка пишется на нём самом
  'lang.it',
])

describe('каталоги ru и it', () => {
  it('содержат одни и те же ключи', () => {
    expect(Object.keys(flatIt).sort()).toEqual(Object.keys(flatRu).sort())
  })

  it('используют одинаковые подстановки {name}', () => {
    for (const key of Object.keys(flatRu)) {
      expect(placeholders(flatIt[key] ?? ''), `подстановки в «${key}»`).toEqual(placeholders(flatRu[key] ?? ''))
    }
  })

  it('не содержат пустых текстов', () => {
    for (const [key, v] of [...Object.entries(flatRu), ...Object.entries(flatIt)]) expect(v.trim(), key).not.toBe('')
  })

  it('итальянский каталог без кириллицы (кроме названия русского языка), русский — с кириллицей', () => {
    for (const [key, v] of Object.entries(flatIt)) {
      if (key === 'lang.ru') continue
      expect(hasCyrillic(v), `кириллица в it: ${key} = ${v}`).toBe(false)
    }
    for (const [key, v] of Object.entries(flatRu)) {
      if (SAME_IN_BOTH.has(key)) continue
      expect(hasCyrillic(v), `в ru нет кириллицы: ${key} = ${v}`).toBe(true)
    }
  })

  it('не оставляют непереведённых копий: одинаковый текст допустим только для перечисленных ключей', () => {
    for (const key of Object.keys(flatRu)) {
      if (flatRu[key] === flatIt[key]) expect(SAME_IN_BOTH.has(key), `«${key}» одинаков в ru и it`).toBe(true)
    }
  })
})

describe('pickLocale', () => {
  it.each([
    [null, [], 'ru'], // нечего сравнивать — по умолчанию
    [null, ['it-IT', 'en'], 'it'],
    [null, ['en-US', 'it', 'ru'], 'it'], // первый поддерживаемый в порядке предпочтений браузера
    [null, ['en-US', 'ru-RU', 'it'], 'ru'],
    [null, ['de', 'fr'], 'ru'], // неподдерживаемые языки — по умолчанию
    ['it', ['ru-RU'], 'it'], // сохранённый выбор важнее браузера
    ['ru', ['it-IT'], 'ru'],
    ['de', ['it-IT'], 'it'], // мусор в хранилище игнорируется
    ['', ['IT-it'], 'it'],
    [undefined, ['IT'], 'it'],
  ] as const)('сохранено=%j, браузер=%j → %s', (saved, langs, want) => {
    expect(pickLocale(saved, langs)).toBe(want)
  })

  it('язык по умолчанию — русский', () => expect(DEFAULT_LOCALE).toBe('ru'))
})

describe('translate / t', () => {
  afterEach(() => setLocale('ru', false))

  it('подставляет параметры и оставляет неизвестные на виду', () => {
    expect(translate('ru', 'dashboard.greeting', { name: 'Влад' })).toBe('Здравствуйте, Влад')
    expect(translate('it', 'dashboard.greeting', { name: 'Vlad' })).toBe('Ciao, Vlad')
    expect(translate('it', 'dashboard.greeting')).toBe('Ciao, {name}')
  })

  it('t() следует за текущим языком', () => {
    setLocale('ru', false)
    expect(t('nav.logout')).toBe('Выйти')
    setLocale('it', false)
    expect(getLocale()).toBe('it')
    expect(t('nav.logout')).toBe('Esci')
  })

  it('resolveMessage переводит ключи и не трогает готовый текст сервера', () => {
    setLocale('it', false)
    expect(isMessageKey('validation.email')).toBe(true)
    expect(resolveMessage('validation.email')).toBe('Email non valida')
    expect(resolveMessage('Nome utente o password errati')).toBe('Nome utente o password errati')
    expect(isMessageKey('validation')).toBe(false) // раздел — не ключ
    expect(isMessageKey('')).toBe(false)
  })
})

describe('formatBytes', () => {
  it.each([
    [0, '0 Б', '0 B'],
    [1023, '1 023 Б', '1023 B'],
    [1024, '1 КБ', '1 KB'],
    [1536, '1,5 КБ', '1,5 KB'],
    [10 * 1024, '10 КБ', '10 KB'],
    [500 * 1024 * 1024, '500 МБ', '500 MB'],
    [3 * 1024 ** 3, '3 ГБ', '3 GB'],
  ])('%d', (n, ruWant, itWant) => {
    // Разделитель тысяч в ru — неразрывный пробел; сравниваем без учёта вида пробелов.
    const norm = (s: string) => s.replace(/\s/g, ' ')
    expect(norm(formatBytes(n, 'ru'))).toBe(norm(ruWant))
    expect(norm(formatBytes(n, 'it'))).toBe(norm(itWant))
  })
})

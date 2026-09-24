// i18n панели: русский и итальянский, автоопределение языка, переключатель в шапке.
// Собственная небольшая реализация вместо vue-i18n: ключи проверяются компилятором (несуществующий ключ
// не соберётся), а сборка не требует eval и потому работает под строгим CSP панели.
import { computed, ref } from 'vue'
import { it } from './it'
import { ru, type Messages } from './ru'

export type Locale = 'ru' | 'it'
export const LOCALES: readonly Locale[] = ['ru', 'it']
export const DEFAULT_LOCALE: Locale = 'ru'

const STORAGE_KEY = 'vh-lang'
const catalogs: Record<Locale, Messages> = { ru, it }

type Paths<T, P extends string = ''> = {
  [K in keyof T & string]: T[K] extends string ? `${P}${K}` : Paths<T[K], `${P}${K}.`>
}[keyof T & string]

/** Допустимые ключи каталога, например 'sites.ftp.enable'. */
export type MessageKey = Paths<typeof ru>

export type Params = Record<string, string | number>

const isLocale = (v: unknown): v is Locale => typeof v === 'string' && (LOCALES as readonly string[]).includes(v)

/**
 * Выбор языка: сохранённый выбор пользователя → первый поддерживаемый язык из настроек браузера
 * (порядок в navigator.languages — это приоритет пользователя) → русский по умолчанию.
 * Чистая функция: то, что читается из браузера, передаётся параметрами.
 */
export function pickLocale(saved: string | null | undefined, languages: readonly string[]): Locale {
  if (isLocale(saved)) return saved
  for (const tag of languages) {
    const primary = tag.toLowerCase().split('-')[0]
    if (isLocale(primary)) return primary
  }
  return DEFAULT_LOCALE
}

function readSaved(): string | null {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null // приватный режим или запрещённое хранилище — просто не запоминаем
  }
}

function browserLanguages(): readonly string[] {
  if (typeof navigator === 'undefined') return []
  return navigator.languages?.length ? navigator.languages : navigator.language ? [navigator.language] : []
}

const current = ref<Locale>(pickLocale(readSaved(), browserLanguages()))
/** true, пока пользователь ни разу не выбирал язык сам: тогда показываем подсказку про автовыбор. */
const auto = ref(!isLocale(readSaved()))

export function getLocale(): Locale {
  return current.value
}

function lookup(locale: Locale, key: string): string | undefined {
  let node: unknown = catalogs[locale]
  for (const part of key.split('.')) {
    if (node === null || typeof node !== 'object') return undefined
    node = (node as Record<string, unknown>)[part]
  }
  return typeof node === 'string' ? node : undefined
}

/** Является ли строка ключом каталога (так отличают ключ от готового текста сервера). */
export function isMessageKey(s: string): s is MessageKey {
  return lookup('ru', s) !== undefined
}

function interpolate(template: string, params?: Params): string {
  if (!params) return template
  return template.replace(/\{(\w+)\}/g, (whole, name: string) => (name in params ? String(params[name]) : whole))
}

/** Перевод по ключу для явной локали (нужен там, где нет реактивности, и в тестах). */
export function translate(locale: Locale, key: MessageKey, params?: Params): string {
  return interpolate(lookup(locale, key) ?? lookup(DEFAULT_LOCALE, key) ?? key, params)
}

/** Перевод по ключу для текущего языка. Читает реактивное состояние, поэтому шаблоны обновляются при смене языка. */
export function t(key: MessageKey, params?: Params): string {
  return translate(current.value, key, params)
}

/** Ключ → перевод, всё остальное (готовый текст, например ответ сервера на нужном языке) → как есть. */
export function resolveMessage(message: string): string {
  return isMessageKey(message) ? t(message) : message
}

export function setLocale(locale: Locale, persist = true): void {
  current.value = locale
  if (persist) {
    auto.value = false
    try {
      localStorage.setItem(STORAGE_KEY, locale)
    } catch {
      /* без хранилища выбор действует до перезагрузки */
    }
  }
  applyLocale()
}

/** Отражает язык в документе: атрибут lang (скринридеры, переносы, шрифты) и заголовок вкладки. */
export function applyLocale(): void {
  if (typeof document === 'undefined') return
  document.documentElement.lang = current.value
  document.title = t('app.title')
}

/** Подхватывает смену языка в другой вкладке. */
export function watchStorage(): void {
  window.addEventListener('storage', (e) => {
    if (e.key === STORAGE_KEY && isLocale(e.newValue) && e.newValue !== current.value) setLocale(e.newValue, false)
  })
}

// --- форматирование по локали ---

export function formatDateTime(iso: string, locale: Locale = current.value): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))
}

/** Момент события в журнале: с секундами. */
export function formatTimestamp(iso: string, locale: Locale = current.value): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(iso))
}

/** Размер: 1,5 МБ / 1,5 MB (запятая в обоих языках). */
export function formatBytes(n: number, locale: Locale = current.value): string {
  const num = (v: number) => new Intl.NumberFormat(locale, { maximumFractionDigits: v >= 10 ? 0 : 1 }).format(v)
  if (n < 1024) return `${num(n)} ${translate(locale, 'units.b')}`
  const units = ['units.kb', 'units.mb', 'units.gb'] as const
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${num(v)} ${translate(locale, units[i] as (typeof units)[number])}`
}

export function useI18n() {
  return {
    t,
    locale: computed(() => current.value),
    auto: computed(() => auto.value),
    setLocale,
    locales: LOCALES,
    formatDateTime: (iso: string) => formatDateTime(iso),
    formatBytes: (n: number) => formatBytes(n),
  }
}

export function escapeHtml(s: string): string

export interface PageMeta {
  title: string
  description: string
  url: string
  /** Готовый HTML (уже экранированный) для #app */
  body: string
  ogType?: string
  /** Язык страницы: <html lang>; по умолчанию ru */
  lang?: string
  /** og:locale: ru_RU, it_IT */
  ogLocale?: string
  /** Название сайта для og:site_name */
  siteName?: string
  /** Та же страница на других языках (hreflang) */
  alternates?: { lang: string; url: string }[]
}

export const PRERENDER_LANGS: { code: string; ogLocale: string; suffix: string; query: string }[]

/** Подставляет заголовок, описание, og:-теги и содержимое #app в index.html; нет ожидаемого места — ошибка. */
export function buildPage(html: string, meta: PageMeta): string

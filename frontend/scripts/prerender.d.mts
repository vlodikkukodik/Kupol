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
  /** Картинка превью (og:image/twitter:image); без неё — twitter:card=summary без картинки */
  image?: { url: string; width: number; height: number }
  /** Структурированные данные — вставляются как <script type="application/ld+json"> */
  jsonLd?: Record<string, unknown>
}

export const PRERENDER_LANGS: { code: string; ogLocale: string; suffix: string; query: string; ogImage: string }[]

/** Подставляет заголовок, описание, og:-теги (в т.ч. картинку) и содержимое #app в index.html; нет ожидаемого места — ошибка. */
export function buildPage(html: string, meta: PageMeta): string

export function escapeHtml(s: string): string

export interface PageMeta {
  title: string
  description: string
  url: string
  /** Готовый HTML (уже экранированный) для #app */
  body: string
  ogType?: string
}

/** Подставляет заголовок, описание, og:-теги и содержимое #app в index.html; нет ожидаемого места — ошибка. */
export function buildPage(html: string, meta: PageMeta): string

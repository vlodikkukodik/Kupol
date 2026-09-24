import type { MessageKey } from '@/i18n'

// Пути внутри сайта: без ведущего слеша, корень — пустая строка (как на сервере, но без ".").

export function joinPath(dir: string, name: string): string {
  return dir ? `${dir}/${name}` : name
}

export function parentPath(p: string): string {
  const i = p.lastIndexOf('/')
  return i < 0 ? '' : p.slice(0, i)
}

export function baseName(p: string): string {
  return p.slice(p.lastIndexOf('/') + 1)
}

export interface Crumb {
  name: string
  path: string
}

/** Хлебные крошки: [корень, a, a/b, ...]. */
export function breadcrumbs(p: string, rootName: string): Crumb[] {
  const out: Crumb[] = [{ name: rootName, path: '' }]
  let acc = ''
  for (const seg of p.split('/').filter(Boolean)) {
    acc = joinPath(acc, seg)
    out.push({ name: seg, path: acc })
  }
  return out
}

/** Проверка имени файла/папки до отправки на сервер (сервер проверяет повторно). Возвращает ключ текста ошибки. */
export function validateName(name: string): MessageKey | null {
  const n = name.trim()
  if (!n) return 'validation.nameRequired'
  if (n === '.' || n === '..') return 'validation.nameInvalid'
  if (/[\\/\0]/.test(n)) return 'validation.nameSlash'
  if (n.length > 255) return 'validation.nameTooLong'
  return null
}

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

/** Проверка имени файла/папки до отправки на сервер (сервер проверяет повторно). */
export function validateName(name: string): string | null {
  const n = name.trim()
  if (!n) return 'Введите имя'
  if (n === '.' || n === '..') return 'Недопустимое имя'
  if (/[\\/\0]/.test(n)) return 'Имя не должно содержать / и \\'
  if (n.length > 255) return 'Слишком длинное имя'
  return null
}

// Каталоги сообщений: по файлам на область (shell, account, catalog, …), у каждого языка свой набор файлов с теми же ключами
// (это проверяет tests/i18n.test.ts). Файл отдаёт объект с разделами верхнего уровня («app», «catalog», …); имя раздела — первый
// сегмент ключа: t('catalog.title'). Один раздел не может жить в двух файлах.
import type { Locale } from '../index'

type Tree = { [key: string]: string | Tree }
const modules = import.meta.glob<{ default: Tree }>('./*/*.ts', { eager: true })

function collect(lang: Locale): Tree {
  const out: Tree = {}
  for (const [path, mod] of Object.entries(modules)) {
    const m = /^\.\/([a-z]+)\/([\w-]+)\.ts$/.exec(path)
    if (!m || m[1] !== lang) continue
    for (const [section, value] of Object.entries(mod.default)) {
      if (section in out) throw new Error(`i18n: раздел «${section}» есть в двух файлах языка ${lang} (${path})`)
      out[section] = value
    }
  }
  return out
}

export const messages = { ru: collect('ru'), it: collect('it') }

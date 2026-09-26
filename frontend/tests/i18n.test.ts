// Сторож интернационализации: каталоги языков совпадают, каждый ключ из кода существует, а «просто текста» в коде не осталось.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { LOCALES, pickFromBrowser, i18n, t, tc, setLocale } from '@/i18n'
import { messages } from '@/i18n/messages'

const SRC = resolve(import.meta.dirname, '../src')

type Tree = { [key: string]: string | Tree }

function flatten(tree: Tree, prefix = ''): Map<string, string> {
  const out = new Map<string, string>()
  for (const [k, v] of Object.entries(tree)) {
    const key = prefix ? `${prefix}.${k}` : k
    if (typeof v === 'string') out.set(key, v)
    else for (const [kk, vv] of flatten(v, key)) out.set(kk, vv)
  }
  return out
}

const placeholders = (s: string): string[] => [...new Set([...s.matchAll(/\{(\w+)\}/g)].map((m) => m[1] ?? ''))].sort()

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else out.push(p)
  }
  return out
}

/** Убирает комментарии: русские пояснения в коде — не тексты интерфейса */
function stripComments(file: string, src: string): string {
  let s = src.replace(/<!--[\s\S]*?-->/g, '')
  s = s.replace(/\/\*[\s\S]*?\*\//g, '')
  s = s.replace(/(^|[^:'"`\\])\/\/.*$/gm, '$1')
  if (file.endsWith('.css')) return s
  return s
}

const ru = flatten(messages.ru as Tree)
const it_ = flatten(messages.it as Tree)

describe('каталоги языков', () => {
  it('в проекте два языка: русский и итальянский', () => {
    expect([...LOCALES]).toEqual(['ru', 'it'])
  })

  it('набор ключей русского и итальянского совпадает', () => {
    const onlyRu = [...ru.keys()].filter((k) => !it_.has(k))
    const onlyIt = [...it_.keys()].filter((k) => !ru.has(k))
    expect({ onlyRu, onlyIt }).toEqual({ onlyRu: [], onlyIt: [] })
  })

  it('подстановки {имя} в переводах те же, что в русском', () => {
    const bad: string[] = []
    for (const [k, v] of ru) {
      const other = it_.get(k)
      if (other !== undefined && placeholders(v).join() !== placeholders(other).join()) bad.push(`${k}: ${placeholders(v)} ≠ ${placeholders(other)}`)
    }
    expect(bad).toEqual([])
  })

  it('пустых сообщений нет; в итальянском нет кириллицы', () => {
    const empty = [...ru].filter(([, v]) => v.trim() === '').map(([k]) => k)
    expect(empty).toEqual([])
    // исключение — названия языков на самих языках («Русский» в итальянском интерфейсе): lang.*
    const cyr = [...it_].filter(([k, v]) => !k.startsWith('lang.') && /[А-Яа-яЁё]/.test(v)).map(([k]) => k)
    expect(cyr).toEqual([])
  })

  it('итальянский не копирует русский: сообщение, где есть слова, переведено', () => {
    // допустимы названия и знаки: без подстановок {имя} слов из букв меньше двух — ничего переводить не нужно
    const words = (s: string) => s.replace(/\{\w+\}/g, '')
    const same = [...it_].filter(([k, v]) => !k.startsWith('lang.') && ru.get(k) === v && /[A-Za-zА-Яа-я]{2,}/.test(words(v))).map(([k]) => k)
    expect(same).toEqual([])
  })
})

describe('сообщения собираются', () => {
  // каждое сообщение обоих языков проходит через настоящий переводчик: разбор синтаксиса, подстановки, множественное число
  for (const lang of LOCALES) {
    it(`[${lang}] нет сообщений, которые не разбираются или теряют подстановки`, () => {
      const flat = lang === 'ru' ? ru : it_
      const bad: string[] = []
      for (const [key, text] of flat) {
        const named = Object.fromEntries(placeholders(text).map((p) => [p, 'X']))
        const plural = text.includes(' | ')
        let out = ''
        try {
          out = i18n.global.t(key, named, { locale: lang, ...(plural ? { plural: 2 } : {}) }) as string
        } catch (err) {
          bad.push(`${key}: ${String(err)}`)
          continue
        }
        if (out === key || /[{}]/.test(out) || (!plural && out.includes(' | '))) bad.push(`${key}: «${out}»`)
      }
      expect(bad).toEqual([])
    })
  }
})

describe('ключи в коде', () => {
  const files = walk(SRC).filter((f) => /\.(vue|ts)$/.test(f) && !f.includes('/i18n/') && !f.includes('/generated/'))
  const keyRe = /(?:\$t|\bt|\btc|\bte)\(\s*['"`]([\w-]+(?:\.[\w-]+)+)['"`]|keypath=["']([\w.-]+)["']/g

  it('каждый ключ, написанный в коде буквально, есть в каталоге', () => {
    const missing: string[] = []
    for (const f of files) {
      const src = readFileSync(f, 'utf8')
      for (const m of src.matchAll(keyRe)) {
        const key = m[1] ?? m[2] ?? ''
        if (!ru.has(key)) missing.push(`${relative(SRC, f)}: ${key}`)
      }
    }
    expect(missing).toEqual([])
  })

  it('русского текста в коде нет: всё через каталоги (комментарии не в счёт)', () => {
    const offenders: string[] = []
    for (const f of walk(SRC)) {
      if (f.includes('/i18n/') || f.includes('/generated/')) continue
      if (!/\.(vue|ts|css)$/.test(f)) continue
      const src = stripComments(f, readFileSync(f, 'utf8'))
      src.split('\n').forEach((line, i) => {
        if (/[А-Яа-яЁё]/.test(line)) offenders.push(`${relative(SRC, f)}:${i + 1}: ${line.trim().slice(0, 90)}`)
      })
    }
    expect(offenders).toEqual([])
  })
})

describe('выбор языка', () => {
  it('по списку предпочтений браузера', () => {
    expect(pickFromBrowser(['it-IT', 'en'])).toBe('it')
    expect(pickFromBrowser(['it'])).toBe('it')
    expect(pickFromBrowser(['ru-RU', 'it'])).toBe('ru')
    expect(pickFromBrowser(['en-US', 'it'])).toBe('it')
    expect(pickFromBrowser(['en-US', 'de'])).toBe('ru')
    expect(pickFromBrowser([])).toBe('ru')
  })
})

describe('множественное число', () => {
  // ключ добавляется каталогом common; здесь проверяется правило на своём сообщении
  it('русский: 1 / 2–4 / 5–20, 21 — как 1', () => {
    i18n.global.setLocaleMessage('ru', { ...(messages.ru as Tree), _plural: { files: '{n} файл | {n} файла | {n} файлов' } } as never)
    setLocale('ru')
    const forms = [1, 2, 4, 5, 11, 12, 14, 21, 22, 25, 101, 111, 0].map((n) => tc('_plural.files', n))
    expect(forms).toEqual(['1 файл', '2 файла', '4 файла', '5 файлов', '11 файлов', '12 файлов', '14 файлов', '21 файл', '22 файла', '25 файлов', '101 файл', '111 файлов', '0 файлов'])
    expect(t('_plural.files', 1)).toBeTruthy()
  })
})

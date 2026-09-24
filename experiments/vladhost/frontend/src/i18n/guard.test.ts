// Сторож i18n: пользовательский текст живёт только в каталогах. Код разбирается по AST (TypeScript и
// компилятор шаблонов Vue), поэтому комментарии на русском не мешают, а любой русский строковый литерал,
// текст или атрибут шаблона — нарушение.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import ts from 'typescript'
import { parse } from 'vue/compiler-sfc'
import { describe, expect, it } from 'vitest'
import { it as itCatalog } from './it'
import { ru } from './ru'

const SRC = join(__dirname, '..')
const hasCyrillic = (s: string) => /\p{Script=Cyrillic}/u.test(s)

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name)
    return statSync(p).isDirectory() ? walk(p) : [p]
  })
}

// Каталоги содержат тексты по определению; тесты — данные проверок.
const files = walk(SRC).filter(
  (f) => /\.(ts|vue)$/.test(f) && !/\.test\.ts$/.test(f) && !/[\\/]i18n[\\/](ru|it)\.ts$/.test(f),
)

/** Строковые литералы кода (без комментариев). */
function literals(code: string): string[] {
  const sf = ts.createSourceFile('x.ts', code, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS)
  const out: string[] = []
  const visit = (n: ts.Node) => {
    if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) out.push(n.text)
    else if (ts.isTemplateHead(n) || ts.isTemplateMiddle(n) || ts.isTemplateTail(n)) out.push(n.text)
    ts.forEachChild(n, visit)
  }
  visit(sf)
  return out
}

interface Found {
  file: string
  text: string
}

/** Все «значимые» тексты файла: литералы скриптов, текст, атрибуты и выражения шаблонов. */
function texts(file: string): string[] {
  const src = readFileSync(file, 'utf-8')
  if (file.endsWith('.ts')) return literals(src)

  const { descriptor } = parse(src, { filename: file })
  const out: string[] = []
  for (const block of [descriptor.script, descriptor.scriptSetup]) if (block) out.push(...literals(block.content))

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const visit = (node: any) => {
    switch (node.type) {
      case 1: // элемент
        for (const p of node.props ?? []) {
          if (p.type === 6 && p.value) out.push(p.value.content) // обычный атрибут
          if (p.type === 7 && p.exp?.content) out.push(...literals(p.exp.content)) // v-bind, v-if, @click…
        }
        break
      case 2: // текст
        out.push(node.content)
        break
      case 5: // {{ выражение }}
        if (node.content?.content) out.push(...literals(node.content.content))
        break
    }
    for (const c of node.children ?? []) visit(c)
  }
  if (descriptor.template?.ast) visit(descriptor.template.ast)
  return out
}

describe('сторож i18n', () => {
  it('нашёл исходники фронтенда', () => {
    expect(files.length).toBeGreaterThan(15)
  })

  it('в коде и шаблонах нет захардкоженного русского текста', () => {
    const found: Found[] = []
    for (const f of files) {
      for (const text of texts(f)) if (hasCyrillic(text)) found.push({ file: relative(SRC, f), text: text.trim().slice(0, 70) })
    }
    expect(found, 'вынесите тексты в src/i18n/ru.ts и it.ts').toEqual([])
  })

  it('каждый ключ каталога где-то используется (нет мёртвых ключей)', () => {
    const used = new Set(files.flatMap(texts))
    const keys: string[] = []
    const collect = (o: unknown, prefix = '') => {
      for (const [k, v] of Object.entries(o as Record<string, unknown>)) {
        const path = prefix ? `${prefix}.${k}` : k
        if (typeof v === 'string') keys.push(path)
        else collect(v, path)
      }
    }
    collect(ru)
    const dead = keys.filter((k) => !used.has(k))
    expect(dead, 'ключи есть в каталоге, но в коде на них нет ссылок').toEqual([])
    // Итальянский каталог не может отставать: типы это уже гарантируют, здесь — на случай обхода типов.
    expect(Object.keys(itCatalog)).toEqual(Object.keys(ru))
  })

  it('сам сторож видит нарушения (проверка на заведомо плохом коде)', () => {
    expect(literals("const a = 'привет'; // комментарий").filter(hasCyrillic)).toEqual(['привет'])
    expect(literals('const a = `привет ${x} мир`').filter(hasCyrillic)).toEqual(['привет ', ' мир'])
    expect(literals('// только комментарий\nconst a = 1').filter(hasCyrillic)).toEqual([])

    const { descriptor } = parse('<template><p title="подсказка">Текст</p><!-- комментарий --></template>')
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const ast = descriptor.template?.ast as any
    const p = ast.children[0]
    expect(p.props[0].value.content).toBe('подсказка')
    expect(p.children[0].content).toBe('Текст')
  })
})

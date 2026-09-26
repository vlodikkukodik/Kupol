// @vitest-environment node
// Контраст пар «текст на фоне» из дизайн-токенов: WCAG 2.2 AA (4.5:1 для текста, 3:1 для границ и кольца фокуса).
// Значения читаются из самого tokens.css, так что правка палитры, ломающая читаемость, роняет тест.
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const css = readFileSync(resolve(import.meta.dirname, '../src/styles/tokens.css'), 'utf8')

const raw = new Map<string, string>()
for (const m of css.matchAll(/^\s*(--[a-z0-9-]+)\s*:\s*([^;]+);/gm)) {
  const [, name, value] = m
  if (name && value) raw.set(name, value.trim())
}

/** Значение токена с раскрытием var(--…) до цвета #rrggbb. */
function color(name: string): string {
  let value = raw.get(name)
  for (let i = 0; value !== undefined && i < 8; i++) {
    const ref = /^var\((--[a-z0-9-]+)\)$/.exec(value)
    if (!ref?.[1]) break
    value = raw.get(ref[1])
  }
  if (!value || !/^#[0-9a-f]{6}$/i.test(value)) throw new Error(`токен ${name} не сводится к цвету: ${value}`)
  return value
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = Number.parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  }) as [number, number, number]
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number]
  return (hi + 0.05) / (lo + 0.05)
}

const token = (n: string) => (n.startsWith('#') ? n : color(n))

describe('токены: файл разобран', () => {
  it('находит палитру и смысловые токены', () => {
    expect(raw.size).toBeGreaterThan(80)
    expect(color('--text')).toMatch(/^#/)
    expect(color('--surface')).toMatch(/^#/)
  })
})

// [текст, фон, минимум]
const TEXT_PAIRS: [string, string, number][] = [
  // бумага
  ['--text', '--surface', 4.5],
  ['--text', '--surface-raised', 4.5],
  ['--text', '--surface-sunken', 4.5],
  ['--text', '--surface-strong', 4.5],
  ['--text-muted', '--surface', 4.5],
  ['--text-muted', '--surface-sunken', 4.5],
  ['--text-muted', '--surface-strong', 4.5],
  ['--link', '--surface', 4.5],
  ['--link', '--surface-sunken', 4.5],
  ['--link-hover', '--surface', 4.5],
  ['--danger', '--surface', 4.5],
  ['--danger', '--surface-sunken', 4.5],
  ['--success', '--surface', 4.5],
  ['--warning', '--surface', 4.5],
  ['--accent-text', '--surface', 4.5],
  // стол
  ['--on-bg', '--bg', 4.5],
  ['--on-bg', '--bg-raised', 4.5],
  ['--on-bg-muted', '--bg', 4.5],
  ['--on-bg-muted', '--bg-raised', 4.5],
  // кнопки и плашки: светлый текст на чернилах, тёмный на бумаге
  ['--text-inverse', '--ink-900', 4.5],
  ['--text-inverse', '--red-700', 4.5],
  ['--paper-100', '--redaction', 4.5],
  ['--ink-900', '--paper-100', 4.5],
  ['--ink-900', '--amber-300', 4.5],
  // штампы и метки статусов на бумаге
  ['--red-700', '--surface', 4.5],
  ['--red-700', '--surface-sunken', 4.5],
  ['--green-700', '--surface', 4.5],
  ['--green-700', '--surface-sunken', 4.5],
  ['--amber-700', '--surface', 4.5],
  ['--amber-700', '--surface-sunken', 4.5],
  ['--ink-700', '--surface', 4.5],
  ['--ink-600', '--surface-sunken', 4.5],
  // папки
  ['--ink-900', '--kraft-200', 4.5],
  ['--ink-900', '--kraft-300', 4.5],
  // цветные оповещения (фоны заданы в UiAlert и DeleteAccountForm)
  ['--red-800', '#f8e6e1', 4.5],
  ['#1c4529', '#e6efe2', 4.5],
  ['#5c3806', '#f6ecd0', 4.5],
  ['--red-800', '#f8ece8', 4.5],
]

// Не текст: границы полей ввода и кольцо фокуса — 3:1 (WCAG 1.4.11)
const UI_PAIRS: [string, string, number][] = [
  ['--border-strong', '--surface', 3],
  ['--border-strong', '--surface-raised', 3],
  ['--ink-900', '--surface', 3],
  ['--on-bg', '--bg', 3],
  // двойное кольцо: тёмная внутренняя линия видна на бумаге, жёлтая внешняя — на столе и рядом с тёмной
  ['--focus-inner', '--surface', 3],
  ['--focus', '--focus-inner', 3],
  ['--focus', '--bg', 3],
]

describe('контраст текста (≥ 4.5:1)', () => {
  it.each(TEXT_PAIRS)('%s на %s', (fg, bg, min) => {
    const ratio = contrast(token(fg), token(bg))
    expect(ratio, `${fg} на ${bg}: ${ratio.toFixed(2)}`).toBeGreaterThanOrEqual(min)
  })
})

describe('контраст элементов интерфейса (≥ 3:1)', () => {
  it.each(UI_PAIRS)('%s на %s', (fg, bg, min) => {
    const ratio = contrast(token(fg), token(bg))
    expect(ratio, `${fg} на ${bg}: ${ratio.toFixed(2)}`).toBeGreaterThanOrEqual(min)
  })
})

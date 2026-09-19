// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { validateLogin, validatePassword } from '@/lib/rules'

interface Vector {
  input?: string
  codepoints?: number[]
  ok: boolean
  note?: string
}

// Общий с сервером файл векторов: тот же читает Go-тест accounts.TestLoginRulesMatchSharedVectors.
const vectorsPath = resolve(import.meta.dirname, '../../backend/internal/accounts/testdata/login_rules.json')
const { vectors } = JSON.parse(readFileSync(vectorsPath, 'utf8')) as { vectors: Vector[] }
const valueOf = (v: Vector): string => (v.input !== undefined ? v.input : String.fromCodePoint(...(v.codepoints ?? [])))
const ch = (code: number) => String.fromCharCode(code)

describe('validateLogin — общие векторы с сервером', () => {
  it('файл векторов прочитан целиком', () => {
    expect(vectors.length).toBeGreaterThanOrEqual(30)
  })

  it.each(vectors.map((v) => [JSON.stringify(valueOf(v)), v] as const))('%s', (_name, v) => {
    // клиент не знает зарезервированных слов: для него важен только формат (v.ok)
    const err = validateLogin(valueOf(v))
    expect(err === '', `${v.note ?? ''} → «${err}»`).toBe(v.ok)
  })
})

describe('validateLogin — тексты ошибок совпадают с серверными', () => {
  it.each([
    ['ab', 'Логин: от 3 до 24 символов'],
    ['_abc', 'Логин должен начинаться с буквы или цифры'],
    ['ab c', 'Логин может содержать буквы (латиница или кириллица), цифры, «_» и «-»'],
    ['Kуратор7', 'Логин: буквы только латиницей или только кириллицей, не вперемешку'],
  ])('%s', (input, message) => {
    expect(validateLogin(input)).toBe(message)
  })
})

describe('validatePassword', () => {
  it('допускает 8–128 символов, считая символы, а не байты', () => {
    expect(validatePassword('12345678')).toBe('')
    expect(validatePassword('абвгдежз')).toBe('') // 8 символов, 16 байт
    expect(validatePassword('x'.repeat(128))).toBe('')
    expect(validatePassword('абвгдеж')).toBe('Пароль: от 8 до 128 символов') // 7 символов, 14 байт
    expect(validatePassword('1234567')).toBe('Пароль: от 8 до 128 символов')
    expect(validatePassword('x'.repeat(129))).toBe('Пароль: от 8 до 128 символов')
    expect(validatePassword('')).toBe('Пароль: от 8 до 128 символов')
  })

  it('считает символ вне BMP одним символом', () => {
    const smile = String.fromCodePoint(0x1f600)
    // 4 «эмодзи» по 2 UTF-16 единицы: 8 единиц, но 4 символа — слишком коротко
    expect(validatePassword(smile.repeat(4))).toBe('Пароль: от 8 до 128 символов')
    expect(validatePassword(smile.repeat(8))).toBe('')
  })

  it('отвергает управляющие символы', () => {
    for (const bad of [`passwo${ch(0)}rd`, `pass${ch(10)}word1`, `pass${ch(9)}word1`]) {
      expect(validatePassword(bad)).toBe('Пароль не должен содержать управляющих символов')
    }
  })

  it('отвергает пароль, совпадающий с логином (без регистра, ё = е)', () => {
    expect(validatePassword('VladVlad', 'vladvlad')).toBe('Пароль не должен совпадать с логином')
    expect(validatePassword('Стажёр77', 'стажер77')).toBe('Пароль не должен совпадать с логином')
    expect(validatePassword('VladVlad', 'other')).toBe('')
  })
})

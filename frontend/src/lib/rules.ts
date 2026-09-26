// Правила логина и пароля — зеркало серверных (backend/internal/accounts/rules.go).
// Нужны для мгновенной подсказки в форме; окончательное решение всегда за сервером.
// Общие тестовые векторы: backend/internal/accounts/testdata/login_rules.json.
import { t } from '@/i18n'

export const LOGIN_MIN = 3
export const LOGIN_MAX = 24
export const PASSWORD_MIN = 8
export const PASSWORD_MAX = 128

const isDigit = (ch: string) => ch >= '0' && ch <= '9'
const isLatin = (ch: string) => (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
// А–я, Ё, ё — кодами, чтобы в исходниках не было кириллицы вне каталогов языков (её ищет tests/i18n.test.ts)
// А–я, Ё, ё — кодами, чтобы в исходниках не было кириллицы вне каталогов языков (её ищет tests/i18n.test.ts)
const cp = (code: number) => String.fromCodePoint(code)
const isCyrillic = (ch: string) => (ch >= cp(0x410) && ch <= cp(0x44f)) || ch === cp(0x401) || ch === cp(0x451)

/** Длина в символах (кодовых точках), как считает сервер. */
const length = (s: string) => Array.from(s).length

/**
 * Проверка логина. Возвращает текст ошибки или '' (логин допустим по формату).
 * Список зарезервированных слов знает только сервер.
 */
export function validateLogin(input: string): string {
  const chars = Array.from(input.normalize('NFC'))

  if (chars.length < LOGIN_MIN || chars.length > LOGIN_MAX) {
    return t('rules.loginLength')
  }

  let seen: 'latin' | 'cyrillic' | null = null
  for (const [i, ch] of chars.entries()) {
    let script: 'latin' | 'cyrillic' | null = null
    if (isDigit(ch)) {
      // цифры допустимы в любой позиции
    } else if (ch === '_' || ch === '-') {
      if (i === 0) return t('rules.loginStart')
    } else if (isLatin(ch)) {
      script = 'latin'
    } else if (isCyrillic(ch)) {
      script = 'cyrillic'
    } else {
      return t('rules.loginChars')
    }
    if (script) {
      if (seen && seen !== script) return t('rules.loginMixed')
      seen = script
    }
  }
  return ''
}

const fold = (s: string) => s.normalize('NFC').toLowerCase().replaceAll(cp(0x451), cp(0x435))

/** Проверка нового пароля. Возвращает текст ошибки или ''. */
export function validatePassword(password: string, login = ''): string {
  const n = length(password)
  if (n < PASSWORD_MIN || n > PASSWORD_MAX) return t('rules.passwordLength')
  if (/\p{Cc}/u.test(password)) return t('rules.passwordControl')
  if (login && fold(password) === fold(login)) return t('rules.passwordLikeLogin')
  return ''
}

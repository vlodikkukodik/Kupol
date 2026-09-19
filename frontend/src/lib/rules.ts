// Правила логина и пароля — зеркало серверных (backend/internal/accounts/rules.go).
// Нужны для мгновенной подсказки в форме; окончательное решение всегда за сервером.
// Общие тестовые векторы: backend/internal/accounts/testdata/login_rules.json.

export const LOGIN_MIN = 3
export const LOGIN_MAX = 24
export const PASSWORD_MIN = 8
export const PASSWORD_MAX = 128

const isDigit = (ch: string) => ch >= '0' && ch <= '9'
const isLatin = (ch: string) => (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')
const isCyrillic = (ch: string) => (ch >= 'А' && ch <= 'я') || ch === 'Ё' || ch === 'ё'

/** Длина в символах (кодовых точках), как считает сервер. */
const length = (s: string) => Array.from(s).length

/**
 * Проверка логина. Возвращает текст ошибки или '' (логин допустим по формату).
 * Список зарезервированных слов знает только сервер.
 */
export function validateLogin(input: string): string {
  const chars = Array.from(input.normalize('NFC'))

  if (chars.length < LOGIN_MIN || chars.length > LOGIN_MAX) {
    return 'Логин: от 3 до 24 символов'
  }

  let seen: 'latin' | 'cyrillic' | null = null
  for (const [i, ch] of chars.entries()) {
    let script: 'latin' | 'cyrillic' | null = null
    if (isDigit(ch)) {
      // цифры допустимы в любой позиции
    } else if (ch === '_' || ch === '-') {
      if (i === 0) return 'Логин должен начинаться с буквы или цифры'
    } else if (isLatin(ch)) {
      script = 'latin'
    } else if (isCyrillic(ch)) {
      script = 'cyrillic'
    } else {
      return 'Логин может содержать буквы (латиница или кириллица), цифры, «_» и «-»'
    }
    if (script) {
      if (seen && seen !== script) return 'Логин: буквы только латиницей или только кириллицей, не вперемешку'
      seen = script
    }
  }
  return ''
}

const fold = (s: string) => s.normalize('NFC').toLowerCase().replaceAll('ё', 'е')

/** Проверка нового пароля. Возвращает текст ошибки или ''. */
export function validatePassword(password: string, login = ''): string {
  const n = length(password)
  if (n < PASSWORD_MIN || n > PASSWORD_MAX) return 'Пароль: от 8 до 128 символов'
  if (/\p{Cc}/u.test(password)) return 'Пароль не должен содержать управляющих символов'
  if (login && fold(password) === fold(login)) return 'Пароль не должен совпадать с логином'
  return ''
}

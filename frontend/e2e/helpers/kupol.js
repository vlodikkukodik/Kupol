import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'

// Сквозные тесты документов наполняют настоящую БД тем же путём, что автор: командой `kupol doc import`.
//  Ни моков, ни прямых INSERT: фикстуры проходят проверку формата и правила видимости боевого кода.

const ROOT = resolve(import.meta.dirname, '..', '..', '..')
export const BACKEND = resolve(ROOT, 'backend')
export const CLI = resolve(BACKEND, 'bin', 'kupol')
export const PACK = resolve(import.meta.dirname, '..', 'fixtures', 'pack.json')

/** Пишут ли тесты в БД: против внешнего адреса — только если это явно разрешено (своя временная БД). */
export const canWrite = !process.env.KUPOL_E2E_BASE_URL || process.env.KUPOL_E2E_ALLOW_WRITES === '1'

/** Запуск `kupol <args>` с настройками из окружения (KUPOL_DATABASE_URL — та БД, с которой работает сайт). */
export function cli(...args) {
  try {
    return execFileSync(CLI, args, {
      env: { ...process.env, KUPOL_LOG_LEVEL: 'error' },
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    })
  } catch (err) {
    throw new Error(`kupol ${args.join(' ')} завершился с ошибкой:\n${err.stderr || err.message}`, { cause: err })
  }
}

// 30 меморандумов 1902 года — на 2 страницы каталога (25 на странице).
export const BULK_COUNT = 30
const BULK_FILE = resolve(tmpdir(), 'kupol-e2e-bulk.json')
const bulkCodes = () => Array.from({ length: BULK_COUNT }, (_, i) => `МЕМО-${9001 + i}`)

function writeBulk() {
  const documents = bulkCodes().map((code, i) => ({
    code,
    type: 'memo',
    title: `[e2e] Массовый меморандум ${String(i + 1).padStart(2, '0')}`,
    status: 'published',
    level: 0,
    composed: { year: 1902, month: 1, day: i + 1 },
    blocks: [{ id: 'body', type: 'paragraph', data: { text: 'Проверка постраничной навигации.' } }],
  }))
  writeFileSync(BULK_FILE, JSON.stringify({ documents }))
}

/** Шифры всех документов фикстур. */
export function packCodes() {
  const { documents } = JSON.parse(readFileSync(PACK, 'utf8'))
  return [...documents.map((d) => d.code), ...bulkCodes()]
}

export function seedDocuments() {
  writeBulk()
  cli('doc', 'import', PACK)
  return cli('doc', 'import', BULK_FILE)
}

export function removeDocuments() {
  for (const code of packCodes()) {
    try {
      cli('doc', 'delete', code, '--yes')
    } catch {
      // документа уже нет — так и должно быть
    }
  }
}

// Ответы на вопросы анкеты — как их прочитал бы человек на главной странице.
const CAPTCHA_ANSWERS = [
  [/В каком году основан/, '1974'],
  [/Какой гриф/, 'Форма КУПОЛ-1'],
  [/Как сокращённо/, 'ЦАК'],
  [/пропущенное слово/, 'объектами'],
  [/С какого слова начинается/, 'Комитет'],
  [/Сколько букв/, '5'],
]

export const PASSWORD = 'секретный пароль 1'
export const uniqueLogin = () => `e2e${Math.random().toString(36).slice(2, 10)}`

/**
 * Регистрирует пользователя настоящим запросом к API и выдаёт ему уровень (и Директорат) командой сервера.
 * Сессия попадает в куки переданного контекста браузера: страницы этого контекста открываются уже «вошедшими».
 */
export async function signUp(context, { level = 1, directorate = false, roles = [] } = {}) {
  const login = uniqueLogin()
  const cap = await (await context.request.get('/api/auth/captcha')).json()
  const hit = CAPTCHA_ANSWERS.find(([re]) => re.test(cap.question))
  if (!hit) throw new Error(`неизвестный вопрос анкеты: ${cap.question}`)
  const res = await context.request.post('/api/auth/register', {
    data: { login, password: PASSWORD, captcha_id: cap.id, captcha_answer: hit[1] },
  })
  if (!res.ok()) throw new Error(`регистрация не удалась: ${res.status()} ${await res.text()}`)
  if (level > 1) cli('user', 'set-level', login, String(level))
  if (directorate) cli('user', 'set-directorate', login, 'on')
  for (const role of roles) cli('user', 'set-role', login, role, 'on')
  return login
}

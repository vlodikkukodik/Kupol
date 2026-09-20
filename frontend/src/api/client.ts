// Клиент API. Все обычные запросы идут на тот же домен (/api/...), где их принимает PHP-прокси и передаёт Go API.
// Ошибки приводятся к единому виду ApiError; о каждом исходе можно узнать через subscribe (сторы связи и сессии).
import type { ErrorBody } from './generated/httpapi'
import type { LintIssue, LintReport, Problem } from './generated/documents'

export interface LockNotice {
  holder: string
  expiresAt: string
}

export interface ApiErrorInit {
  /** HTTP-код; 0 — до сервера не достучались */
  status: number
  /** Машинный код (not_found, maintenance, network, …) */
  code: string
  message: string
  requestId?: string
  /** Ошибки по полям формы: имя поля в JSON запроса → текст */
  fields?: Record<string, string>
  /** Через сколько секунд повторить (429) */
  retryAfter?: number
  requiredLevel?: number
  requiredLevelName?: string
  /** Замечания к содержимому документа (422 validation) */
  problems?: Problem[]
  /** Кто правит документ (409 locked) */
  lock?: LockNotice | null
  /** Актуальная редакция (409 conflict) */
  currentRevision?: number
  /** Отчёт линтера канона (422 lint_failed) */
  lint?: LintReport | null
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId: string
  readonly fields: Record<string, string>
  readonly retryAfter: number
  readonly requiredLevel: number
  readonly requiredLevelName: string
  readonly problems: Problem[]
  readonly lock: LockNotice | null
  readonly currentRevision: number
  readonly lint: LintReport | null

  constructor(init: ApiErrorInit) {
    super(init.message)
    this.name = 'ApiError'
    this.status = init.status
    this.code = init.code
    this.requestId = init.requestId ?? ''
    this.fields = init.fields ?? {}
    this.retryAfter = init.retryAfter ?? 0
    this.requiredLevel = init.requiredLevel ?? 0
    this.requiredLevelName = init.requiredLevelName ?? ''
    this.problems = init.problems ?? []
    this.lock = init.lock ?? null
    this.currentRevision = init.currentRevision ?? 0
    this.lint = init.lint ?? null
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

export type ApiEvent = { ok: true } | { ok: false; error: ApiError }

export interface RequestOptions {
  method?: string
  body?: unknown
  signal?: AbortSignal
  headers?: Record<string, string>
}

/** Файл, который отдал сервер (экспорт документа): содержимое и имя из Content-Disposition */
export interface Download {
  blob: Blob
  filename: string
}

export interface Client {
  request<T = unknown>(path: string, opts?: RequestOptions): Promise<T>
  /** GET файла: тело — не JSON, ошибки — как у обычных запросов */
  download(path: string, opts?: RequestOptions): Promise<Download>
  get<T = unknown>(path: string, opts?: RequestOptions): Promise<T>
  post<T = unknown>(path: string, body?: unknown, opts?: RequestOptions): Promise<T>
  put<T = unknown>(path: string, body?: unknown, opts?: RequestOptions): Promise<T>
  patch<T = unknown>(path: string, body?: unknown, opts?: RequestOptions): Promise<T>
  delete<T = unknown>(path: string, body?: unknown, opts?: RequestOptions): Promise<T>
  /** Подписка на исходы всех запросов. Возвращает функцию отписки. */
  subscribe(fn: (event: ApiEvent) => void): () => void
}

const DEFAULT_TIMEOUT_MS = 30_000

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v)

/** Замечания: только записи вида {path: строка, message: строка}. */
function cleanProblems(raw: unknown): Problem[] {
  if (!Array.isArray(raw)) return []
  const out: Problem[] = []
  for (const p of raw) {
    if (isRecord(p) && typeof p.path === 'string' && typeof p.message === 'string') out.push({ path: p.path, message: p.message })
  }
  return out
}

function cleanLock(raw: unknown): LockNotice | null {
  if (!isRecord(raw) || typeof raw.holder !== 'string' || typeof raw.expires_at !== 'string') return null
  return { holder: raw.holder, expiresAt: raw.expires_at }
}

/** Только строковые значения: сервер шлёт fields как объект {поле: текст}. */
function cleanFields(raw: unknown): Record<string, string> {
  if (!isRecord(raw)) return {}
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(raw)) if (typeof v === 'string') out[k] = v
  return out
}

/** Отчёт линтера из ответа сервера; неверная форма — как будто отчёта нет. */
function cleanLint(raw: unknown): LintReport | null {
  if (!isRecord(raw) || !Array.isArray(raw.issues)) return null
  const issues: LintIssue[] = []
  for (const i of raw.issues) {
    if (isRecord(i) && (i.severity === 'error' || i.severity === 'warning') && typeof i.code === 'string' && typeof i.message === 'string') {
      issues.push({ severity: i.severity, code: i.code, message: i.message, ...(typeof i.block_id === 'string' && i.block_id ? { block_id: i.block_id } : {}) })
    }
  }
  return { issues, errors: issues.filter((i) => i.severity === 'error').length, warnings: issues.filter((i) => i.severity === 'warning').length }
}

const int = (v: unknown): number => (typeof v === 'number' && Number.isInteger(v) ? v : 0)

/** Ошибка Go API / прокси: { error: { code, message, request_id } }. */
async function toApiError(res: Response): Promise<ApiError> {
  let payload: unknown = null
  try {
    payload = await res.json()
  } catch {
    // ответ не JSON — например, страница ошибки самого хостинга
  }
  const e = isRecord(payload) && isRecord(payload.error) ? (payload.error as Partial<ErrorBody['error']> & Record<string, unknown>) : null
  if (e && typeof e.code === 'string') {
    return new ApiError({
      status: res.status,
      code: e.code,
      message: typeof e.message === 'string' ? e.message : res.statusText,
      requestId: typeof e.request_id === 'string' ? e.request_id : (res.headers.get('x-request-id') ?? ''),
      fields: cleanFields(e.fields),
      retryAfter: int(e.retry_after) || Number(res.headers.get('retry-after')) || 0,
      requiredLevel: int(e.required_level),
      requiredLevelName: typeof e.required_level_name === 'string' ? e.required_level_name : '',
      problems: cleanProblems(e.problems),
      lock: cleanLock(e.lock),
      currentRevision: int(e.current_revision),
      lint: cleanLint(e.lint),
    })
  }
  return new ApiError({
    status: res.status,
    code: 'bad_response',
    message: res.statusText || 'Неожиданный ответ сервера',
    requestId: res.headers.get('x-request-id') ?? '',
    retryAfter: Number(res.headers.get('retry-after')) || 0,
  })
}

export interface ClientOptions {
  /** Префикс путей; по умолчанию /api */
  base?: string
  timeoutMs?: number
}

export function createClient({ base = '/api', timeoutMs = DEFAULT_TIMEOUT_MS }: ClientOptions = {}): Client {
  const listeners = new Set<(event: ApiEvent) => void>()
  const notify = (event: ApiEvent) => listeners.forEach((fn) => fn(event))

  /** Отправляет запрос и возвращает успешный ответ; сбой связи и коды не 2xx — ApiError (и событие для сторов связи). */
  async function send({ path, method = 'GET', body, signal, headers = {} }: RequestOptions & { path: string }): Promise<Response> {
    if (!path.startsWith('/')) throw new TypeError(`путь API должен начинаться с "/": ${path}`)

    const init: RequestInit = { method, headers: { Accept: 'application/json', ...headers }, credentials: 'same-origin' }
    if (body !== undefined) {
      ;(init.headers as Record<string, string>)['Content-Type'] = 'application/json'
      init.body = JSON.stringify(body)
    }
    const timeout = AbortSignal.timeout(timeoutMs)
    init.signal = signal ? AbortSignal.any([signal, timeout]) : timeout

    let res: Response
    try {
      res = await fetch(base + path, init)
    } catch (cause) {
      // Отмену вызывающим отдаём как есть — это не сбой сети.
      if (signal?.aborted) throw cause
      const timedOut = timeout.aborted
      const err = new ApiError({
        status: 0,
        code: timedOut ? 'timeout' : 'network',
        message: timedOut ? 'Архив не ответил вовремя' : 'Нет связи с архивом',
      })
      notify({ ok: false, error: err })
      throw err
    }

    if (!res.ok) {
      const err = await toApiError(res)
      notify({ ok: false, error: err })
      throw err
    }
    return res
  }

  async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
    const res = await send({ path, ...opts })
    notify({ ok: true })
    if (res.status === 204) return null as T
    const text = await res.text()
    if (text === '') return null as T
    try {
      return JSON.parse(text) as T
    } catch {
      const err = new ApiError({ status: res.status, code: 'bad_response', message: 'Неожиданный ответ сервера' })
      notify({ ok: false, error: err })
      throw err
    }
  }

  async function download(path: string, opts: RequestOptions = {}): Promise<Download> {
    const res = await send({ path, ...opts, method: 'GET', headers: { Accept: '*/*', ...opts.headers } })
    const blob = await res.blob()
    notify({ ok: true })
    const named = /filename="([^"]+)"/.exec(res.headers.get('content-disposition') ?? '')
    return { blob, filename: named?.[1] ?? 'file' }
  }

  return {
    request,
    download,
    get: (path, opts) => request(path, { ...opts, method: 'GET' }),
    post: (path, body, opts) => request(path, { ...opts, method: 'POST', body }),
    put: (path, body, opts) => request(path, { ...opts, method: 'PUT', body }),
    patch: (path, body, opts) => request(path, { ...opts, method: 'PATCH', body }),
    delete: (path, body, opts) => request(path, { ...opts, method: 'DELETE', body }),
    subscribe(fn) {
      listeners.add(fn)
      return () => {
        listeners.delete(fn)
      }
    },
  }
}

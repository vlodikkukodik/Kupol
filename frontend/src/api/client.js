// Клиент API. Все обычные запросы идут на тот же домен (/api/...), где их принимает
// PHP-прокси и передаёт Go API. Ошибки приводятся к единому виду ApiError.

export class ApiError extends Error {
  /**
   * @param {object} p
   * @param {number} p.status  HTTP-код; 0 — до сервера не достучались
   * @param {string} p.code    машинный код ошибки (not_found, maintenance, network, ...)
   * @param {string} p.message человекочитаемый текст
   * @param {string} [p.requestId]
   * @param {Record<string,string>} [p.fields] ошибки по полям формы (имя поля в JSON запроса -> текст)
   * @param {number} [p.retryAfter] через сколько секунд повторить (для 429)
   */
  constructor({
    status,
    code,
    message,
    requestId = '',
    fields = {},
    retryAfter = 0,
    requiredLevel = 0,
    requiredLevelName = '',
    problems = [],
    lock = null,
    currentRevision = 0,
  }) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
    this.fields = fields
    this.retryAfter = retryAfter
    this.requiredLevel = requiredLevel
    this.requiredLevelName = requiredLevelName
    /** Замечания к содержимому документа: [{path: 'blocks[2].data.text', message}] (422 validation). */
    this.problems = problems
    /** Кто правит документ: {holder, expiresAt} (409 locked). */
    this.lock = lock
    /** Актуальная редакция документа (409 conflict). */
    this.currentRevision = currentRevision
  }
}

/** Замечания: только записи вида {path: строка, message: строка}. */
function cleanProblems(raw) {
  if (!Array.isArray(raw)) return []
  return raw
    .filter((p) => p && typeof p.path === 'string' && typeof p.message === 'string')
    .map((p) => ({ path: p.path, message: p.message }))
}

function cleanLock(raw) {
  if (!raw || typeof raw !== 'object' || typeof raw.holder !== 'string' || typeof raw.expires_at !== 'string') return null
  return { holder: raw.holder, expiresAt: raw.expires_at }
}

/** Только строковые значения: сервер шлёт fields как объект {поле: текст}. */
function cleanFields(raw) {
  if (!raw || typeof raw !== 'object') return {}
  return Object.fromEntries(Object.entries(raw).filter(([, v]) => typeof v === 'string'))
}

const DEFAULT_TIMEOUT_MS = 30_000

/** Ошибка Go API / прокси: { error: { code, message, request_id } }. */
async function toApiError(res) {
  let payload = null
  try {
    payload = await res.json()
  } catch {
    // ответ не JSON — например, страница ошибки самого хостинга
  }
  const e = payload && typeof payload === 'object' ? payload.error : null
  if (e && typeof e.code === 'string') {
    return new ApiError({
      status: res.status,
      code: e.code,
      message: typeof e.message === 'string' ? e.message : res.statusText,
      requestId: typeof e.request_id === 'string' ? e.request_id : res.headers.get('x-request-id') || '',
      fields: cleanFields(e.fields),
      retryAfter: Number.isInteger(e.retry_after) ? e.retry_after : Number(res.headers.get('retry-after')) || 0,
      requiredLevel: Number.isInteger(e.required_level) ? e.required_level : 0,
      requiredLevelName: typeof e.required_level_name === 'string' ? e.required_level_name : '',
      problems: cleanProblems(e.problems),
      lock: cleanLock(e.lock),
      currentRevision: Number.isInteger(e.current_revision) ? e.current_revision : 0,
    })
  }
  return new ApiError({
    status: res.status,
    code: 'bad_response',
    message: res.statusText || 'Неожиданный ответ сервера',
    requestId: res.headers.get('x-request-id') || '',
    retryAfter: Number(res.headers.get('retry-after')) || 0,
  })
}

/**
 * @param {object} [opts]
 * @param {string} [opts.base]       префикс путей, по умолчанию /api
 * @param {number} [opts.timeoutMs]
 */
export function createClient({ base = '/api', timeoutMs = DEFAULT_TIMEOUT_MS } = {}) {
  const listeners = new Set()
  const notify = (event) => listeners.forEach((fn) => fn(event))

  /**
   * @param {string} path  например '/health'
   * @param {{method?:string, body?:unknown, signal?:AbortSignal, headers?:Record<string,string>}} [opts]
   */
  async function request(path, { method = 'GET', body, signal, headers = {} } = {}) {
    if (!path.startsWith('/')) throw new TypeError(`путь API должен начинаться с "/": ${path}`)

    const init = { method, headers: { Accept: 'application/json', ...headers }, credentials: 'same-origin' }
    if (body !== undefined) {
      init.headers['Content-Type'] = 'application/json'
      init.body = JSON.stringify(body)
    }
    const timeout = AbortSignal.timeout(timeoutMs)
    init.signal = signal ? AbortSignal.any([signal, timeout]) : timeout

    let res
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

    notify({ ok: true })
    if (res.status === 204) return null
    const text = await res.text()
    if (text === '') return null
    try {
      return JSON.parse(text)
    } catch {
      const err = new ApiError({ status: res.status, code: 'bad_response', message: 'Неожиданный ответ сервера' })
      notify({ ok: false, error: err })
      throw err
    }
  }

  return {
    request,
    get: (path, opts) => request(path, { ...opts, method: 'GET' }),
    post: (path, body, opts) => request(path, { ...opts, method: 'POST', body }),
    put: (path, body, opts) => request(path, { ...opts, method: 'PUT', body }),
    patch: (path, body, opts) => request(path, { ...opts, method: 'PATCH', body }),
    delete: (path, body, opts) => request(path, { ...opts, method: 'DELETE', body }),
    /** Подписка на исходы всех запросов: {ok:true} | {ok:false,error}. Возвращает функцию отписки. */
    subscribe(fn) {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
  }
}

import type { z } from 'zod'
import { getLocale, t } from '@/i18n'
import { apiErrorSchema, sessionSchema } from './schemas'

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public field?: string,
  ) {
    super(message)
  }
}

// Access-токен живёт только в памяти: XSS не украдёт его из localStorage.
// Восстановление сессии после перезагрузки идёт через refresh-cookie (httpOnly).
let accessToken: string | null = null
let onSessionLost: () => void = () => {}
let refreshing: Promise<boolean> | null = null

export function setAccessToken(token: string | null) {
  accessToken = token
}

export function onUnauthorized(handler: () => void) {
  onSessionLost = handler
}

async function send(path: string, method: string, body: unknown, token: string | null): Promise<Response> {
  // Язык выбран пользователем в шапке: сервер отвечает на нём (тексты ошибок).
  const headers: Record<string, string> = { 'Accept-Language': getLocale() }
  const isForm = body instanceof FormData
  // Для multipart Content-Type с границей выставляет сам браузер.
  if (body !== undefined && !isForm) headers['Content-Type'] = 'application/json'
  if (token) headers.Authorization = `Bearer ${token}`
  return fetch(path, {
    method,
    headers,
    body: body === undefined ? undefined : isForm ? body : JSON.stringify(body),
    credentials: 'same-origin',
  })
}

async function toError(res: Response): Promise<ApiError> {
  const parsed = apiErrorSchema.safeParse(await res.json().catch(() => null))
  if (parsed.success) {
    const e = parsed.data.error
    return new ApiError(res.status, e.code, e.message, e.field)
  }
  return new ApiError(res.status, 'unknown', t('errors.server', { status: res.status }))
}

/** Обновляет access-токен по refresh-cookie. Параллельные вызовы делят один запрос. */
export function refreshSession(): Promise<boolean> {
  refreshing ??= (async () => {
    try {
      const res = await send('/api/auth/refresh', 'POST', undefined, null)
      if (!res.ok) return false
      const parsed = sessionSchema.safeParse(await res.json())
      if (!parsed.success) return false
      accessToken = parsed.data.access_token
      return true
    } catch {
      return false
    } finally {
      refreshing = null
    }
  })()
  return refreshing
}

interface Options<S extends z.ZodType> {
  method?: string
  body?: unknown
  schema?: S
  /** false — не подставлять токен и не пытаться обновить сессию (вход, регистрация). */
  auth?: boolean
}

/** Запрос, у которого нет тела ответа (204). */
export async function apiVoid(path: string, opts: Omit<Options<z.ZodType>, 'schema'> = {}): Promise<void> {
  await api(path, opts)
}

/** Отправляет запрос с токеном; при 401 один раз обновляет сессию и повторяет. Ошибочные статусы не разбираются. */
async function call(path: string, method: string, body: unknown, auth: boolean): Promise<Response> {
  let res: Response
  try {
    res = await send(path, method, body, auth ? accessToken : null)
  } catch {
    throw new ApiError(0, 'network', t('errors.network'))
  }
  if (res.status === 401 && auth) {
    if (await refreshSession()) {
      res = await send(path, method, body, accessToken)
    }
    if (res.status === 401) {
      accessToken = null
      onSessionLost()
    }
  }
  return res
}

export async function api<S extends z.ZodType>(path: string, opts: Options<S> = {}): Promise<z.infer<S>> {
  const { method = 'GET', body, schema, auth = true } = opts
  const res = await call(path, method, body, auth)
  if (!res.ok) throw await toError(res)
  if (res.status === 204 || !schema) return undefined as z.infer<S>
  const parsed = schema.safeParse(await res.json())
  if (!parsed.success) throw new ApiError(res.status, 'bad_response', t('errors.badResponse'))
  return parsed.data
}

/** Скачивание файла с авторизацией (обычная ссылка не передаёт токен из памяти). */
export async function apiBlob(path: string): Promise<Blob> {
  const res = await call(path, 'GET', undefined, true)
  if (!res.ok) throw await toError(res)
  return res.blob()
}

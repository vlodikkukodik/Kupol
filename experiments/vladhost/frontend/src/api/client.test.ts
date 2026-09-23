import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { z } from 'zod'
import { api, ApiError, onUnauthorized, setAccessToken } from './client'
import { fieldErrors, registerForm } from './schemas'

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

const session = { access_token: 'NEW', expires_in: 900, user: { id: 1, email: 'a@b.co', username: 'abc', role: 'user', created_at: 'x' } }

describe('api client', () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    setAccessToken('OLD')
    onUnauthorized(() => {})
  })
  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  it('на 401 обновляет токен и повторяет запрос один раз', async () => {
    fetchMock
      .mockResolvedValueOnce(json(401, { error: { code: 'unauthorized', message: 'x' } }))
      .mockResolvedValueOnce(json(200, session))
      .mockResolvedValueOnce(json(200, { ok: 1 }))
    const out = await api('/api/thing', { schema: z.object({ ok: z.number() }) })
    expect(out).toEqual({ ok: 1 })
    const retry = fetchMock.mock.calls[2]?.[1] as RequestInit
    expect((retry.headers as Record<string, string>).Authorization).toBe('Bearer NEW')
  })

  it('если обновить не удалось — сообщает о потере сессии и бросает ApiError', async () => {
    const lost = vi.fn()
    onUnauthorized(lost)
    fetchMock.mockResolvedValue(json(401, { error: { code: 'unauthorized', message: 'сессия недействительна' } }))
    await expect(api('/api/thing')).rejects.toMatchObject({ status: 401, code: 'unauthorized' })
    expect(lost).toHaveBeenCalledOnce()
  })

  it('параллельные 401 делят один запрос refresh', async () => {
    let refreshCalls = 0
    fetchMock.mockImplementation(async (url) => {
      if (String(url).endsWith('/refresh')) {
        refreshCalls++
        return json(200, session)
      }
      const hdr = (fetchMock.mock.lastCall?.[1] as RequestInit).headers as Record<string, string>
      return hdr.Authorization === 'Bearer NEW' ? json(200, { ok: 1 }) : json(401, { error: { code: 'unauthorized', message: 'x' } })
    })
    const schema = z.object({ ok: z.number() })
    await Promise.all([api('/api/a', { schema }), api('/api/b', { schema }), api('/api/c', { schema })])
    expect(refreshCalls).toBe(1)
  })

  it('вход без авторизации не пытается обновлять сессию', async () => {
    fetchMock.mockResolvedValue(json(401, { error: { code: 'invalid_credentials', message: 'неверный логин или пароль' } }))
    await expect(api('/api/auth/login', { method: 'POST', body: {}, auth: false })).rejects.toBeInstanceOf(ApiError)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('отклоняет ответ, не совпавший со схемой', async () => {
    fetchMock.mockResolvedValue(json(200, { ok: 'not-a-number' }))
    await expect(api('/api/thing', { schema: z.object({ ok: z.number() }) })).rejects.toMatchObject({ code: 'bad_response' })
  })

  it('FormData отправляет как есть, без ручного Content-Type (границу ставит браузер)', async () => {
    fetchMock.mockResolvedValue(json(200, { ok: 1 }))
    const body = new FormData()
    body.append('file', new Blob(['zip']), 'a.zip')
    await api('/api/upload', { method: 'POST', body, schema: z.object({ ok: z.number() }) })
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit
    expect(init.body).toBe(body)
    expect((init.headers as Record<string, string>)['Content-Type']).toBeUndefined()
  })

  it('сетевую ошибку превращает в ApiError', async () => {
    fetchMock.mockRejectedValue(new TypeError('fail'))
    await expect(api('/api/thing')).rejects.toMatchObject({ code: 'network', status: 0 })
  })
})

describe('форма регистрации', () => {
  const ok = { invite: 'abc', email: 'a@b.co', username: 'John-1', password: 'password1' }

  it('нормализует имя в нижний регистр', () => {
    const r = registerForm.safeParse(ok)
    expect(r.success && r.data.username).toBe('john-1')
  })

  it.each([
    ['username', { username: 'ab' }],
    ['username', { username: '-abc' }],
    ['username', { username: 'a--b' }],
    ['username', { username: 'a.b' }],
    ['email', { email: 'nope' }],
    ['password', { password: 'short' }],
    ['invite', { invite: '  ' }],
  ])('находит ошибку в поле %s', (field, patch) => {
    const r = registerForm.safeParse({ ...ok, ...patch })
    expect(r.success).toBe(false)
    if (!r.success) expect(Object.keys(fieldErrors(r.error))).toContain(field)
  })
})

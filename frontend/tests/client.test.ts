// @vitest-environment node
// Клиент API проверяется по настоящей сети: реальный HTTP-сервер и настоящий fetch.
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import http from 'node:http'
import type { AddressInfo } from 'node:net'
import { ApiError, createClient, type ApiEvent } from '@/api/client'

let server: http.Server
let base = ''
let closedPortBase = ''

interface Echo {
  method: string
  contentType?: string
  accept?: string
  body: string
}

beforeAll(async () => {
  server = http.createServer(async (req, res) => {
    const url = new URL(req.url ?? '/', 'http://x')
    const chunks: Buffer[] = []
    for await (const c of req) chunks.push(c as Buffer)
    const body = Buffer.concat(chunks).toString('utf8')
    const json = (status: number, obj: unknown, headers: Record<string, string> = {}) => {
      res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', ...headers })
      res.end(JSON.stringify(obj))
    }

    switch (url.pathname) {
      case '/api/ok':
        return json(200, { hello: 'мир' })
      case '/api/empty':
        res.writeHead(204)
        return res.end()
      case '/api/blank200':
        res.writeHead(200)
        return res.end()
      case '/api/echo':
        return json(200, { method: req.method, contentType: req.headers['content-type'], accept: req.headers.accept, body })
      case '/api/missing':
        return json(404, { error: { code: 'not_found', message: 'Дело не найдено', request_id: 'rid-404' } })
      case '/api/html500':
        res.writeHead(500, { 'Content-Type': 'text/html', 'X-Request-Id': 'rid-html' })
        return res.end('<h1>Internal Server Error</h1>')
      case '/api/validation':
        return json(422, {
          error: {
            code: 'validation',
            message: 'Проверьте поля формы',
            request_id: 'rid-422',
            fields: { login: 'Логин: от 3 до 24 символов', password: 'Пароль: от 8 до 128 символов', junk: 42, nested: { a: 1 } },
            problems: [{ path: 'blocks[0].data.text', message: 'слишком длинный' }, { path: 5 }, 'мусор'],
          },
        })
      case '/api/locked':
        return json(409, { error: { code: 'locked', message: 'Документ правит vera', lock: { holder: 'vera', expires_at: '2026-09-19T12:00:00Z' } } })
      case '/api/conflict':
        return json(409, { error: { code: 'conflict', message: 'Документ изменён', current_revision: 7 } })
      case '/api/lint':
        return json(422, {
          error: {
            code: 'lint_failed',
            message: 'Документ не прошёл проверку канона',
            lint: {
              issues: [
                { severity: 'error', code: 'broken_link', message: 'Ссылка на О-9998: такого документа нет', block_id: 'l' },
                { severity: 'warning', code: 'no_dossier_header', message: 'Нет шапки досье' },
                { severity: 'fatal', code: 'x', message: 'неизвестная тяжесть' },
                { severity: 'error', code: 5, message: 'не строка' },
                'мусор',
              ],
              errors: 99,
              warnings: 99,
            },
          },
        })
      case '/api/lint-junk':
        return json(422, { error: { code: 'lint_failed', message: 'x', lint: 'не объект' } })
      case '/api/denied':
        return json(403, {
          error: { code: 'access_denied', message: 'Доступ запрещён', request_id: 'rid-403', required_level: 4, required_level_name: 'Надзиратель' },
        })
      case '/api/denied-junk':
        return json(403, { error: { code: 'access_denied', message: 'Доступ запрещён', required_level: '4', required_level_name: 7 } })
      case '/api/limited':
        return json(429, { error: { code: 'rate_limited', message: 'Слишком много попыток', retry_after: 840 } }, { 'Retry-After': '840' })
      case '/api/limited-header-only':
        res.writeHead(429, { 'Content-Type': 'text/plain', 'Retry-After': '30' })
        return res.end('slow down')
      case '/api/maintenance':
        return json(503, { error: { code: 'maintenance', message: 'Архив закрыт на инвентаризацию' } })
      case '/api/badjson':
        res.writeHead(200, { 'Content-Type': 'application/json' })
        return res.end('{not json')
      case '/api/file':
        res.writeHead(200, { 'Content-Type': 'text/markdown; charset=utf-8', 'Content-Disposition': 'attachment; filename="MEMO-1.md"' })
        return res.end('# Заголовок\n')
      case '/api/file-unnamed':
        res.writeHead(200, { 'Content-Type': 'application/octet-stream' })
        return res.end('x')
      case '/api/slow':
        return void setTimeout(() => json(200, { late: true }), 1500)
    }
    return json(404, { error: { code: 'not_found', message: 'нет такого маршрута' } })
  })
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}/api`

  // Порт, на котором точно никто не слушает.
  const probe = http.createServer()
  await new Promise<void>((r) => probe.listen(0, '127.0.0.1', r))
  closedPortBase = `http://127.0.0.1:${(probe.address() as AddressInfo).port}/api`
  await new Promise((r) => probe.close(r))
})

afterAll(async () => {
  server.closeAllConnections()
  await new Promise((r) => server.close(r))
})

const caught = async (p: Promise<unknown>): Promise<ApiError> => (await p.then(
  () => {
    throw new Error('ожидалась ошибка')
  },
  (e: unknown) => e,
)) as ApiError

describe('createClient', () => {
  it('GET возвращает разобранный JSON (кириллица цела)', async () => {
    const api = createClient({ base })
    expect(await api.get('/ok')).toEqual({ hello: 'мир' })
  })

  it('отправляет Accept: application/json', async () => {
    const api = createClient({ base })
    expect((await api.get<Echo>('/echo')).accept).toBe('application/json')
  })

  it('POST/PUT/PATCH/DELETE: метод, Content-Type и тело в JSON', async () => {
    const api = createClient({ base })
    const payload = { текст: 'Изделие К-19', n: 1 }
    for (const m of ['post', 'put', 'patch'] as const) {
      const r = await api[m]<Echo>('/echo', payload)
      expect(r.method).toBe(m.toUpperCase())
      expect(r.contentType).toBe('application/json')
      expect(JSON.parse(r.body)).toEqual(payload)
    }
    const d = await api.delete<Echo>('/echo')
    expect(d.method).toBe('DELETE')
    expect(d.body).toBe('')

    // DELETE с телом (подтверждение паролем при удалении аккаунта)
    const dd = await api.delete<Echo>('/echo', { password: 'пароль' })
    expect(dd.method).toBe('DELETE')
    expect(JSON.parse(dd.body)).toEqual({ password: 'пароль' })
    expect(dd.contentType).toBe('application/json')
    expect(d.contentType).toBeUndefined()
  })

  it('204 и пустой 200 дают null', async () => {
    const api = createClient({ base })
    expect(await api.get('/empty')).toBeNull()
    expect(await api.get('/blank200')).toBeNull()
  })

  it('ошибка API превращается в ApiError с code, message и request_id', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/missing'))
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 404, code: 'not_found', message: 'Дело не найдено', requestId: 'rid-404' })
  })

  it('«Доступ запрещён»: нужный уровень доходит до вызывающего', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/denied'))
    expect(err).toMatchObject({ status: 403, code: 'access_denied', requestId: 'rid-403', requiredLevel: 4, requiredLevelName: 'Надзиратель' })
  })

  it('нужный уровень принимается только числом, название — только строкой', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/denied-junk'))
    expect(err).toMatchObject({ requiredLevel: 0, requiredLevelName: '' })
    const other = await caught(api.get('/missing'))
    expect(other).toMatchObject({ requiredLevel: 0, requiredLevelName: '' })
  })

  it('ошибки формы: fields доходят до вызывающего, нестроковые значения отбрасываются', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/validation'))
    expect(err).toMatchObject({ status: 422, code: 'validation', requestId: 'rid-422' })
    expect(err.fields).toEqual({ login: 'Логин: от 3 до 24 символов', password: 'Пароль: от 8 до 128 символов' })
  })

  it('замечания к документу: остаются только записи с path и message строками', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/validation'))
    expect(err.problems).toEqual([{ path: 'blocks[0].data.text', message: 'слишком длинный' }])
  })

  it('замок и конфликт редакций: кто правит и какая редакция актуальна', async () => {
    const api = createClient({ base })
    const locked = await caught(api.get('/locked'))
    expect(locked).toMatchObject({ status: 409, code: 'locked', lock: { holder: 'vera', expiresAt: '2026-09-19T12:00:00Z' } })
    const conflict = await caught(api.get('/conflict'))
    expect(conflict).toMatchObject({ status: 409, code: 'conflict', currentRevision: 7, lock: null })
  })

  it('отчёт линтера: только верные замечания; итоги считаются по ним, а не берутся из ответа', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/lint'))
    expect(err).toMatchObject({ status: 422, code: 'lint_failed' })
    expect(err.lint).toEqual({
      issues: [
        { severity: 'error', code: 'broken_link', message: 'Ссылка на О-9998: такого документа нет', block_id: 'l' },
        { severity: 'warning', code: 'no_dossier_header', message: 'Нет шапки досье' },
      ],
      errors: 1,
      warnings: 1,
    })
    expect((await caught(api.get('/lint-junk'))).lint).toBeNull()
    expect((await caught(api.get('/conflict'))).lint).toBeNull()
  })

  it('без fields — пустой объект, а не undefined', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/missing'))
    expect(err.fields).toEqual({})
    expect(err.retryAfter).toBe(0)
    expect(err.problems).toEqual([])
  })

  it('429: retryAfter из тела, а если тела нет — из заголовка Retry-After', async () => {
    const api = createClient({ base })
    const a = await caught(api.get('/limited'))
    expect(a).toMatchObject({ status: 429, code: 'rate_limited', retryAfter: 840 })
    const b = await caught(api.get('/limited-header-only'))
    expect(b).toMatchObject({ status: 429, code: 'bad_response', retryAfter: 30 })
  })

  it('не-JSON ответ об ошибке (страница хостинга) -> bad_response, request id из заголовка', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/html500'))
    expect(err).toMatchObject({ status: 500, code: 'bad_response', requestId: 'rid-html' })
  })

  it('битый JSON в успешном ответе -> bad_response', async () => {
    const api = createClient({ base })
    const err = await caught(api.get('/badjson'))
    expect(err).toMatchObject({ code: 'bad_response', status: 200 })
  })

  it('недоступный сервер -> status 0, code network', async () => {
    const api = createClient({ base: closedPortBase })
    const err = await caught(api.get('/ok'))
    expect(err).toMatchObject({ status: 0, code: 'network' })
  })

  it('таймаут -> status 0, code timeout, и быстро', async () => {
    const api = createClient({ base, timeoutMs: 150 })
    const t = Date.now()
    const err = await caught(api.get('/slow'))
    expect(err).toMatchObject({ status: 0, code: 'timeout' })
    expect(Date.now() - t).toBeLessThan(1200)
  })

  it('отмена вызывающим пробрасывается как есть и не считается сбоем связи', async () => {
    const api = createClient({ base })
    const events: ApiEvent[] = []
    api.subscribe((e) => events.push(e))
    const ctl = new AbortController()
    const p = api.get('/slow', { signal: ctl.signal })
    setTimeout(() => ctl.abort(), 50)
    const err = await caught(p)
    expect(err).not.toBeInstanceOf(ApiError)
    expect(err.name).toBe('AbortError')
    expect(events).toEqual([])
  })

  it('download: содержимое файла и имя из Content-Disposition; ошибки — как у обычных запросов', async () => {
    const events: ApiEvent[] = []
    const api = createClient({ base })
    api.subscribe((e) => events.push(e))
    const file = await api.download('/file')
    expect(file.filename).toBe('MEMO-1.md')
    expect(await file.blob.text()).toBe('# Заголовок\n')
    expect(file.blob.type).toContain('text/markdown')
    expect((await api.download('/file-unnamed')).filename).toBe('file') // без заголовка — имя по умолчанию
    expect(events).toEqual([{ ok: true }, { ok: true }])

    const err = await caught(api.download('/missing'))
    expect(err).toMatchObject({ status: 404, code: 'not_found', requestId: 'rid-404' })
    expect(events[2]).toMatchObject({ ok: false })
    await expect(api.download('file')).rejects.toThrow(TypeError)
  })

  it('путь без ведущего "/" отвергается сразу', async () => {
    const api = createClient({ base })
    await expect(api.get('ok')).rejects.toThrow(TypeError)
  })
})

describe('subscribe', () => {
  it('сообщает об успехе и о сбое; отписка работает', async () => {
    const api = createClient({ base })
    const events: ApiEvent[] = []
    const off = api.subscribe((e) => events.push(e))

    await api.get('/ok')
    await api.get('/missing').catch(() => {})
    await api.get('/maintenance').catch(() => {})
    expect(events.map((e) => e.ok)).toEqual([true, false, false])
    const [, notFound, maintenance] = events
    expect(notFound?.ok === false && notFound.error.code).toBe('not_found')
    expect(maintenance?.ok === false && maintenance.error).toMatchObject({ status: 503, code: 'maintenance' })

    off()
    await api.get('/ok')
    expect(events).toHaveLength(3)
  })
})

// @vitest-environment node
import { beforeEach, describe, expect, it } from 'vitest'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { createPinia, setActivePinia } from 'pinia'
import { ApiError, createClient } from '@/api/client'
import { classifyError, useConnectionStore } from '@/stores/connection'

const err = (status: number, code: string, requestId = '') => new ApiError({ status, code, message: code, requestId })

describe('classifyError', () => {
  it('инфраструктурные сбои -> outage', () => {
    expect(classifyError(err(0, 'network'))).toBe('outage')
    expect(classifyError(err(0, 'timeout'))).toBe('outage')
    expect(classifyError(err(502, 'upstream_unavailable'))).toBe('outage')
    expect(classifyError(err(504, 'upstream_timeout'))).toBe('outage')
    expect(classifyError(err(503, 'bad_response'))).toBe('outage')
    expect(classifyError(err(500, 'proxy_misconfigured'))).toBe('outage')
  })

  it('режим обслуживания -> maintenance', () => {
    expect(classifyError(err(503, 'maintenance'))).toBe('maintenance')
  })

  it('обычные ошибки прикладного уровня — связь есть', () => {
    expect(classifyError(err(401, 'unauthorized'))).toBe('online')
    expect(classifyError(err(404, 'not_found'))).toBe('online')
    expect(classifyError(err(422, 'validation'))).toBe('online')
    expect(classifyError(err(429, 'rate_limited'))).toBe('online')
    expect(classifyError(err(500, 'internal'))).toBe('online')
  })
})

describe('connection store', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('стартует в unknown', () => {
    expect(useConnectionStore().state).toBe('unknown')
  })

  it('успех -> online и сброс номера обращения', () => {
    const s = useConnectionStore()
    s.apply({ ok: false, error: err(502, 'upstream_unavailable', 'rid-1') })
    expect(s).toMatchObject({ state: 'outage', requestId: 'rid-1' })
    s.apply({ ok: true })
    expect(s).toMatchObject({ state: 'online', requestId: '' })
  })

  it('maintenance и обычная ошибка после сбоя', () => {
    const s = useConnectionStore()
    s.apply({ ok: false, error: err(503, 'maintenance', 'rid-2') })
    expect(s.state).toBe('maintenance')
    s.apply({ ok: false, error: err(404, 'not_found', 'rid-3') })
    expect(s).toMatchObject({ state: 'online', requestId: '' })
  })

  it('attach подписывается на клиент и возвращает отписку', async () => {
    const srv: Server = createServer((req, res) => {
      if (req.url === '/api/down') {
        res.writeHead(502, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ error: { code: 'upstream_unavailable', message: 'Сбой архива', request_id: 'rid-77' } }))
        return
      }
      res.writeHead(200, { 'Content-Type': 'application/json' })
      res.end('{}')
    })
    await new Promise<void>((r) => srv.listen(0, '127.0.0.1', r))
    try {
      const client = createClient({ base: `http://127.0.0.1:${(srv.address() as AddressInfo).port}/api` })
      const s = useConnectionStore()
      const off = s.attach(client)

      await client.get('/down').catch(() => {})
      expect(s).toMatchObject({ state: 'outage', requestId: 'rid-77' })
      await client.get('/up')
      expect(s.state).toBe('online')

      off()
      await client.get('/down').catch(() => {})
      expect(s.state).toBe('online')
    } finally {
      srv.closeAllConnections()
      await new Promise((r) => srv.close(r))
    }
  })
})

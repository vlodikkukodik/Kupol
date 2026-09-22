// Охрана страниц панели команды — через настоящий маршрутизатор приложения и настоящий HTTP-сервер,
// который отвечает на /api/auth/session так, как отвечает Go API.
import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import http from 'node:http'
import type { AddressInfo } from 'node:net'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, type Router } from 'vue-router'
import type { UserDTO } from '@/api/generated/httpapi'
import type { Capability, useAuthStore } from '@/stores/auth'

let server: http.Server
let session: { user: UserDTO | null } = { user: null } // что сервер отвечает сейчас
let mode: 'ok' | 'down' = 'ok'
let sessionHits = 0

const ALL: Capability[] = ['team_panel', 'write_drafts', 'review', 'publish', 'edit_published', 'manage_glossary', 'manage_templates', 'manage_timeline', 'manage_team']
const user = (capabilities: string[], extra: Partial<UserDTO> = {}): UserDTO => ({
  login: 'tester',
  level: 1,
  level_name: 'Посетитель',
  directorate: false,
  roles: [],
  capabilities,
  created_at: '2026-09-19T12:00:00Z',
  totp_enabled: false,
  xp: 0,
  login_streak: 0,
  next_level_xp: 100,
  ...extra,
})

beforeAll(async () => {
  server = http.createServer((req, res) => {
    const send = (status: number, obj: unknown) => {
      res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' })
      res.end(JSON.stringify(obj))
    }
    if (req.url === '/api/auth/session') {
      sessionHits++
      if (mode === 'down') return send(502, { error: { code: 'upstream_unavailable', message: 'Сбой архива' } })
      return send(200, session)
    }
    send(404, { error: { code: 'not_found', message: 'нет' } })
  })
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))
  vi.stubEnv('VITE_API_BASE', `http://127.0.0.1:${(server.address() as AddressInfo).port}/api`)
})

afterAll(async () => {
  vi.unstubAllEnvs()
  server.closeAllConnections()
  await new Promise((r) => server.close(r))
})

let auth: ReturnType<typeof useAuthStore>
let router: Router

beforeEach(async () => {
  vi.resetModules()
  mode = 'ok'
  sessionHits = 0
  session = { user: null }
  const { createAppRouter } = await import('@/router')
  const { useAuthStore: store } = await import('@/stores/auth')
  setActivePinia(createPinia())
  auth = store()
  router = createAppRouter(createMemoryHistory())
})

/** Открыть адрес и вернуть, где оказались. */
async function visit(path: string) {
  await router.push(path)
  const r = router.currentRoute.value
  return { name: r.name, path: r.path, fullPath: r.fullPath }
}

async function signIn(u: UserDTO) {
  session = { user: u }
  await auth.load(true)
}

describe('панель команды: охрана страниц', () => {
  it('гость возвращается на главную с адресом возврата', async () => {
    session = { user: null }
    const at = await visit('/team')
    expect(at.name).toBe('home')
    expect(at.fullPath).toBe('/?next=/team')
  })

  it('вошедший без прав видит «Дело не найдено» по тому же адресу — существование панели не раскрывается', async () => {
    await signIn(user([]))
    for (const path of ['/team', '/team/members']) {
      const at = await visit(path)
      expect(at.name, path).toBe('not-found')
      expect(at.path, path).toBe(path)
    }
  })

  it('участник команды без права выдавать роли: панель открывается, «Команда» — нет', async () => {
    await signIn(user(['team_panel', 'write_drafts']))
    expect((await visit('/team')).name).toBe('team')
    const at = await visit('/team/members')
    expect(at.name).toBe('not-found')
    expect(at.path).toBe('/team/members')
  })

  it('Директорат (все права) открывает оба раздела', async () => {
    await signIn(user(ALL, { directorate: true }))
    expect((await visit('/team')).name).toBe('team')
    expect((await visit('/team/members')).name).toBe('team-members')
  })

  it('права перечитываются при входе на страницу: снятая роль перестаёт действовать без перезагрузки сайта', async () => {
    await signIn(user(ALL, { directorate: true }))
    expect((await visit('/team/members')).name).toBe('team-members')

    // Директорат сняли: интерфейс ещё помнит старые права, а сервер уже нет
    session = { user: user(['team_panel']) }
    await router.push('/catalog')
    const hitsBefore = sessionHits
    const at = await visit('/team/members')
    expect(sessionHits).toBeGreaterThan(hitsBefore)
    expect(at.name).toBe('not-found')
  })

  it('выданная роль тоже действует сразу', async () => {
    await signIn(user([]))
    expect((await visit('/team')).name).toBe('not-found')
    session = { user: user(['team_panel']) }
    expect((await visit('/catalog')).name).toBe('catalog')
    expect((await visit('/team')).name).toBe('team')
  })

  it('если сервер сессию не подтвердил (вышли в другой вкладке), страница не открывается', async () => {
    await signIn(user(ALL, { directorate: true }))
    session = { user: null }
    await router.push('/team')
    expect(router.currentRoute.value.name).not.toBe('team')
  })

  it('при сбое связи решает то, что уже известно, а охрана не падает', async () => {
    await signIn(user(['team_panel']))
    mode = 'down'
    expect((await visit('/team')).name).toBe('team')
    expect((await visit('/team/members')).name).toBe('not-found')
  })

  it('обычные страницы права не спрашивают и лишних запросов не делают', async () => {
    await signIn(user([]))
    const before = sessionHits
    await visit('/catalog')
    await visit('/file')
    expect(sessionHits).toBe(before)
  })
})

describe('auth.can', () => {
  it('смотрит только на список прав от сервера', async () => {
    await signIn(user(['team_panel']))
    expect(auth.can('team_panel')).toBe(true)
    expect(auth.can('manage_team')).toBe(false)
    expect(auth.can('' as Capability)).toBe(false)
    // роль «editor» в списке ролей ничего не даёт: права выдаёт сервер
    await signIn(user([], { roles: [{ id: 'editor', name: 'Редактор' }] }))
    expect(auth.can('publish')).toBe(false)
  })

  it('гость ничего не может', () => {
    auth.$patch({ user: null })
    expect(auth.can('team_panel')).toBe(false)
  })
})

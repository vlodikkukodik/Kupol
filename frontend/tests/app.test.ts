// Приложение целиком (App + роутер + Pinia + клиент) против настоящего HTTP-сервера,
// который отвечает на /api/health так, как это делают Go API и PHP-прокси.
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import http from 'node:http'
import type { AddressInfo } from 'node:net'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, type Pinia } from 'pinia'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { pageTitle } from '@/router'

let server: http.Server
let mode: 'ok' | 'outage' | 'maintenance' = 'ok'
let hits = 0

beforeAll(async () => {
  server = http.createServer((req, res) => {
    hits++
    const send = (status: number, obj: unknown) => {
      res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'X-Request-Id': 'rid-app-1' })
      res.end(JSON.stringify(obj))
    }
    // Сбой и обслуживание действуют на все пути: так ведут себя прокси и API, а порядок запросов не важен.
    if (mode === 'outage') return send(502, { error: { code: 'upstream_unavailable', message: 'Сбой архива', request_id: 'rid-app-1' } })
    if (mode === 'maintenance') return send(503, { error: { code: 'maintenance', message: 'Архив закрыт на инвентаризацию', request_id: 'rid-app-1' } })
    if (req.url === '/api/health') return send(200, { status: 'ok', db: 'ok' })
    if (req.url === '/api/auth/session') return send(200, { user: null }) // Гражданин
    if (req.url?.startsWith('/api/documents/recent')) return send(200, { items: [] })
    send(404, { error: { code: 'not_found', message: 'Дело не найдено' } })
  })
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))
  vi.stubEnv('VITE_API_BASE', `http://127.0.0.1:${(server.address() as AddressInfo).port}/api`)
})

afterAll(async () => {
  vi.unstubAllEnvs()
  server.closeAllConnections()
  await new Promise((r) => server.close(r))
})

let wrapper: VueWrapper | undefined
beforeEach(() => {
  mode = 'ok'
  hits = 0
  vi.resetModules()
})
afterEach(() => wrapper?.unmount())

async function mountApp(path = '/') {
  const { default: App } = await import('@/App.vue')
  const { routes } = await import('@/router')
  const { useConnectionStore } = await import('@/stores/connection')
  const { createQueryClient } = await import('@/api/query')

  // Реальные маршруты приложения + маршрут, которому нужен API.
  const last = routes.at(-1)
  if (!last) throw new Error('нет маршрутов')
  const router: Router = createRouter({
    history: createMemoryHistory(),
    routes: [
      ...routes.slice(0, -1),
      { path: '/needs-api', component: { template: '<p id="api-page">Страница с данными</p>' }, meta: { needsApi: true } },
      last,
    ],
  })
  router.afterEach((to) => {
    document.title = pageTitle(to.meta)
  })
  const pinia: Pinia = createPinia()
  useConnectionStore(pinia).attach()

  void router.push(path)
  await router.isReady()
  wrapper = mount(App, { global: { plugins: [pinia, router, [VueQueryPlugin, { queryClient: createQueryClient() }]] } })

  // Монитор связи сразу шлёт настоящий запрос на сервер — ждём, пока придёт ответ.
  const connection = useConnectionStore(pinia)
  await vi.waitFor(() => expect(connection.state).not.toBe('unknown'), { timeout: 3000 })
  await flushPromises()
  return { router, connection }
}

/** Нажать кнопку на странице ошибки и дождаться, пока завершится проверка связи. */
async function retry(connection: { checking: boolean }) {
  await wrapper?.find('main button.ui-button').trigger('click')
  await vi.waitFor(() => expect(connection.checking).toBe(false), { timeout: 3000 })
  await flushPromises()
}

const text = () => wrapper?.text() ?? ''
const h1 = () => wrapper?.find('h1').text()

describe('главная', () => {
  it('показывает «дверь»: название и гриф; примечание автора — только мелко в подвале, не на самой странице', async () => {
    await mountApp('/')
    expect(h1()).toBe('Купол')
    expect(text()).toContain('Комитет Управления Паранормальными Объектами и Локациями')
    expect(text()).toContain('Форма КУПОЛ-1')
    expect(wrapper?.find('footer .disclaimer').text()).toContain('художественный вымысел')
    expect(wrapper?.find('main').text()).not.toContain('художественный вымысел')
    expect(wrapper?.find('main aside').exists()).toBe(false)
  })

  it('примечание автора в подвале есть и на других страницах', async () => {
    await mountApp('/needs-api')
    expect(wrapper?.find('footer .disclaimer').text()).toContain('художественный вымысел')
  })

  it('при старте проверяет связь и показывает «установлена»', async () => {
    const { connection } = await mountApp('/')
    expect(hits).toBeGreaterThanOrEqual(1)
    expect(connection.state).toBe('online')
    expect(wrapper?.find('.status').text()).toBe('Связь с архивом: установлена')
    expect(wrapper?.find('.status').attributes('data-state')).toBe('online')
  })

  it('есть ссылка «К содержимому» и main с id', async () => {
    await mountApp('/')
    expect(wrapper?.find('a.skip-link').attributes('href')).toBe('#content')
    expect(wrapper?.find('main#content').exists()).toBe(true)
  })

  it('гость видит приглашение оформить допуск', async () => {
    await mountApp('/')
    expect(text()).toContain('Уровень 0 · Гражданин')
    expect(wrapper?.findAll('button').some((b) => b.text().includes('Получить допуск'))).toBe(true)
  })
})

describe('сбой связи', () => {
  it('страница без API остаётся доступной, в подвале — «нарушена»', async () => {
    mode = 'outage'
    const { connection } = await mountApp('/')
    expect(connection.state).toBe('outage')
    expect(h1()).toBe('Купол')
    expect(wrapper?.find('.status').text()).toBe('Связь с архивом: нарушена')
  })

  it('страница с API заменяется на «Сбой архива» с номером обращения', async () => {
    mode = 'outage'
    await mountApp('/needs-api')
    expect(h1()).toBe('Сбой архива')
    expect(wrapper?.find('[role="alert"]').exists()).toBe(true)
    expect(text()).toContain('Номер обращения: rid-app-1')
    expect(wrapper?.find('#api-page').exists()).toBe(false)
  })

  it('«Повторить» возвращает страницу, когда связь восстановлена', async () => {
    mode = 'outage'
    const { connection } = await mountApp('/needs-api')
    expect(h1()).toBe('Сбой архива')

    mode = 'ok'
    await retry(connection)
    expect(wrapper?.find('#api-page').exists()).toBe(true)
    expect(wrapper?.find('h1').exists()).toBe(false)
  })

  it('«Повторить» при сохраняющемся сбое остаётся на ошибке', async () => {
    mode = 'outage'
    const { connection } = await mountApp('/needs-api')
    const before = hits
    await retry(connection)
    expect(hits).toBeGreaterThan(before)
    expect(h1()).toBe('Сбой архива')
  })
})

describe('обслуживание', () => {
  it('закрывает весь сайт, даже страницы без API', async () => {
    mode = 'maintenance'
    await mountApp('/')
    expect(h1()).toBe('Архив закрыт на инвентаризацию')
    expect(wrapper?.find('article[role="status"]').exists()).toBe(true)
    expect(wrapper?.find('.status').text()).toBe('Архив закрыт на инвентаризацию')
  })

  it('снимается, когда архив снова открыт', async () => {
    mode = 'maintenance'
    const { connection } = await mountApp('/')
    mode = 'ok'
    await retry(connection)
    expect(h1()).toBe('Купол')
  })
})

describe('404', () => {
  it('неизвестный адрес -> «Дело не найдено», заголовок вкладки, ссылка на главную', async () => {
    await mountApp('/no/such/document')
    expect(h1()).toBe('Дело не найдено')
    expect(text()).toContain('Изъято')
    expect(document.title).toBe('Дело не найдено — КУПОЛ')
    expect(wrapper?.find('main a.ui-button').attributes('href')).toBe('/')
  })

  it('заголовок вкладки главной', async () => {
    await mountApp('/')
    expect(document.title).toBe('КУПОЛ — Центральный архив')
  })
})

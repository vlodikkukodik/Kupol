import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { createServer, type Server } from 'node:http'
import { startFakeHelper } from './helpers'

const DOWNLOAD_PORT = 18081
const GATEWAY_PORT = 18091

function listen(server: Server, port: number): Promise<void> {
  return new Promise((resolve, reject) => {
    server.once('error', reject)
    server.listen(port, '127.0.0.1', () => resolve())
  })
}

// Установка WordPress в один клик. Дистрибутив отдаёт «сервер загрузок» теста (сумма закреплена в каталоге стенда), а «шлюз с
// WordPress» играет установщик: страница установки, форма и корень сайта. Базу создаёт настоящая MariaDB стенда.
test('установка WordPress: форма, ход, пароли один раз, состояние «установлено»', async ({ page, request }) => {
  test.setTimeout(120_000)
  const seen: string[] = []
  const helper = startFakeHelper(seen)
  const installed = new Set<string>()
  const forms: string[] = []
  const download = createServer((_req, res) => res.end(readFileSync('/tmp/vh-e2e-cms/wordpress.zip')))
  const gateway = createServer((req, res) => {
    const host = req.headers.host ?? ''
    if (req.url?.startsWith('/wp-admin/install.php') && req.method === 'GET') return void res.end('install.php')
    if (req.url?.startsWith('/wp-admin/install.php')) {
      let body = ''
      req.on('data', (c) => (body += c))
      req.on('end', () => {
        if (req.url?.includes('step=1')) return void res.end('step one') // выбор языка: WordPress качает языковой пакет
        forms.push(body)
        installed.add(host)
        res.end('Success!')
      })
      return
    }
    if (req.url === '/' && !installed.has(host)) {
      res.writeHead(302, { Location: '/wp-admin/install.php' })
      return void res.end()
    }
    res.end(req.url === '/' ? '<link href="/wp-content/x.css">' : 'ok')
  })
  await listen(download, DOWNLOAD_PORT)
  await listen(gateway, GATEWAY_PORT)
  let token = ''
  let siteId = 0
  try {
    const admin = process.env.E2E_ADMIN!
    const login = await request.post('/api/auth/login', { data: { login: admin, password: process.env.E2E_ADMIN_PASSWORD } })
    token = (await login.json()).access_token as string
    const auth = { Authorization: `Bearer ${token}` }
    const slug = `wp${Date.now().toString(36)}`.slice(0, 20)
    siteId = (await (await request.post('/api/sites', { data: { slug }, headers: auth })).json()).site.id as number

    await page.goto('/login')
    await page.getByLabel('Email или имя').fill(admin)
    await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByText(`Здравствуйте, ${admin}`)).toBeVisible()
    await page.goto(`/sites/${siteId}/apps`)
    await expect(page.getByRole('heading', { name: 'Приложения' })).toBeVisible()

    // Условия выполнены, ошибки формы видны до отправки
    await expect(page.getByText('Пустая папка сайта')).toBeVisible()
    await page.getByRole('textbox', { name: 'Название сайта' }).fill('Мой блог')
    await page.getByRole('textbox', { name: 'Логин администратора' }).fill('a b')
    await page.getByRole('button', { name: 'Установить' }).click()
    await expect(page.getByText('Логин: 3–60 символов')).toBeVisible()
    await page.getByRole('textbox', { name: 'Логин администратора' }).fill('vladadmin')

    await page.getByRole('button', { name: 'Установить' }).click()
    // Ход установки, затем окно с паролями
    await expect(page.getByTestId('cms-progress').or(page.getByTestId('cms-admin-password'))).toBeVisible()
    await expect(page.getByTestId('cms-admin-password')).toBeVisible({ timeout: 60_000 })
    expect((await page.getByTestId('cms-admin-password').innerText()).length).toBe(24)
    await expect(page.getByTestId('cms-admin-user')).toHaveText('vladadmin')
    await expect(page.getByText(/WordPress установлен/)).toBeVisible()
    await page.getByTestId('cms-close').click()

    // Состояние «установлено»; пароли больше не показываются
    await expect(page.getByTestId('cms-installed')).toBeVisible()
    await expect(page.getByTestId('cms-installed').getByText(/Установлено: WordPress 7\.1\.2/)).toBeVisible()
    await page.reload()
    await expect(page.getByTestId('cms-installed')).toBeVisible()
    await expect(page.getByTestId('cms-admin-password')).toHaveCount(0)

    expect(seen.some((s) => s.startsWith('apply:php:'))).toBeTruthy()
    expect(forms.length).toBe(1)
    expect(forms[0]).toContain('user_name=vladadmin')
    expect(forms[0]).toContain('language=ru_RU')
  } finally {
    // Уборка: база, созданная установкой, и сайт (иначе лимит баз стенда исчерпается).
    if (token) {
      const auth = { Authorization: `Bearer ${token}` }
      const dbs = (await (await request.get('/api/databases', { headers: auth })).json()) as { databases: { id: number; name: string }[] }
      for (const d of dbs.databases.filter((x) => x.name.includes(`_wp${siteId}`))) await request.delete(`/api/databases/${d.id}`, { headers: auth })
      if (siteId) await request.delete(`/api/sites/${siteId}`, { headers: auth })
    }
    clearInterval(helper)
    download.close()
    gateway.close()
  }
})

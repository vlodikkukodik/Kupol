import { expect, test } from '@playwright/test'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { chmodSync, writeFileSync } from 'node:fs'
import { startFakeHelper } from './helpers'

const run = promisify(execFile)

// SSH и веб-терминал: пара ключей из панели, включение доступа, вход настоящим ssh-клиентом на SSH-сервер панели и терминал в браузере.
test('SSH: ключи, вход по ssh и терминал в браузере', async ({ page, request }) => {
  test.setTimeout(120_000)
  const helper = startFakeHelper([])
  let token = ''
  let siteId = 0
  try {
    const admin = process.env.E2E_ADMIN!
    token = (await (await request.post('/api/auth/login', { data: { login: admin, password: process.env.E2E_ADMIN_PASSWORD } })).json()).access_token as string
    const auth = { Authorization: `Bearer ${token}` }
    const slug = `sh${Date.now().toString(36)}`.slice(0, 20)
    siteId = (await (await request.post('/api/sites', { data: { slug }, headers: auth })).json()).site.id as number

    await page.goto('/login')
    await page.getByLabel('Email или имя').fill(admin)
    await page.getByLabel('Пароль').fill(process.env.E2E_ADMIN_PASSWORD!)
    await page.getByRole('button', { name: 'Войти' }).click()
    await expect(page.getByText(`Здравствуйте, ${admin}`)).toBeVisible()

    // Ключи: ошибка проверки, затем пара из панели
    await page.getByRole('navigation', { name: 'Основное меню' }).getByText('SSH-ключи').click()
    await expect(page.getByRole('heading', { name: 'SSH-ключи' })).toBeVisible()
    await page.getByRole('textbox', { name: 'Название' }).fill('e2e')
    await page.getByRole('textbox', { name: 'Открытый ключ' }).fill('не ключ')
    await page.getByRole('button', { name: 'Добавить ключ' }).click()
    await expect(page.getByText('Не удалось разобрать ключ')).toBeVisible()
    await page.getByRole('button', { name: 'Создать пару ключей' }).click()
    await expect(page.getByTestId('private-key')).toContainText('BEGIN OPENSSH PRIVATE KEY')
    const pem = (await page.getByTestId('private-key').innerText()).trim() + '\n'
    const keyFile = `/tmp/vh-e2e-shell/id_e2e_${slug}`
    writeFileSync(keyFile, pem)
    chmodSync(keyFile, 0o600)
    await page.getByTestId('private-close').click()
    await expect(page.getByTestId('key-e2e')).toBeVisible()
    await expect(page.getByTestId('key-e2e').getByText('ещё не входили')).toBeVisible()

    // Включение доступа на странице сайта
    await page.goto(`/sites/${siteId}/terminal`)
    await expect(page.getByRole('heading', { name: 'Терминал и SSH' })).toBeVisible()
    await page.getByRole('switch', { name: 'Доступ к оболочке' }).click()
    await expect(page.getByTestId('ssh-command')).toContainText(`ssh ${slug}.${admin}@ssh.example.test -p 2223`)

    // Настоящий ssh-клиент: вход по ключу, команда, код выхода
    const base = ['-p', '2223', '-o', 'StrictHostKeyChecking=no', '-o', 'UserKnownHostsFile=/dev/null', '-o', 'BatchMode=yes', '-o', 'LogLevel=ERROR']
    const ssh = (login: string, cmd: string, key = true) =>
      run('ssh', [...(key ? ['-i', keyFile, '-o', 'IdentitiesOnly=yes'] : []), ...base, `${login}@127.0.0.1`, cmd], { encoding: 'utf8', timeout: 30_000 })
    expect((await ssh(`${slug}.${admin}`, 'echo hello-from-ssh; pwd')).stdout).toContain('hello-from-ssh')
    await expect(ssh(`${slug}.${admin}`, 'exit 3')).rejects.toMatchObject({ code: 3 })
    // Чужой логин и вход без ключа не пускают
    await expect(ssh(`nosuch.${admin}`, 'true')).rejects.toBeTruthy()
    await expect(ssh(`${slug}.${admin}`, 'true', false)).rejects.toBeTruthy()

    // Отметка последнего входа появилась
    await page.goto('/ssh')
    await expect(page.getByTestId('key-e2e')).toBeVisible()
    await expect(page.getByTestId('key-e2e').getByText('ещё не входили')).toHaveCount(0)

    // Веб-терминал
    await page.goto(`/sites/${siteId}/terminal`)
    await page.getByRole('button', { name: 'Открыть терминал' }).click()
    await expect(page.getByTestId('terminal')).toBeVisible()
    await page.locator('.xterm-helper-textarea').focus()
    await page.keyboard.type('echo hello-from-web\n')
    await expect(page.getByTestId('terminal')).toContainText('hello-from-web', { timeout: 15_000 })
    await page.keyboard.type('exit 2\n')
    await expect(page.getByTestId('terminal-note')).toContainText('Сеанс завершён (код 2)')
  } finally {
    if (token) {
      const auth = { Authorization: `Bearer ${token}` }
      if (siteId) await request.put(`/api/sites/${siteId}/shell`, { data: { enabled: false }, headers: auth })
      const keys = (await (await request.get('/api/ssh/keys', { headers: auth })).json()) as { keys: { id: number }[] }
      for (const k of keys.keys) await request.delete(`/api/ssh/keys/${k.id}`, { headers: auth })
      if (siteId) await request.delete(`/api/sites/${siteId}`, { headers: auth })
    }
    clearInterval(helper)
  }
})

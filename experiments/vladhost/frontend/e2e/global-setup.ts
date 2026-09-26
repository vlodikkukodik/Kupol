import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import { chmodSync, existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { userInfo } from 'node:os'

// Заводит свежего администратора на каждый прогон и чистит каталог сайтов.
const SHELL_DIR = '/tmp/vh-e2e-shell'

// Посредник оболочек стенда: настоящий бинарь, но вместо systemd-run — скрипт, который просто выполняет команду (песочницы на стенде нет).
async function startShellBroker(): Promise<ChildProcess> {
  rmSync(SHELL_DIR, { recursive: true, force: true })
  mkdirSync(SHELL_DIR, { recursive: true })
  writeFileSync(`${SHELL_DIR}/systemd-run`, '#!/bin/bash\nwhile [ "${1:-}" != "--" ]; do shift; done\nshift\nexec "$@"\n')
  writeFileSync(`${SHELL_DIR}/systemctl`, '#!/bin/bash\n')
  chmodSync(`${SHELL_DIR}/systemd-run`, 0o755)
  chmodSync(`${SHELL_DIR}/systemctl`, 0o755)
  const child = spawn('../bin/vladhost', ['shell-broker'], {
    env: {
      ...process.env,
      VLADHOST_SHELL_SOCKET: `${SHELL_DIR}/shell.sock`,
      VLADHOST_RUNTIME_DIR: '/tmp/vh-e2e-runtime',
      VLADHOST_SITES_ROOT: '/tmp/vh-e2e-sites',
      VLADHOST_PANEL_USER: userInfo().username,
      VLADHOST_SHELL_SYSTEMD_RUN: `${SHELL_DIR}/systemd-run`,
      VLADHOST_SHELL_SYSTEMCTL: `${SHELL_DIR}/systemctl`,
    },
    stdio: 'inherit',
  })
  for (let i = 0; i < 100 && !existsSync(`${SHELL_DIR}/shell.sock`); i++) await new Promise((r) => setTimeout(r, 50))
  return child
}

export default async function globalSetup() {
  rmSync('/tmp/vh-e2e-sites', { recursive: true, force: true })
  rmSync('/tmp/vh-e2e-domains', { recursive: true, force: true })
  rmSync('/tmp/vh-e2e-logs', { recursive: true, force: true })
  rmSync('/tmp/vh-e2e-mail', { recursive: true, force: true })
  // Среды выполнения: исполнителя (root-скрипт) на стенде нет, его роль играет сам тест; здесь только папки обмена и список установленного.
  rmSync('/tmp/vh-e2e-runtime', { recursive: true, force: true })
  mkdirSync('/tmp/vh-e2e-runtime/queue', { recursive: true })
  mkdirSync('/tmp/vh-e2e-runtime/results', { recursive: true })
  mkdirSync('/tmp/vh-e2e-runtime/shell', { recursive: true })
  writeFileSync('/tmp/vh-e2e-runtime/caps', 'php=8.2,8.3\nnode=22.1.0\npython=\n')
  const id = Date.now().toString(36)
  const admin = { username: `adm${id}`, email: `adm${id}@example.com`, password: 'e2e-password-1' }
  execFileSync('../bin/vladhost', ['admin', 'create', '--email', admin.email, '--username', admin.username], {
    env: { ...process.env, VLADHOST_ADMIN_PASSWORD: admin.password },
    stdio: 'inherit',
  })
  process.env.E2E_ADMIN = admin.username
  process.env.E2E_ADMIN_PASSWORD = admin.password
  const broker = await startShellBroker()
  return () => {
    broker.kill()
  }
}

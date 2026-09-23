import { execFileSync } from 'node:child_process'
import { rmSync } from 'node:fs'

// Заводит свежего администратора на каждый прогон и чистит каталог сайтов.
export default function globalSetup() {
  rmSync('/tmp/vh-e2e-sites', { recursive: true, force: true })
  const id = Date.now().toString(36)
  const admin = { username: `adm${id}`, email: `adm${id}@example.com`, password: 'e2e-password-1' }
  execFileSync('../bin/vladhost', ['admin', 'create', '--email', admin.email, '--username', admin.username], {
    env: { ...process.env, VLADHOST_ADMIN_PASSWORD: admin.password },
    stdio: 'inherit',
  })
  process.env.E2E_ADMIN = admin.username
  process.env.E2E_ADMIN_PASSWORD = admin.password
}

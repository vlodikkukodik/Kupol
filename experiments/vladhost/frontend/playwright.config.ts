import { defineConfig } from '@playwright/test'

// Сквозные тесты идут через настоящий бэкенд (bin/vladhost, нужна БД: make db-up) и Vite с прокси /api.
export default defineConfig({
  testDir: 'e2e',
  globalSetup: './e2e/global-setup.ts',
  timeout: 60_000,
  workers: 1,
  reporter: 'list',
  use: { baseURL: 'http://127.0.0.1:5174', locale: 'ru-RU', trace: 'retain-on-failure' },
  webServer: [
    {
      command: '../bin/vladhost serve',
      url: 'http://127.0.0.1:8090/api/healthz',
      reuseExistingServer: false,
      env: {
        VLADHOST_SITES_ROOT: '/tmp/vh-e2e-sites',
        VLADHOST_COOKIE_SECURE: 'false',
        VLADHOST_FTP_ADDR: '127.0.0.1:2121',
        VLADHOST_FTP_PASSIVE_PORTS: '42300-42310',
      },
    },
    { command: 'npm run dev -- --host 127.0.0.1', url: 'http://127.0.0.1:5174', reuseExistingServer: false },
  ],
})

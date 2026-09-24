import { defineConfig } from '@playwright/test'

const DEV = 'http://127.0.0.1:5174'
const PREVIEW = 'http://127.0.0.1:4180'

// Сквозные тесты идут через настоящий бэкенд (bin/vladhost, нужна БД: make db-up):
//  - проект dev — Vite dev-сервер с прокси /api;
//  - проект csp — production-сборка (vite preview) под тем же CSP, что на бою.
export default defineConfig({
  testDir: 'e2e',
  globalSetup: './e2e/global-setup.ts',
  timeout: 60_000,
  workers: 1,
  reporter: 'list',
  use: { locale: 'ru-RU', trace: 'retain-on-failure' },
  projects: [
    { name: 'dev', testIgnore: /(csp|shots)\.spec/, use: { baseURL: DEV } },
    { name: 'csp', testMatch: /csp\.spec/, use: { baseURL: PREVIEW } },
    { name: 'shots', testMatch: /shots\.spec/, use: { baseURL: DEV } },
  ],
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
    { command: 'npm run dev -- --host 127.0.0.1', url: DEV, reuseExistingServer: false },
    { command: 'npm run preview -- --host 127.0.0.1 --port 4180 --strictPort', url: PREVIEW, reuseExistingServer: false },
  ],
})

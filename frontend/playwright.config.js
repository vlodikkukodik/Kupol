import { defineConfig, devices } from '@playwright/test'
import { existsSync } from 'node:fs'
import { resolve } from 'node:path'

// Порты берём из корневого .env, как и остальной стек.
const envFile = resolve(import.meta.dirname, '..', '.env')
if (existsSync(envFile)) process.loadEnvFile(envFile)
const port = process.env.KUPOL_FRONT_PORT || '5173'

// По умолчанию тесты идут против ВСЕГО настоящего стека dev:
// Chromium -> Vite -> PHP-прокси -> Go API -> PostgreSQL (стек поднимает scripts/dev.sh).
// KUPOL_E2E_BASE_URL — прогнать те же тесты против уже развёрнутого сайта
// (scripts/e2e-apache.sh: боевая сборка на Apache + PHP 8.3).
const external = process.env.KUPOL_E2E_BASE_URL
const baseURL = external || `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: 'e2e',
  outputDir: 'e2e/results',
  globalSetup: './e2e/global-setup.js', // собирает CLI и загружает документы-фикстуры; возвращает очистку
  fullyParallel: true,
  reporter: [['list']],
  use: { baseURL, ...devices['Desktop Chrome'], locale: 'ru-RU' },
  webServer: external
    ? undefined
    : {
        command: '../scripts/dev.sh',
        // готов, когда вся цепочка до БД отвечает
        url: `${baseURL}/api/health`,
        reuseExistingServer: true,
        timeout: 180_000,
      },
})

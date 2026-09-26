import { defineConfig } from '@playwright/test'
import { createHash } from 'node:crypto'
import { mkdirSync, writeFileSync } from 'node:fs'
import { makeZip } from './e2e/zip'

// Установка приложений: «WordPress» стенда — крошечный архив, который отдаёт сам тест (порт 18081), с закреплённой суммой в каталоге.
// Каталог читается при старте сервера, поэтому файлы создаются здесь, а не в globalSetup.
export const CMS_DOWNLOAD_PORT = 18081
export const CMS_GATEWAY_PORT = 18091
const CMS_DIR = '/tmp/vh-e2e-cms'
mkdirSync(CMS_DIR, { recursive: true })
const wpZip = makeZip({
  'wordpress/index.php': '<?php // front',
  'wordpress/wp-login.php': '<?php // login',
  'wordpress/wp-admin/install.php': '<?php // installer',
  'wordpress/wp-content/index.php': '<?php // silence',
})
writeFileSync(`${CMS_DIR}/wordpress.zip`, wpZip)
writeFileSync(
  `${CMS_DIR}/catalog.json`,
  JSON.stringify([
    {
      id: 'wordpress',
      name: 'WordPress',
      version: '7.1.2',
      url: `http://127.0.0.1:${CMS_DOWNLOAD_PORT}/wordpress.zip`,
      sha256: createHash('sha256').update(wpZip).digest('hex'),
    },
  ]),
)

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
        VLADHOST_SERVER_IPS: '203.0.113.10',
        VLADHOST_DOMAINS_DIR: '/tmp/vh-e2e-domains',
        VLADHOST_LOG_DIR: '/tmp/vh-e2e-logs',
        // Базы данных: те же серверы, что поднимает `make db-up` (PostgreSQL 5433, MariaDB 3307)
        VLADHOST_DB_PG_ADMIN_URL: 'postgres://vladhost:vladhost@127.0.0.1:5433/vladhost?sslmode=disable',
        VLADHOST_DB_MARIADB_ADMIN_DSN: 'root:vladhost@tcp(127.0.0.1:3307)/',
        VLADHOST_DB_HOST: '127.0.0.1',
        VLADHOST_RUNTIME_DIR: '/tmp/vh-e2e-runtime',
        // SSH и веб-терминал: посредник оболочек стенда поднимает globalSetup
        VLADHOST_SHELL_SOCKET: '/tmp/vh-e2e-shell/shell.sock',
        VLADHOST_SSH_ADDR: '127.0.0.1:2223',
        VLADHOST_SSH_HOST_KEY: '/tmp/vh-e2e-shell/host_key',
        VLADHOST_SSH_HOST: 'ssh.example.test',
        VLADHOST_DNS_NS: 'ns.example.test,ns2.example.test', // собственный DNS; исполнителя играет тест
        VLADHOST_WEBMAIL_URL: 'https://webmail.example.test',
        VLADHOST_MAIL_HOST: 'mail.example.test', // почта на своих доменах; исполнителя играет тест
        VLADHOST_CMS_CATALOG: `${CMS_DIR}/catalog.json`,
        VLADHOST_GATEWAY_URL: `http://127.0.0.1:${CMS_GATEWAY_PORT}`,
        VLADHOST_MAIL_SPOOL: '/tmp/vh-e2e-mail', // письма складываются файлами .eml: SMTP в стенде нет
      },
    },
    { command: 'npm run dev -- --host 127.0.0.1', url: DEV, reuseExistingServer: false },
    { command: 'npm run preview -- --host 127.0.0.1 --port 4180 --strictPort', url: PREVIEW, reuseExistingServer: false },
  ],
})

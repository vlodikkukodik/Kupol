import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

// Та же политика, что на боевом nginx (deploy/nginx/vladhost-https.conf). При изменении правьте оба места:
// e2e/csp.spec.ts гоняет production-сборку под этим CSP и падает, если она что-то нарушает.
const CSP =
  "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: { port: 5174, proxy: { '/api': 'http://127.0.0.1:8090' } },
  preview: { proxy: { '/api': 'http://127.0.0.1:8090' }, headers: { 'Content-Security-Policy': CSP } },
  test: { environment: 'node', include: ['src/**/*.test.ts'] },
})

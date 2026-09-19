import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { rmSync } from 'node:fs'
import { resolve } from 'node:path'

// Секреты и служебные файлы прокси не должны попасть в сборку, которая заливается по FTP:
// боевой config.php лежит на хостинге отдельно.
function stripProxyPrivateFiles() {
  let outDir
  return {
    name: 'kupol-strip-proxy-private-files',
    apply: 'build',
    configResolved(config) {
      outDir = resolve(config.root, config.build.outDir)
    },
    closeBundle() {
      for (const f of ['api/config.php', 'api/config.example.php']) {
        rmSync(resolve(outDir, f), { force: true })
      }
    },
  }
}

export default defineConfig(({ mode }) => {
  // Порты берём из корневого .env (общий с Makefile и scripts/dev.sh).
  const env = loadEnv(mode, resolve(import.meta.dirname, '..'), 'KUPOL_')
  const proxyPort = env.KUPOL_PROXY_PORT || '8081'
  return {
    plugins: [vue(), stripProxyPrivateFiles()],
    server: {
      host: '127.0.0.1',
      port: Number(env.KUPOL_FRONT_PORT || 5173),
      strictPort: true,
      // dev идёт тем же путём, что прод: Vite -> PHP-прокси (php -S) -> Go API.
      proxy: { '/api': { target: `http://127.0.0.1:${proxyPort}`, changeOrigin: false } },
    },
    build: { target: 'es2022', sourcemap: false },
    test: {
      environment: 'jsdom',
      include: ['tests/**/*.test.js'],
    },
  }
})

// Node-обёртка для хостингов, которые умеют запускать только Node.js-приложения.
// Запускает собранный Go-бинарник kupol (`kupol serve`) и слушает порт из PORT.
//
//   PORT              порт, который выдал хостинг (обязателен на хостинге; по умолчанию 8080)
//   HOST              адрес для прослушивания (по умолчанию 0.0.0.0)
//   KUPOL_BIN         путь к бинарнику (по умолчанию ./kupol рядом с этим файлом)
//   .env рядом с server.js подхватывается, если есть (переменные окружения важнее).
//   Остальные KUPOL_* передаются бинарнику как есть.
'use strict';

const { spawn } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const dir = __dirname;

// Простейший загрузчик .env (KEY=VALUE, # комментарии, кавычки).
const envFile = path.join(dir, '.env');
if (fs.existsSync(envFile)) {
  for (const line of fs.readFileSync(envFile, 'utf8').split(/\r?\n/)) {
    const m = line.match(/^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/);
    if (!m || line.trim().startsWith('#')) continue;
    let val = m[2];
    if (/^(['"]).*\1$/.test(val)) val = val.slice(1, -1);
    if (process.env[m[1]] === undefined) process.env[m[1]] = val;
  }
}

const port = process.env.PORT || '8080';
const host = process.env.HOST || '0.0.0.0';
const bin = path.resolve(dir, process.env.KUPOL_BIN || 'kupol');

if (!fs.existsSync(bin)) {
  console.error(`[kupol-node] не найден бинарник ${bin} — соберите его (make build-back) и положите рядом`);
  process.exit(1);
}
try { fs.chmodSync(bin, 0o755); } catch { /* нет прав — пусть spawn сам скажет */ }

const env = { ...process.env, KUPOL_HTTP_ADDR: `${host}:${port}` };

let child = null;
let stopping = false;
let restarts = 0;
let lastStart = 0;

function start() {
  lastStart = Date.now();
  console.log(`[kupol-node] запуск ${bin} serve на ${env.KUPOL_HTTP_ADDR}`);
  child = spawn(bin, ['serve'], { cwd: dir, env, stdio: 'inherit' });
  child.on('error', (err) => {
    console.error('[kupol-node] не удалось запустить бинарник:', err.message);
    process.exit(1);
  });
  child.on('exit', (code, signal) => {
    child = null;
    if (stopping) process.exit(code ?? 0);
    console.error(`[kupol-node] kupol завершился (code=${code}, signal=${signal})`);
    // Если прожил больше минуты — считаем, что это разовый сбой, и сбрасываем счётчик.
    if (Date.now() - lastStart > 60_000) restarts = 0;
    if (++restarts > 5) {
      console.error('[kupol-node] слишком много падений подряд — выхожу');
      process.exit(code || 1);
    }
    const delay = Math.min(1000 * 2 ** (restarts - 1), 30_000);
    console.error(`[kupol-node] перезапуск через ${delay} мс`);
    setTimeout(start, delay);
  });
}

function stop(sig) {
  stopping = true;
  if (!child) process.exit(0);
  child.kill(sig);
  // Go-сервер сам корректно завершается по SIGTERM; страховка на случай зависания.
  setTimeout(() => child && child.kill('SIGKILL'), 15_000).unref();
}

process.on('SIGTERM', () => stop('SIGTERM'));
process.on('SIGINT', () => stop('SIGINT'));

start();

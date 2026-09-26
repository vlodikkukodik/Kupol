#!/usr/bin/env bash
# Разработка одной командой: PostgreSQL (docker) + Go API + PHP-прокси (php -S) + Vite.
# Путь запроса в dev тот же, что на проде: браузер -> Vite -> PHP-прокси -> Go API -> PostgreSQL.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

[ -f .env ] || make env
set -a
. ./.env
set +a
export XDEBUG_MODE=off
# Origin сайта для защиты от CSRF: адрес Vite, если не задан явно
export KUPOL_SITE_ORIGIN="${KUPOL_SITE_ORIGIN:-http://127.0.0.1:${KUPOL_FRONT_PORT}}"

for bin in docker go php npm; do
  command -v "$bin" >/dev/null || { echo "нужен $bin"; exit 1; }
done
[ -d frontend/node_modules ] || (cd frontend && npm install)

PIDS=()
cleanup() {
  trap - EXIT INT TERM
  echo
  echo "Останавливаю..."
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "== PostgreSQL"
docker compose up -d --wait postgres

echo "== Go API"
(cd backend && go build -o bin/kupol ./cmd/kupol)
backend/bin/kupol serve &
PIDS+=($!)

echo "== PHP-прокси на :${KUPOL_PROXY_PORT}"
php -d display_errors=0 -S "127.0.0.1:${KUPOL_PROXY_PORT}" frontend/dev/proxy-router.php >/dev/null 2>&1 &
PIDS+=($!)

echo "== Vite на :${KUPOL_FRONT_PORT}"
(cd frontend && exec npm run dev) &
PIDS+=($!)

echo
echo "Сайт:  http://127.0.0.1:${KUPOL_FRONT_PORT}"
echo "API:   http://${KUPOL_HTTP_ADDR}/health"
echo "Остановить: Ctrl+C"
wait

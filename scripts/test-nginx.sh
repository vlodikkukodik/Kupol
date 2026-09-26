#!/usr/bin/env bash
# Проверка deploy/server/nginx-kupol-api.conf настоящим nginx (docker, образ nginx:stable):
#   PHP-прокси -> nginx -> Go API -> PostgreSQL, и прямые запросы браузера (WebSocket/загрузки) через nginx.
# Проверяет то, что ломается молча: сохранность сырого URI (иначе не сходится подпись PHP-прокси),
# защиту от подделки X-Forwarded-For, лимиты тела, формат ошибок.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[ -f .env ] || { echo "нет .env — выполните make env"; exit 1; }
set -a; . ./.env; set +a
export XDEBUG_MODE=off

for bin in curl jq php go docker; do command -v "$bin" >/dev/null || { echo "нужен $bin"; exit 1; }; done
docker compose up -d --wait postgres >/dev/null

TMP="$(mktemp -d)"
DB="kupol_nginx_$(openssl rand -hex 4)"
CONTAINER="kupol-nginx-test-$$"
PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  wait 2>/dev/null || true
  docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$TMP" 2>/dev/null || true
}
trap cleanup EXIT

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }
API_PORT="$(free_port)"; NGINX_PORT="$(free_port)"; PROXY_PORT="$(free_port)"

FAILS=0
expect() { if [ "$2" = "$3" ]; then echo "  ok   $1"; else echo "  FAIL $1: получено '$2', ожидалось '$3'"; FAILS=$((FAILS+1)); fi; }

docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc \
  "CREATE DATABASE $DB ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0"
(cd backend && go build -o "$TMP/kupol" ./cmd/kupol)

# Go настроен так же, как в deploy/server/kupol.env.example: nginx на этой же машине — доверенный.
KUPOL_DATABASE_URL="postgres://$KUPOL_DB_USER:$KUPOL_DB_PASSWORD@127.0.0.1:$KUPOL_DB_PORT/$DB?sslmode=disable" \
KUPOL_HTTP_ADDR="127.0.0.1:$API_PORT" KUPOL_TRUSTED_PROXIES=127.0.0.1 KUPOL_LOG_LEVEL=warn \
  "$TMP/kupol" serve 2>"$TMP/api.log" &
PIDS+=($!)
for _ in $(seq 1 100); do curl -fsS -o /dev/null "http://127.0.0.1:$API_PORT/health" 2>/dev/null && break; sleep 0.1; done

# Боевой конфиг nginx с подставленными портами (меняем только listen и адрес Go).
sed -e "s#listen 80;#listen $NGINX_PORT;#" \
    -e "/listen \[::\]:80;/d" \
    -e "s#127.0.0.1:8081#127.0.0.1:$API_PORT#" \
    deploy/server/nginx-kupol-api.conf >"$TMP/kupol-api.conf"
chmod a+r "$TMP/kupol-api.conf"

echo "== nginx -t (синтаксис боевого конфига)"
docker run --rm -v "$TMP/kupol-api.conf:/etc/nginx/conf.d/default.conf:ro" nginx:stable nginx -t 2>&1 | sed 's/^/    /'
docker run -d --rm --name "$CONTAINER" --network host \
  -v "$TMP/kupol-api.conf:/etc/nginx/conf.d/default.conf:ro" nginx:stable >/dev/null
for _ in $(seq 1 100); do curl -fsS -o /dev/null "http://127.0.0.1:$NGINX_PORT/health" 2>/dev/null && break; sleep 0.1; done
curl -fsS -o /dev/null "http://127.0.0.1:$NGINX_PORT/health" || { echo "nginx не поднялся"; docker logs "$CONTAINER" 2>&1 | tail; exit 1; }

# PHP-прокси смотрит на nginx (loopback -> http допустим).
KUPOL_PROXY_UPSTREAM="http://127.0.0.1:$NGINX_PORT" \
  php -d display_errors=0 -S "127.0.0.1:$PROXY_PORT" frontend/dev/proxy-router.php >/dev/null 2>&1 &
PIDS+=($!)
for _ in $(seq 1 100); do curl -fsS -o /dev/null "http://127.0.0.1:$PROXY_PORT/api/health" 2>/dev/null && break; sleep 0.1; done

NG="http://127.0.0.1:$NGINX_PORT"
PX="http://127.0.0.1:$PROXY_PORT"

echo
echo "== подпись PHP-прокси переживает nginx (сырой URI не переписывается)"
R="$(curl -sS -w '\n%{http_code}' "$PX/api/health")"
expect "GET /api/health через PHP -> nginx -> Go" "$(tail -n1 <<<"$R")" 200
expect "ip_source" "$(sed '$d' <<<"$R" | jq -r .ip_source)" proxy
for uri in '/api/health?x=%D0%B0&y=a%2Fb' '/api/health?q=a+b&r=%20' '/api/health?e=%3D%26'; do
  expect "спецсимволы в query: $uri" "$(curl -sS -o /dev/null -w '%{http_code}' "$PX$uri")" 200
done
# Двойной слеш: nginx (merge_slashes) не должен переписывать проксируемый URI.
# Если бы переписал — подпись не сошлась бы и Go ответил бы 401 вместо 404.
expect "двойной слеш в пути не ломает подпись (404 от Go, не 401)" "$(curl -sS -o /dev/null -w '%{http_code}' "$PX/api//nope")" 404
expect "%2F в пути не ломает подпись (404 от Go, не 401)" "$(curl -sS -o /dev/null -w '%{http_code}' "$PX/api/a%2Fb")" 404
expect "POST через nginx" "$(curl -sS -o /dev/null -w '%{http_code}' -X POST -d '{}' "$PX/api/health")" 405

echo
echo "== прямые запросы через nginx (как WebSocket и загрузки из браузера)"
R="$(curl -sS "$NG/health")"
expect "ip_source direct" "$(jq -r .ip_source <<<"$R")" direct
expect "client_ip — адрес клиента, а не nginx" "$(jq -r .client_ip <<<"$R")" 127.0.0.1
# Клиент присылает свой X-Forwarded-For — nginx обязан его перезаписать, иначе IP подделывается
# (обход лимитов по IP). curl приходит с 127.0.0.1, значит и Go должен видеть 127.0.0.1.
R="$(curl -sS -H 'X-Forwarded-For: 6.6.6.6' "$NG/health")"
expect "подделка X-Forwarded-For не проходит" "$(jq -r .client_ip <<<"$R")" 127.0.0.1
R="$(curl -sS -H 'X-Forwarded-For: 6.6.6.6, 7.7.7.7' -H 'X-Real-IP: 8.8.8.8' "$NG/health")"
expect "цепочка X-Forwarded-For и X-Real-IP тоже не проходят" "$(jq -r .client_ip <<<"$R")" 127.0.0.1
# Поддельная подпись напрямую через nginx — 401, и заголовки подписи не подменяют IP
R="$(curl -sS -o /dev/null -w '%{http_code}' -H 'X-Kupol-Client-Ip: 6.6.6.6' -H "X-Kupol-Timestamp: $(date +%s)" -H 'X-Kupol-Signature: 00' "$NG/health")"
expect "подделка подписи через nginx -> 401" "$R" 401

echo
echo "== лимиты и ошибки"
BIG="$TMP/big.bin"
head -c 2097152 /dev/zero >"$BIG"
R="$(curl -sS -w '\n%{http_code}' -X POST -H 'Content-Type: application/octet-stream' --data-binary "@$BIG" "$NG/api/health")"
expect "тело 2 МиБ: 413, а не 502" "$(tail -n1 <<<"$R")" 413
expect "413 в формате ошибок API" "$(sed '$d' <<<"$R" | jq -r .error.code)" payload_too_large
head -c 1048576 /dev/zero >"$TMP/edge.bin"
expect "тело ровно 1 МиБ доходит до Go (405, не 413)" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/octet-stream' --data-binary "@$TMP/edge.bin" "$NG/api/health")" 405
head -c 1048577 /dev/zero >"$TMP/over.bin"
expect "тело 1 МиБ + 1 байт: 413" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/octet-stream' --data-binary "@$TMP/over.bin" "$NG/api/health")" 413
R="$(curl -sS -w '\n%{http_code}' "$NG/api/nope")"
expect "404 в формате API" "$(sed '$d' <<<"$R" | jq -r .error.code)" not_found

echo
echo "== заголовки ответа"
curl -sS -D "$TMP/h" -o /dev/null "$NG/health"
expect "X-Request-Id проходит" "$(grep -ci '^x-request-id: [0-9a-f]\{16\}' "$TMP/h")" 1
expect "Cache-Control проходит" "$(grep -i '^cache-control:' "$TMP/h" | tr -d '\r' | awk '{print $2}')" no-store

echo
if [ "$FAILS" -ne 0 ]; then
  echo "test-nginx: упало $FAILS"
  echo "--- лог Go:"; cat "$TMP/api.log"
  exit 1
fi
echo "test-nginx: всё прошло"

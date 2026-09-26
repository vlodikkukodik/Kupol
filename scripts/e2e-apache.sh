#!/usr/bin/env bash
# Проверка боевой сборки на настоящем Apache + PHP 8.3 (образ php:8.3-apache — то же, что shared-хостинг):
# собранный dist + .htaccess + PHP-прокси + настоящий Go API + PostgreSQL.
# Затем прогоняет deploy/check-site.sh и браузерные тесты Playwright против этого Apache.
#
#   scripts/e2e-apache.sh            # всё
#   SKIP_BROWSER=1 scripts/e2e-apache.sh   # только check-site.sh
#   PLAYWRIGHT_ARGS="team.spec.js -g телефон" scripts/e2e-apache.sh   # только выбранные браузерные тесты
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[ -f .env ] || { echo "нет .env — выполните make env"; exit 1; }
set -a; . ./.env; set +a
export XDEBUG_MODE=off

for bin in curl jq go docker npm; do command -v "$bin" >/dev/null || { echo "нужен $bin"; exit 1; }; done
docker compose up -d --wait postgres >/dev/null

TMP="$(mktemp -d)"
DB="kupol_apache_$(openssl rand -hex 4)"
CONTAINER="kupol-apache-e2e-$$"
PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  wait 2>/dev/null || true
  docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1 || true
  # файлы, созданные в контейнере от root, могут не удаляться обычным rm
  rm -rf "$TMP" 2>/dev/null || true
}
trap cleanup EXIT

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }
API_PORT="$(free_port)"
WEB_PORT="$(free_port)"

echo "== сборка фронтенда"
(cd frontend && npm ci --silent && npm run build --silent)
cp -r frontend/dist "$TMP/www"
[ ! -e "$TMP/www/api/config.php" ] || { echo "config.php попал в dist!"; exit 1; }

echo "== Go API"
docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc \
  "CREATE DATABASE $DB ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0"
(cd backend && go build -o "$TMP/kupol" ./cmd/kupol)
KUPOL_DATABASE_URL="postgres://$KUPOL_DB_USER:$KUPOL_DB_PASSWORD@127.0.0.1:$KUPOL_DB_PORT/$DB?sslmode=disable" \
KUPOL_HTTP_ADDR="127.0.0.1:$API_PORT" KUPOL_LOG_LEVEL=warn \
KUPOL_SITE_ORIGIN="http://127.0.0.1:$WEB_PORT" \
  "$TMP/kupol" serve 2>"$TMP/api.log" &
PIDS+=($!)
for _ in $(seq 1 100); do curl -fsS -o /dev/null "http://127.0.0.1:$API_PORT/health" 2>/dev/null && break; sleep 0.1; done
curl -fsS -o /dev/null "http://127.0.0.1:$API_PORT/health" || { echo "Go API не поднялся"; cat "$TMP/api.log"; exit 1; }

# Боевой config.php кладётся на хостинг отдельно от сборки — так же и здесь.
# upstream по http допустим только для loopback (контейнер в host-сети видит 127.0.0.1 хоста).
cat >"$TMP/www/api/config.php" <<EOF
<?php
return [
    'upstream' => 'http://127.0.0.1:$API_PORT',
    'secret'   => '$KUPOL_PROXY_SECRET',
];
EOF
chmod -R a+rX "$TMP/www"

echo "== Apache + PHP 8.3 на :$WEB_PORT"
printf 'Listen %s\n' "$WEB_PORT" >"$TMP/ports.conf"
cat >"$TMP/vhost.conf" <<EOF
<VirtualHost *:$WEB_PORT>
  DocumentRoot /var/www/html
  <Directory /var/www/html>
    AllowOverride All
    Require all granted
  </Directory>
  ErrorLog /proc/self/fd/2
</VirtualHost>
EOF
docker run -d --rm --name "$CONTAINER" --network host \
  -v "$TMP/www:/var/www/html:ro" \
  -v "$TMP/ports.conf:/etc/apache2/ports.conf:ro" \
  -v "$TMP/vhost.conf:/etc/apache2/sites-enabled/000-default.conf:ro" \
  php:8.3-apache sh -c 'a2enmod rewrite headers deflate >/dev/null 2>&1 && echo "ServerName localhost" >/etc/apache2/conf-enabled/servername.conf && exec apache2-foreground' >/dev/null

for _ in $(seq 1 100); do curl -fsS -o /dev/null "http://127.0.0.1:$WEB_PORT/" 2>/dev/null && break; sleep 0.2; done
curl -fsS -o /dev/null "http://127.0.0.1:$WEB_PORT/" || { echo "Apache не поднялся"; docker logs "$CONTAINER" 2>&1 | tail -20; exit 1; }
docker exec "$CONTAINER" php -v | head -1

echo
STATUS=0
"$ROOT/deploy/check-site.sh" "http://127.0.0.1:$WEB_PORT" || STATUS=$?

if [ "${SKIP_BROWSER:-0}" != 1 ]; then
  echo
  echo "== браузерные тесты против Apache"
  # KUPOL_E2E_ALLOW_WRITES: здесь своя временная БД, поэтому сценарии с регистрацией пользователей разрешены.
  # KUPOL_DATABASE_URL — та же временная БД: тесты загружают документы и выдают уровни командой `kupol`
  # (в .env указана dev-БД, и без этой подстановки команды ушли бы не в ту базу, из которой читает сайт).
  (cd frontend && \
    KUPOL_E2E_BASE_URL="http://127.0.0.1:$WEB_PORT" KUPOL_E2E_ALLOW_WRITES=1 KUPOL_E2E_APACHE=1 \
    KUPOL_DATABASE_URL="postgres://$KUPOL_DB_USER:$KUPOL_DB_PASSWORD@127.0.0.1:$KUPOL_DB_PORT/$DB?sslmode=disable" \
    npx playwright test ${PLAYWRIGHT_ARGS:-}) || STATUS=$?
fi

exit "$STATUS"

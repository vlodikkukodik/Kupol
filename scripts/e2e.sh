#!/usr/bin/env bash
# Сквозная проверка цепочки: curl -> PHP-прокси -> Go API -> PostgreSQL.
# Поднимает ВСЁ настоящее: свежую БД, собранный бинарник Go и php -S с боевым index.php.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[ -f .env ] || { echo "нет .env — выполните make env"; exit 1; }
set -a; . ./.env; set +a
export XDEBUG_MODE=off

for bin in curl jq php go docker; do command -v "$bin" >/dev/null || { echo "нужен $bin"; exit 1; }; done
docker compose up -d --wait postgres >/dev/null

TMP="$(mktemp -d)"
DB="kupol_e2e_$(openssl rand -hex 4)"
PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
  docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc \
  "CREATE DATABASE $DB ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0"

echo "== сборка Go"
( cd backend && go build -o "$TMP/kupol" ./cmd/kupol )

API_PORT="$(free_port)"; PROXY_PORT="$(free_port)"; BAD_PROXY_PORT="$(free_port)"
export KUPOL_DATABASE_URL="postgres://$KUPOL_DB_USER:$KUPOL_DB_PASSWORD@127.0.0.1:$KUPOL_DB_PORT/$DB?sslmode=disable"
export KUPOL_HTTP_ADDR="127.0.0.1:$API_PORT"
export KUPOL_LOG_LEVEL=info
# Здесь проверяются лимиты по умолчанию (как в проде): смягчённые значения из .env сбрасываем.
unset KUPOL_LIMIT_REGISTER_PER_HOUR KUPOL_LIMIT_API_PER_MINUTE KUPOL_LIMIT_LOGIN_ATTEMPTS KUPOL_LIMIT_LOGIN_LOCK_ATTEMPTS
export KUPOL_SITE_ORIGIN="https://kupol.e2e.test"

"$TMP/kupol" serve 2>"$TMP/api.log" & PIDS+=($!)

start_proxy() { # port secret
  KUPOL_PROXY_UPSTREAM="http://127.0.0.1:$API_PORT" KUPOL_PROXY_SECRET="$2" \
    php -d display_errors=0 -S "127.0.0.1:$1" frontend/dev/proxy-router.php >/dev/null 2>&1 &
  PIDS+=($!)
}
start_proxy "$PROXY_PORT" "$KUPOL_PROXY_SECRET"
start_proxy "$BAD_PROXY_PORT" "$(openssl rand -hex 32)"

wait_for() {
  for _ in $(seq 1 100); do curl -fsS -o /dev/null "$1" 2>/dev/null && return 0; sleep 0.1; done
  echo "не дождался $1"; cat "$TMP/api.log"; exit 1
}
wait_for "http://127.0.0.1:$API_PORT/health"
wait_for "http://127.0.0.1:$PROXY_PORT/api/health"

FAILS=0
expect() { # описание, фактическое, ожидаемое
  if [ "$2" = "$3" ]; then echo "  ok   $1"; else echo "  FAIL $1: получено '$2', ожидалось '$3'"; FAILS=$((FAILS+1)); fi
}

echo "== через PHP-прокси"
R="$(curl -sS -w '\n%{http_code}' "http://127.0.0.1:$PROXY_PORT/api/health?probe=1")"
CODE="$(tail -n1 <<<"$R")"; BODY="$(sed '$d' <<<"$R")"
expect "GET /api/health -> 200" "$CODE" 200
expect "status"       "$(jq -r .status   <<<"$BODY")" ok
expect "db"           "$(jq -r .db       <<<"$BODY")" ok
expect "ip_source (подпись PHP принята Go)" "$(jq -r .ip_source <<<"$BODY")" proxy
expect "client_ip"    "$(jq -r .client_ip <<<"$BODY")" 127.0.0.1

echo "== подделка заголовков через прокси не влияет на IP"
BODY="$(curl -sS -H 'X-Kupol-Client-Ip: 6.6.6.6' -H 'X-Forwarded-For: 6.6.6.6' "http://127.0.0.1:$PROXY_PORT/api/health")"
expect "client_ip не подменён" "$(jq -r .client_ip <<<"$BODY")" 127.0.0.1
expect "ip_source" "$(jq -r .ip_source <<<"$BODY")" proxy

echo "== 404 и request id проходят сквозь прокси"
R="$(curl -sS -D "$TMP/h" -w '\n%{http_code}' "http://127.0.0.1:$PROXY_PORT/api/nope")"
expect "404" "$(tail -n1 <<<"$R")" 404
expect "error.code" "$(sed '$d' <<<"$R" | jq -r .error.code)" not_found
expect "message" "$(sed '$d' <<<"$R" | jq -r .error.message)" "Дело не найдено"
RID_HDR="$(grep -i '^x-request-id:' "$TMP/h" | tr -d '\r' | awk '{print $2}')"
expect "request_id в теле = X-Request-Id" "$(sed '$d' <<<"$R" | jq -r .error.request_id)" "$RID_HDR"

echo "== неверный секрет на прокси: Go отвергает подпись (громко, не молча)"
R="$(curl -sS -w '\n%{http_code}' "http://127.0.0.1:$BAD_PROXY_PORT/api/health")"
expect "401" "$(tail -n1 <<<"$R")" 401
expect "error.code" "$(sed '$d' <<<"$R" | jq -r .error.code)" bad_proxy_signature

echo "== напрямую на Go (как WebSocket/загрузки)"
BODY="$(curl -sS "http://127.0.0.1:$API_PORT/health")"
expect "ip_source" "$(jq -r .ip_source <<<"$BODY")" direct
expect "client_ip" "$(jq -r .client_ip <<<"$BODY")" 127.0.0.1
R="$(curl -sS -o /dev/null -w '%{http_code}' -H 'X-Kupol-Client-Ip: 6.6.6.6' -H "X-Kupol-Timestamp: $(date +%s)" -H 'X-Kupol-Signature: 00' "http://127.0.0.1:$API_PORT/health")"
expect "поддельная подпись напрямую -> 401" "$R" 401

echo "== аккаунты через PHP-прокси: куки в обе стороны, Origin, лимиты"
P="http://127.0.0.1:$PROXY_PORT"
ORIGIN="$KUPOL_SITE_ORIGIN"
JAR="$TMP/jar.txt"
H='Content-Type: application/json'

captcha_answer() { # текст вопроса -> ответ (как прочитал бы человек на главной)
  case "$1" in
    *"В каком году основан"*) echo 1974 ;;
    *"Какой гриф"*) echo "Форма КУПОЛ-1" ;;
    *"Как сокращённо"*) echo ЦАК ;;
    *"пропущенное слово"*) echo объектами ;;
    *"С какого слова начинается"*) echo Комитет ;;
    *"Сколько букв"*) echo 5 ;;
    *) echo "?" ;;
  esac
}

CAP="$(curl -sS "$P/api/auth/captcha")"
CAP_ID="$(jq -r .id <<<"$CAP")"; CAP_Q="$(jq -r .question <<<"$CAP")"
expect "анкета: id — uuid" "$(grep -cE '^[0-9a-f-]{36}$' <<<"$CAP_ID")" 1
REG_BODY="$(jq -n --arg id "$CAP_ID" --arg a "$(captcha_answer "$CAP_Q")" '{login:"E2E-Vlad",password:"секретный пароль",captcha_id:$id,captcha_answer:$a}')"

R="$(curl -sS -c "$JAR" -D "$TMP/reg.head" -w '\n%{http_code}' -H "Origin: $ORIGIN" -H "$H" -d "$REG_BODY" "$P/api/auth/register")"
expect "регистрация -> 201" "$(tail -n1 <<<"$R")" 201
BODY="$(sed '$d' <<<"$R")"
expect "логин в ответе" "$(jq -r .user.login <<<"$BODY")" E2E-Vlad
expect "звание" "$(jq -r .user.level_name <<<"$BODY")" Посетитель
expect "резервный код: формат" "$(jq -r .backup_code <<<"$BODY" | grep -cE '^KUPOL(-[A-HJ-NP-Z2-9]{4}){4}$')" 1
BACKUP_CODE="$(jq -r .backup_code <<<"$BODY")"
expect "Set-Cookie прошёл через PHP: HttpOnly" "$(grep -ci '^set-cookie: kupol_session=.*HttpOnly' "$TMP/reg.head")" 1
expect "Set-Cookie прошёл через PHP: SameSite=Lax" "$(grep -ci '^set-cookie: kupol_session=.*SameSite=Lax' "$TMP/reg.head")" 1
expect "в банке кук: сессия HttpOnly" "$(grep -c '^#HttpOnly_.*kupol_session' "$JAR")" 1

expect "кука доходит до Go через PHP: session -> пользователь" "$(curl -sS -b "$JAR" "$P/api/auth/session" | jq -r .user.login)" E2E-Vlad
expect "без куки — Гражданин (user: null)" "$(curl -sS "$P/api/auth/session" | jq -r '.user')" null
SESSION_IP="$(docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d "$DB" -Atc "SELECT host(ip) FROM sessions LIMIT 1")"
expect "в сессии IP клиента, подписанный PHP-прокси" "$SESSION_IP" 127.0.0.1

# повторное использование вопроса анкеты
R="$(curl -sS -w '\n%{http_code}' -H "Origin: $ORIGIN" -H "$H" -d "$(jq -n --arg id "$CAP_ID" --arg a "$(captcha_answer "$CAP_Q")" '{login:"E2E-Other",password:"секретный пароль",captcha_id:$id,captcha_answer:$a}')" "$P/api/auth/register")"
expect "погашенный вопрос анкеты -> 422" "$(tail -n1 <<<"$R")" 422
expect "код ошибки" "$(sed '$d' <<<"$R" | jq -r .error.code)" captcha_failed

# CSRF: Origin доходит до Go через PHP
R="$(curl -sS -b "$JAR" -w '\n%{http_code}' -X POST -H "Origin: https://evil.example" "$P/api/auth/logout")"
expect "чужой Origin -> 403" "$(tail -n1 <<<"$R")" 403
expect "код ошибки" "$(sed '$d' <<<"$R" | jq -r .error.code)" forbidden_origin
expect "без Origin, но с кукой -> 403" "$(curl -sS -b "$JAR" -o /dev/null -w '%{http_code}' -X POST "$P/api/auth/logout")" 403
expect "сессия жива после попыток подделки" "$(curl -sS -b "$JAR" "$P/api/auth/session" | jq -r .user.login)" E2E-Vlad

# выход
expect "выход -> 204" "$(curl -sS -b "$JAR" -c "$JAR" -o /dev/null -w '%{http_code}' -X POST -H "Origin: $ORIGIN" "$P/api/auth/logout")" 204
expect "после выхода — Гражданин" "$(curl -sS -b "$JAR" "$P/api/auth/session" | jq -r '.user')" null

# вход и лимит попыток; Retry-After проходит через PHP
login_code() { curl -sS -o /dev/null -w '%{http_code}' -H "Origin: $ORIGIN" -H "$H" -d "{\"login\":\"e2e-vlad\",\"password\":\"$1\"}" "$P/api/auth/login"; }
expect "вход (логин без учёта регистра) -> 200" "$(login_code 'секретный пароль')" 200
for i in 1 2 3 4 5; do expect "неверный пароль #$i -> 401" "$(login_code 'неверно')" 401; done
R="$(curl -sS -D "$TMP/lock.head" -w '\n%{http_code}' -H "Origin: $ORIGIN" -H "$H" -d '{"login":"e2e-vlad","password":"секретный пароль"}' "$P/api/auth/login")"
expect "6-я попытка (даже с верным паролем) -> 429" "$(tail -n1 <<<"$R")" 429
expect "код ошибки" "$(sed '$d' <<<"$R" | jq -r .error.code)" rate_limited
RA="$(grep -i '^retry-after:' "$TMP/lock.head" | tr -d '\r' | awk '{print $2}')"
expect "Retry-After прошёл через PHP (около 15 минут)" "$([ "${RA:-0}" -gt 800 ] && [ "${RA:-0}" -le 900 ] && echo yes || echo no)" yes

# восстановление по резервному коду (лимит входа относится к другому логину/IP-паре, поэтому — другой логин)
R="$(curl -sS -w '\n%{http_code}' -H "Origin: $ORIGIN" -H "$H" -d "$(jq -n --arg c "$BACKUP_CODE" '{login:"E2E-Vlad",backup_code:$c,new_password:"новый пароль 1"}')" "$P/api/auth/restore")"
expect "восстановление при активной блокировке -> 429 (те же счётчики, что у входа)" "$(tail -n1 <<<"$R")" 429

echo "== миграции применены при старте"
V="$(docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d "$DB" -Atc "SELECT max(version_id) FROM goose_db_version")"
expect "версия схемы = числу файлов миграций" "$V" "$(ls backend/internal/database/migrations/*.sql | wc -l | tr -d ' ')"

echo "== остановка Go по SIGTERM (graceful shutdown)"
kill -TERM "${PIDS[0]}"
EXIT_CODE=0
wait "${PIDS[0]}" || EXIT_CODE=$?
PIDS=("${PIDS[@]:1}")
expect "код выхода" "$EXIT_CODE" 0
expect "в логе 'получен сигнал остановки'" "$(grep -c 'получен сигнал остановки' "$TMP/api.log" || true)" 1
expect "в логе 'API остановлен'" "$(grep -c 'API остановлен' "$TMP/api.log" || true)" 1

echo
if [ "$FAILS" -ne 0 ]; then echo "e2e: упало $FAILS"; echo "--- лог API"; cat "$TMP/api.log"; exit 1; fi
echo "e2e: всё прошло"

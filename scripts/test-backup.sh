#!/usr/bin/env bash
# Проверка резервного копирования на настоящих компонентах: PostgreSQL (docker), age, pg_dump/pg_restore и
# FTP-сервер (pyftpdlib) вместо хостинга. Проверяет, что копия
#   - создаётся, шифруется и не оставляет открытого дампа;
#   - согласована с БД, даже когда во время копии в неё пишут;
#   - восстанавливается и сверяется с манифестом (kupol-restore-check.sh);
#   - ротируется на VPS и на «хостинге»;
#   - ловит порчу (повреждённый или подменённый файл, неверный ключ);
#   - не оставляет полкопии при сбое и не запускается вдвое.
#
#   scripts/test-backup.sh          # или: make test-backup
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[ -f .env ] || { echo "нет .env — выполните make env"; exit 1; }
set -a; . ./.env; set +a

for bin in age age-keygen pg_dump pg_restore psql lftp go docker python3 curl; do
  command -v "$bin" >/dev/null || { echo "нужен $bin"; exit 1; }
done
docker compose up -d --wait postgres >/dev/null

TMP="$(mktemp -d)"
DB="kupol_backup_$(openssl rand -hex 4)"
PIDS=()
FAILED=0
cleanup() {
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
  docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$TMP" 2>/dev/null || true
}
trap cleanup EXIT

ok()   { echo "  ok    $1"; }
fail() { echo "  FAIL  $1"; FAILED=$((FAILED + 1)); }
check() { if [ "$2" = "$3" ]; then ok "$1"; else fail "$1: получено '$2', ожидалось '$3'"; fi; }
truthy() { if eval "$2"; then ok "$1"; else fail "$1"; fi; }
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

ADMIN="postgres://$KUPOL_DB_USER:$KUPOL_DB_PASSWORD@127.0.0.1:$KUPOL_DB_PORT/postgres?sslmode=disable"
DBURL="postgres://$KUPOL_DB_USER:$KUPOL_DB_PASSWORD@127.0.0.1:$KUPOL_DB_PORT/$DB?sslmode=disable"
psql "$ADMIN" -X -q -c "CREATE DATABASE $DB ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0"

echo "== БД с настоящей схемой и данными"
( cd backend && go build -o "$TMP/kupol" ./cmd/kupol )
export KUPOL_DATABASE_URL="$DBURL" KUPOL_LOG_LEVEL=error
"$TMP/kupol" migrate up >/dev/null
psql "$DBURL" -X -q -c "INSERT INTO users (login, password_hash, backup_code_hash, created_at, password_changed_at)
  SELECT 'Пользователь' || g, 'h', 'b', now(), now() FROM generate_series(1, 25) g"
"$TMP/kupol" doc import frontend/e2e/fixtures/pack.json >/dev/null 2>&1
USERS="$(psql "$DBURL" -X -q -At -c 'SELECT count(*) FROM users')"
DOCS="$(psql "$DBURL" -X -q -At -c 'SELECT count(*) FROM documents')"
truthy "в БД есть данные ($USERS пользователей, $DOCS документов)" '[ "$USERS" -ge 25 ] && [ "$DOCS" -ge 5 ]'

echo "== ключи, «хостинг» и настройки"
age-keygen -o "$TMP/backup.key" >/dev/null 2>&1
PUB="$(age-keygen -y "$TMP/backup.key")"
age-keygen -o "$TMP/other.key" >/dev/null 2>&1
FTP_PORT="$(free_port)"
mkdir -p "$TMP/ftp"
python3 -m venv "$TMP/venv" >/dev/null 2>&1
"$TMP/venv/bin/pip" install -q pyftpdlib 2>&1 | grep -v notice || true
"$TMP/venv/bin/python" -m pyftpdlib -i 127.0.0.1 -p "$FTP_PORT" -d "$TMP/ftp" -u backup -P s3cret-pass -w >"$TMP/ftp.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 50); do (exec 3<>"/dev/tcp/127.0.0.1/$FTP_PORT") 2>/dev/null && break; sleep 0.1; done

PING_PORT="$(free_port)"
mkdir -p "$TMP/ping"
( cd "$TMP/ping" && python3 -m http.server "$PING_PORT" --bind 127.0.0.1 >"$TMP/ping.log" 2>&1 ) &
PIDS+=($!)
sleep 0.5

write_conf() { # write_conf [ключ=значение …] — переопределения
  cat >"$TMP/backup.env" <<EOF
KUPOL_DATABASE_URL=$DBURL
AGE_RECIPIENT=$PUB
BACKUP_DIR=$TMP/backups
KEEP_LOCAL=2
KEEP_REMOTE=2
FTP_HOST=127.0.0.1
FTP_PORT=$FTP_PORT
FTP_USER=backup
FTP_PASSWORD=s3cret-pass
FTP_BACKUP_DIR=backups
FTP_TLS=none
HEALTHCHECK_URL=http://127.0.0.1:$PING_PORT/ping
EOF
  for kv in "$@"; do echo "$kv" >>"$TMP/backup.env"; done
}
BACKUP=(env KUPOL_BACKUP_ENV="$TMP/backup.env" "$ROOT/deploy/backup/kupol-backup.sh")
RESTORE=("$ROOT/deploy/backup/kupol-restore-check.sh")
latest() { ls -1 "$TMP/backups"/kupol-*.dump.age 2>/dev/null | sort | tail -1; }
count_local() { ls -1 "$TMP/backups"/kupol-*.dump.age 2>/dev/null | wc -l | tr -d ' '; }
count_remote() { ls -1 "$TMP/ftp/backups"/kupol-*.dump.age 2>/dev/null | wc -l | tr -d ' '; }

write_conf
echo "== одна копия"
"${BACKUP[@]}" >"$TMP/run1.log" 2>&1 && ok "копия выполнена" || { fail "копия не выполнена"; cat "$TMP/run1.log"; }
F="$(latest)"
truthy "зашифрованный файл создан" '[ -s "$F" ]'
truthy "рядом лежат контрольная сумма и манифест" '[ -s "${F%.dump.age}.sha256" ] && [ -s "${F%.dump.age}.manifest" ]'
check "каталог копий доступен только владельцу" "$(stat -c %a "$TMP/backups")" 700
check "файл копии доступен только владельцу" "$(stat -c %a "$F")" 600
truthy "открытого дампа и рабочих каталогов не осталось" '[ -z "$(find "$TMP/backups" -name "*.dump" -o -name ".work.*" | head -1)" ]'
truthy "в шифрованной копии нет читаемых данных" '! grep -aq "Пользователь" "$F" && ! grep -aq "CREATE TABLE" "$F" && ! grep -aq "PGDMP" "$F"'
check "копия на «хостинге» — того же размера" "$(stat -c %s "$TMP/ftp/backups/$(basename "$F")")" "$(stat -c %s "$F")"
truthy "мониторингу отправлен пинг" 'grep -q "GET /ping" "$TMP/ping.log"'
truthy "приватного ключа нигде рядом с копией нет" '! grep -rlaq "AGE-SECRET-KEY" "$TMP/backups" "$TMP/ftp"'

echo "== восстановление и сверка"
if "${RESTORE[@]}" "$F" "$TMP/backup.key" "$ADMIN" >"$TMP/restore.log" 2>&1; then ok "копия восстановлена и сверена"; else fail "восстановление"; cat "$TMP/restore.log"; fi
truthy "сверка перечислила таблицы" 'grep -q "ok   users" "$TMP/restore.log" && grep -q "ok   documents" "$TMP/restore.log"'
truthy "временная БД удалена" '[ "$(psql "$ADMIN" -X -q -At -c "SELECT count(*) FROM pg_database WHERE datname LIKE '"'"'kupol_restore_check_%'"'"'")" = 0 ]'
# копия версионирована по схеме
truthy "версия схемы прочитана" 'grep -q "версия схемы: [0-9]" "$TMP/restore.log"'

echo "== копия согласована, даже когда в БД пишут"
( while true; do psql "$DBURL" -X -q -c "INSERT INTO captcha_challenges (question_id, expires_at) VALUES ('load', now() + interval '1 hour')" >/dev/null 2>&1 || true; done ) &
WRITER=$!
PIDS+=("$WRITER")
sleep 0.5
for i in 1 2 3; do
  KUPOL_BACKUP_STAMP="20990101-00000$i" "${BACKUP[@]}" >"$TMP/load$i.log" 2>&1 || { fail "копия под нагрузкой $i"; cat "$TMP/load$i.log"; }
  G="$TMP/backups/kupol-20990101-00000$i.dump.age"
  if "${RESTORE[@]}" "$G" "$TMP/backup.key" "$ADMIN" >"$TMP/loadr$i.log" 2>&1; then ok "копия $i, снятая во время записи, точно совпала с манифестом"; else fail "копия $i не совпала с манифестом"; cat "$TMP/loadr$i.log"; fi
done
kill "$WRITER" 2>/dev/null || true
truthy "во время копий запись действительно шла" '[ "$(psql "$DBURL" -X -q -At -c "SELECT count(*) FROM captcha_challenges")" -gt 3 ]'

echo "== ротация"
check "на VPS осталось KEEP_LOCAL=2" "$(count_local)" 2
check "на «хостинге» осталось KEEP_REMOTE=2" "$(count_remote)" 2
truthy "вместе со старой копией удалены её сумма и манифест" '[ "$(ls -1 "$TMP/backups" | grep -c "\.sha256$")" = 2 ] && [ "$(ls -1 "$TMP/backups" | grep -c "\.manifest$")" = 2 ]'
truthy "остались самые новые" '[ "$(basename "$(latest)")" = "kupol-20990101-000003.dump.age" ]'
check "и на «хостинге» — самая новая" "$(ls -1 "$TMP/ftp/backups"/kupol-*.dump.age | sort | tail -1 | xargs basename)" "kupol-20990101-000003.dump.age"

echo "== порча копии обнаруживается"
G="$(latest)"
if "${RESTORE[@]}" "$G" "$TMP/other.key" "$ADMIN" >"$TMP/r_wrongkey.log" 2>&1; then fail "чужой ключ принят"; else ok "чужой ключ отвергнут"; fi
truthy "сообщение про ключ понятное" 'grep -q "не расшифровывается" "$TMP/r_wrongkey.log"'

cp "$G" "$TMP/bad.dump.age"; cp "${G%.dump.age}.sha256" "$TMP/bad.sha256"; cp "${G%.dump.age}.manifest" "$TMP/bad.manifest"
mkdir -p "$TMP/tamper"; cp "$G" "$TMP/tamper/kupol-x.dump.age"; cp "${G%.dump.age}.manifest" "$TMP/tamper/kupol-x.manifest"
printf '\x00' | dd of="$TMP/tamper/kupol-x.dump.age" bs=1 seek=200 conv=notrunc 2>/dev/null
( cd "$TMP/tamper" && sed "s/kupol-[0-9-]*.dump.age/kupol-x.dump.age/" "${G%.dump.age}.sha256" >kupol-x.sha256 )
if "${RESTORE[@]}" "$TMP/tamper/kupol-x.dump.age" "$TMP/backup.key" "$ADMIN" >"$TMP/r_flip.log" 2>&1; then fail "изменённый байт не замечен"; else ok "изменённый байт замечен"; fi
truthy "обнаружено по контрольной сумме" 'grep -q "контрольная сумма" "$TMP/r_flip.log"'

head -c 3000 "$G" >"$TMP/tamper/kupol-y.dump.age"
if "${RESTORE[@]}" "$TMP/tamper/kupol-y.dump.age" "$TMP/backup.key" "$ADMIN" >"$TMP/r_trunc.log" 2>&1; then fail "обрезанный файл принят"; else ok "обрезанный файл отвергнут (без контрольной суммы — по расшифровке)"; fi

cp "$G" "$TMP/tamper/kupol-z.dump.age"; sed 's/^users .*/users 999/' "${G%.dump.age}.manifest" >"$TMP/tamper/kupol-z.manifest"
if "${RESTORE[@]}" "$TMP/tamper/kupol-z.dump.age" "$TMP/backup.key" "$ADMIN" >"$TMP/r_manifest.log" 2>&1; then fail "неверный манифест принят"; else ok "расхождение с манифестом обнаружено"; fi
truthy "названа таблица и оба числа" 'grep -q "users" "$TMP/r_manifest.log" && grep -q "999" "$TMP/r_manifest.log"'
truthy "после всех проверок временных БД не осталось" '[ "$(psql "$ADMIN" -X -q -At -c "SELECT count(*) FROM pg_database WHERE datname LIKE '"'"'kupol_restore_check_%'"'"'")" = 0 ]'

echo "== сбои не оставляют полкопии"
BEFORE="$(ls -1 "$TMP/backups" | sort | tr '\n' ' ')"
write_conf "KUPOL_DATABASE_URL=postgres://$KUPOL_DB_USER:wrong@127.0.0.1:$KUPOL_DB_PORT/$DB?sslmode=disable"
if "${BACKUP[@]}" >"$TMP/e1.log" 2>&1; then fail "копия при неверном пароле БД прошла"; else ok "недоступная БД — копия не создана, код выхода не 0"; fi
check "файлы в каталоге копий не менялись" "$(ls -1 "$TMP/backups" | sort | tr '\n' ' ')" "$BEFORE"
truthy "нет остатков рабочего каталога" '[ -z "$(ls -A "$TMP/backups" | grep "^\.work")" ]'

write_conf "AGE_RECIPIENT=не-ключ"
if "${BACKUP[@]}" >"$TMP/e2.log" 2>&1; then fail "копия с неверным ключом age прошла"; else ok "неверный публичный ключ — копия не создана"; fi
truthy "нет остатков открытого дампа" '[ -z "$(find "$TMP/backups" -name "*.dump" -o -name ".work.*" | head -1)" ]'

write_conf "FTP_PORT=$(free_port)"
KUPOL_BACKUP_STAMP=20990102-000001 "${BACKUP[@]}" >"$TMP/e3.log" 2>&1 && fail "недоступный хостинг не замечен" || ok "недоступный хостинг — код выхода не 0 (сработает тревога мониторинга)"
truthy "локальная копия при этом сохранилась" '[ -s "$TMP/backups/kupol-20990102-000001.dump.age" ]'
if "${RESTORE[@]}" "$TMP/backups/kupol-20990102-000001.dump.age" "$TMP/backup.key" "$ADMIN" >/dev/null 2>&1; then ok "и она восстановима"; else fail "локальная копия после сбоя хостинга не восстанавливается"; fi

echo "== одновременный запуск"
write_conf
exec 8>"$TMP/backups/.lock"
flock -n 8
set +e
"${BACKUP[@]}" >"$TMP/e4.log" 2>&1
CODE=$?
set -e
check "пока идёт другая копия, вторая не стартует (код 3)" "$CODE" 3
flock -u 8

echo
if [ "$FAILED" -eq 0 ]; then echo "Все проверки резервного копирования пройдены"; else echo "ПРОВАЛЕНО проверок: $FAILED"; exit 1; fi

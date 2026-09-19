#!/usr/bin/env bash
# Проверка резервной копии: расшифровать, восстановить во ВРЕМЕННУЮ БД и сверить с манифестом.
# Копия, которую ни разу не восстанавливали, — не копия. Запускать автору (приватный ключ age только у него)
# и раз в неделю; в CI та же проверка идёт на тестовых данных (scripts/test-backup.sh).
#
#   deploy/backup/kupol-restore-check.sh <kupol-….dump.age> <файл-с-приватным-ключом> [postgres://админ-подключение]
#
# Админ-подключение (создаёт и удаляет временную БД) берётся из третьего параметра или KUPOL_TEST_DATABASE_URL.
# Ничего, кроме временной БД, не меняется; открытый дамп удаляется в любом случае.
#
# Код выхода: 0 — копия цела и восстановима, иначе 1 (с объяснением) или 2 (неверные параметры).
set -euo pipefail
umask 077

FILE="${1:-}"; KEY="${2:-}"; ADMIN="${3:-${KUPOL_TEST_DATABASE_URL:-}}"
[ -f "$FILE" ] && [ -f "$KEY" ] && [ -n "$ADMIN" ] || { sed -n '2,12p' "$0" >&2; exit 2; }
for bin in age pg_restore psql sha256sum; do command -v "$bin" >/dev/null || { echo "нужен $bin" >&2; exit 2; }; done

BASE="${FILE%.dump.age}"
TMP="$(mktemp -d)"
DB="kupol_restore_check_$(head -c4 /dev/urandom | od -An -tx1 | tr -d ' \n')"
# подключение к временной БД: та же строка, другое имя БД
ADMIN_BASE="${ADMIN%%\?*}"; ADMIN_QUERY=""; [ "$ADMIN" = "$ADMIN_BASE" ] || ADMIN_QUERY="?${ADMIN#*\?}"
TARGET="${ADMIN_BASE%/*}/$DB$ADMIN_QUERY"
cleanup() {
  psql "$ADMIN" -X -q -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT
fail() { echo "ОШИБКА: $*" >&2; exit 1; }

echo "== целостность файла"
if [ -f "$BASE.sha256" ]; then
  ( cd "$(dirname "$FILE")" && sha256sum -c "$(basename "$BASE").sha256" >/dev/null 2>&1 ) || fail "контрольная сумма не совпала: файл повреждён"
  echo "  ok   sha256"
else
  echo "  нет $BASE.sha256 — пропускаю"
fi

echo "== расшифровка"
age -d -i "$KEY" -o "$TMP/dump" "$FILE" 2>"$TMP/age.err" || fail "не расшифровывается (неверный ключ или повреждённый файл): $(tr '\n' ' ' <"$TMP/age.err")"
echo "  ok"

echo "== восстановление во временную БД $DB"
psql "$ADMIN" -X -q -v ON_ERROR_STOP=1 -c "CREATE DATABASE $DB ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0" \
  || fail "не удалось создать временную БД (нужно право CREATEDB и PostgreSQL с ICU)"
pg_restore --no-owner --no-privileges --exit-on-error -d "$TARGET" "$TMP/dump" 2>"$TMP/restore.err" \
  || fail "pg_restore: $(head -c 400 "$TMP/restore.err" | tr '\n' ' ')"
echo "  ok"

echo "== сверка с манифестом"
if [ -f "$BASE.manifest" ]; then
  bad=0
  while read -r table want; do
    got="$(psql "$TARGET" -X -q -At -c "SELECT count(*) FROM \"$table\"" 2>/dev/null || echo "нет таблицы")"
    if [ "$got" = "$want" ]; then printf '  ok   %-22s %s\n' "$table" "$got"; else printf '  ОШИБКА %-20s в копии %s, в манифесте %s\n' "$table" "$got" "$want" >&2; bad=1; fi
  done <"$BASE.manifest"
  [ "$bad" -eq 0 ] || fail "число строк не совпало с манифестом"
else
  echo "  нет $BASE.manifest — сверка пропущена"
fi

echo "== схема"
VERSION="$(psql "$TARGET" -X -q -At -c "SELECT max(version_id) FROM goose_db_version" 2>/dev/null || true)"
[ -n "$VERSION" ] || fail "в копии нет таблицы версий схемы (goose_db_version)"
echo "  версия схемы: $VERSION"
echo "копия восстановима"

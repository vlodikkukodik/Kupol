#!/usr/bin/env bash
# Ночная резервная копия БД КУПОЛ (запускается на VPS таймером systemd — deploy/backup/kupol-backup.timer).
#
#   1. pg_dump в согласованном снимке БД (pg_dump --snapshot) + подсчёт строк в том же снимке — «манифест»;
#   2. проверка, что дамп читается (pg_restore --list) и в нём есть все таблицы;
#   3. шифрование age публичным ключом автора: приватного ключа на VPS нет, украденная копия бесполезна;
#   4. хранение последних KEEP_LOCAL копий на VPS;
#   5. копия на хостинг по FTPS (другая машина и другой провайдер), последние KEEP_REMOTE;
#   6. пинг мониторинга (Healthchecks.io и т.п.), если задан.
#
# Восстановление и проверка копии — deploy/backup/kupol-restore-check.sh (нужен приватный ключ, он у автора).
#
# Настройки — /etc/kupol/backup.env (образец: deploy/backup/backup.env.example). Каталог с копиями доступен только владельцу.
set -euo pipefail
umask 077

CONF="${KUPOL_BACKUP_ENV:-/etc/kupol/backup.env}"
[ -r "$CONF" ] || { echo "нет $CONF" >&2; exit 2; }
set -a
# shellcheck disable=SC1090
. "$CONF"
set +a
: "${KUPOL_DATABASE_URL:?не задан KUPOL_DATABASE_URL}" "${AGE_RECIPIENT:?не задан AGE_RECIPIENT (публичный ключ age1…)}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/kupol}"
KEEP_LOCAL="${KEEP_LOCAL:-14}"
KEEP_REMOTE="${KEEP_REMOTE:-14}"
FTP_TLS="${FTP_TLS:-explicit}"
FTP_VERIFY_CERT="${FTP_VERIFY_CERT:-yes}"
FTP_PORT="${FTP_PORT:-21}"
STAMP="${KUPOL_BACKUP_STAMP:-$(date -u +%Y%m%d-%H%M%S)}"

for bin in pg_dump pg_restore psql age flock; do command -v "$bin" >/dev/null || { echo "нужен $bin" >&2; exit 2; }; done
[ -z "${FTP_HOST:-}" ] || command -v lftp >/dev/null || { echo "для копии на хостинг нужен lftp" >&2; exit 2; }
mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

# Одна копия за раз: если предыдущая ещё идёт, эта не стартует.
exec 9>"$BACKUP_DIR/.lock"
flock -n 9 || { echo "уже выполняется другая копия" >&2; exit 3; }

TMP="$(mktemp -d "$BACKUP_DIR/.work.XXXXXX")"
PSQL_PID=""
cleanup() {
  [ -z "$PSQL_PID" ] || kill "$PSQL_PID" 2>/dev/null || true
  rm -rf "$TMP" # открытый (нешифрованный) дамп не остаётся ни при каком исходе
}
trap cleanup EXIT

NAME="kupol-$STAMP"
DUMP="$TMP/$NAME.dump"
MANIFEST="$TMP/$NAME.manifest"

# --- согласованный снимок: дамп и подсчёт строк видят одну и ту же БД, даже если сайт пишет в неё во время копии
coproc SNAP { psql "$KUPOL_DATABASE_URL" -X -q -At -v ON_ERROR_STOP=1 2>&1; }
PSQL_PID="$SNAP_PID"
echo "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;" >&"${SNAP[1]}"
echo "SELECT 'SNAPSHOT ' || pg_export_snapshot();" >&"${SNAP[1]}"
SNAPSHOT=""
while IFS= read -r -t 30 line <&"${SNAP[0]}"; do
  case "$line" in SNAPSHOT\ *) SNAPSHOT="${line#SNAPSHOT }"; break ;; esac
done
[ -n "$SNAPSHOT" ] || { echo "не удалось получить снимок БД" >&2; exit 1; }

echo "== дамп (снимок $SNAPSHOT)"
pg_dump --format=custom --compress=6 --no-owner --no-privileges --snapshot="$SNAPSHOT" \
  --file="$DUMP" "$KUPOL_DATABASE_URL"

# строки по таблицам — в том же снимке, что и дамп
echo "SELECT 'TABLES ' || coalesce(string_agg(format('%I', tablename), ' ' ORDER BY tablename), '') FROM pg_tables WHERE schemaname = 'public';" >&"${SNAP[1]}"
TABLES=""
while IFS= read -r -t 30 line <&"${SNAP[0]}"; do
  case "$line" in TABLES\ *) TABLES="${line#TABLES }"; break ;; esac
done
[ -n "$TABLES" ] || { echo "в БД нет таблиц — копировать нечего" >&2; exit 1; }
: >"$MANIFEST"
for t in $TABLES; do
  echo "SELECT 'COUNT $t ' || count(*) FROM \"$t\";" >&"${SNAP[1]}"
  while IFS= read -r -t 60 line <&"${SNAP[0]}"; do
    case "$line" in "COUNT $t "*) echo "${line#COUNT }" >>"$MANIFEST"; break ;; esac
  done
done
echo "COMMIT;" >&"${SNAP[1]}"
echo '\q' >&"${SNAP[1]}"
wait "$PSQL_PID" 2>/dev/null || true
PSQL_PID=""

# --- дамп читается и содержит все таблицы манифеста
pg_restore --list "$DUMP" >"$TMP/list.txt"
for t in $TABLES; do
  grep -qE "TABLE public $t( |$)" "$TMP/list.txt" || { echo "в дампе нет таблицы $t" >&2; exit 1; }
done
[ "$(stat -c %s "$DUMP")" -gt 1024 ] || { echo "дамп подозрительно мал" >&2; exit 1; }

# --- шифрование: в каталог копий попадает только зашифрованное
echo "== шифрование"
age -r "$AGE_RECIPIENT" -o "$TMP/$NAME.dump.age" "$DUMP"
rm -f "$DUMP"
chmod 600 "$TMP/$NAME.dump.age" # age создаёт файл с правами по своему усмотрению, не по umask
( cd "$TMP" && sha256sum "$NAME.dump.age" >"$NAME.sha256" )
mv "$TMP/$NAME.dump.age" "$TMP/$NAME.sha256" "$MANIFEST" "$BACKUP_DIR/"
SIZE="$(stat -c %s "$BACKUP_DIR/$NAME.dump.age")"
echo "готово: $BACKUP_DIR/$NAME.dump.age ($SIZE байт), таблиц: $(wc -l <"$BACKUP_DIR/$NAME.manifest")"

# --- на VPS хранится KEEP_LOCAL последних копий
prune_local() {
  local i=0 f
  # shellcheck disable=SC2012
  for f in $(ls -1 "$BACKUP_DIR"/kupol-*.dump.age 2>/dev/null | sort -r); do
    i=$((i + 1))
    if [ "$i" -gt "$KEEP_LOCAL" ]; then
      rm -f "$f" "${f%.dump.age}.sha256" "${f%.dump.age}.manifest"
      echo "удалена старая копия: $(basename "$f")"
    fi
  done
}
prune_local

# --- копия на другую машину
if [ -n "${FTP_HOST:-}" ]; then
  : "${FTP_USER:?не задан FTP_USER}" "${FTP_PASSWORD:?не задан FTP_PASSWORD}" "${FTP_BACKUP_DIR:?не задан FTP_BACKUP_DIR}"
  case "$FTP_TLS" in
    explicit) TLS="set ftp:ssl-allow yes; set ftp:ssl-force yes; set ftp:ssl-protect-data yes; set ssl:verify-certificate $FTP_VERIFY_CERT" ;;
    none) TLS="set ftp:ssl-allow no" ;;
    *) echo "FTP_TLS: explicit или none" >&2; exit 2 ;;
  esac
  export LFTP_PASSWORD="$FTP_PASSWORD"
  ftp() {
    lftp -c "set net:max-retries 3; set net:timeout 30; set net:reconnect-interval-base 5; set cmd:fail-exit yes; $TLS; open --env-password -u '$FTP_USER' -p $FTP_PORT '$FTP_HOST'; $1; bye"
  }
  echo "== копия на $FTP_HOST:$FTP_BACKUP_DIR"
  # каталог создаётся, только если его ещё нет (mkdir на существующий даёт ошибку 550)
  ftp "cd '$FTP_BACKUP_DIR'" >/dev/null 2>&1 || ftp "mkdir -p '$FTP_BACKUP_DIR'"
  ftp "cd '$FTP_BACKUP_DIR'; put -O . '$BACKUP_DIR/$NAME.dump.age'; put -O . '$BACKUP_DIR/$NAME.sha256'; put -O . '$BACKUP_DIR/$NAME.manifest'"
  # передача целая: размер на хостинге равен размеру файла. Формат «ls -l» у серверов разный (есть ли группа),
  # поэтому размер — число, за которым идёт название месяца.
  REMOTE_SIZE="$(ftp "cd '$FTP_BACKUP_DIR'; cls -l '$NAME.dump.age'" \
    | awk '{ for (i = 1; i < NF; i++) if ($(i + 1) ~ /^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)$/) { print $i; exit } }')"
  [ "$REMOTE_SIZE" = "$SIZE" ] || { echo "на хостинге размер $REMOTE_SIZE, ожидалось $SIZE" >&2; exit 1; }
  REMOTE_LIST="$(ftp "cd '$FTP_BACKUP_DIR'; cls -1 kupol-*.dump.age" | sort -r)"
  i=0
  for f in $REMOTE_LIST; do
    i=$((i + 1))
    if [ "$i" -gt "$KEEP_REMOTE" ]; then
      base="${f%.dump.age}"
      ftp "cd '$FTP_BACKUP_DIR'; rm -f '$f' '$base.sha256' '$base.manifest'" || true
      echo "на хостинге удалена старая копия: $f"
    fi
  done
fi

[ -z "${HEALTHCHECK_URL:-}" ] || curl -fsS -m 15 --retry 3 -o /dev/null "$HEALTHCHECK_URL" || echo "не удалось отправить пинг мониторингу" >&2
echo "копия $NAME завершена"

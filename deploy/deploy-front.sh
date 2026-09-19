#!/usr/bin/env bash
# Сборка фронтенда и заливка на shared-хостинг по FTP.
#
#   deploy/deploy-front.sh                       сборка + заливка + проверка сайта
#   deploy/deploy-front.sh --skip-build          залить уже собранный frontend/dist
#   deploy/deploy-front.sh --dry-run             показать, что изменится (в т.ч. что будет удалено), ничего не меняя
#   deploy/deploy-front.sh --wipe                разрешить удалить посторонние файлы в каталоге сайта
#                                                (нужно один раз, если там лежит чужое; см. ниже)
#   deploy/deploy-front.sh --upload-config FILE  залить боевой api/config.php (один раз; при смене секрета)
#
# Защита от потери чужих данных: если в каталоге сайта есть файлы, а сборки КУПОЛ там ещё нет
# (нет маркера .kupol-site), скрипт покажет список и остановится. Осознанно удалить всё лишнее — с --wipe.
# После первого деплоя каталог «наш», и --wipe больше не нужен.
#
# Настройки — в deploy/.env.deploy (в git не попадает; образец: deploy/env.deploy.example).
# Боевой api/config.php заливается ТОЛЬКО через --upload-config и обычным деплоем не затрагивается и не удаляется.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="${KUPOL_DEPLOY_ENV:-$ROOT/deploy/.env.deploy}"
DIST="$ROOT/frontend/dist"

die() { echo "ошибка: $*" >&2; exit 1; }

SKIP_BUILD=0
DRY_RUN=0
WIPE=0
UPLOAD_CONFIG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --skip-build) SKIP_BUILD=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --wipe) WIPE=1 ;;
    --upload-config) shift; UPLOAD_CONFIG="${1:-}"; [ -n "$UPLOAD_CONFIG" ] || die "--upload-config требует путь к файлу" ;;
    -h|--help) sed -n '2,17p' "$0"; exit 0 ;;
    *) die "неизвестный параметр: $1" ;;
  esac
  shift
done

command -v lftp >/dev/null || die "нужен lftp (apt install lftp / brew install lftp)"
[ -f "$CONF" ] || die "нет $CONF — скопируйте deploy/env.deploy.example и заполните"

set -a
# shellcheck disable=SC1090
. "$CONF"
set +a
: "${FTP_HOST:?не задан FTP_HOST}" "${FTP_USER:?не задан FTP_USER}" "${FTP_PASSWORD:?не задан FTP_PASSWORD}" "${FTP_REMOTE_DIR:?не задан FTP_REMOTE_DIR}"
FTP_PORT="${FTP_PORT:-21}"
FTP_TLS="${FTP_TLS:-explicit}"           # explicit | none
FTP_VERIFY_CERT="${FTP_VERIFY_CERT:-yes}" # no — только если у хостинга самоподписанный сертификат
SITE_URL="${SITE_URL:-https://kupol.vladinc.ru}"

case "$FTP_TLS" in
  explicit) TLS_SETTINGS="set ftp:ssl-allow yes; set ftp:ssl-force yes; set ftp:ssl-protect-data yes; set ssl:verify-certificate $FTP_VERIFY_CERT" ;;
  none)     TLS_SETTINGS="set ftp:ssl-allow no"; echo "ВНИМАНИЕ: FTP без шифрования — пароль и файлы идут открытым текстом" >&2 ;;
  *) die "FTP_TLS: explicit или none" ;;
esac

# Пароль передаём через окружение (open --env-password), а не в аргументах:
# иначе он виден в списке процессов.
export LFTP_PASSWORD="$FTP_PASSWORD"
lftp_run() {
  lftp -c "set net:max-retries 3; set net:timeout 20; set net:reconnect-interval-base 3; set cmd:fail-exit yes; $TLS_SETTINGS; open --env-password -u '$FTP_USER' -p $FTP_PORT '$FTP_HOST'; $1; bye"
}

if [ -n "$UPLOAD_CONFIG" ]; then
  [ -f "$UPLOAD_CONFIG" ] || die "нет файла $UPLOAD_CONFIG"
  grep -q "ЗАМЕНИТЬ_НА_СЕКРЕТ" "$UPLOAD_CONFIG" && die "в $UPLOAD_CONFIG остался ЗАМЕНИТЬ_НА_СЕКРЕТ"
  if command -v php >/dev/null && ! XDEBUG_MODE=off php -l "$UPLOAD_CONFIG" >/dev/null 2>&1; then
    die "$UPLOAD_CONFIG: синтаксическая ошибка PHP"
  fi
  echo "== заливаю боевой api/config.php"
  # Каталог api/ создаём, только если его ещё нет (mkdir на существующий даёт ошибку 550).
  # Строка «550 No such file or directory» при самой первой заливке — это нормально.
  lftp_run "cd '$FTP_REMOTE_DIR'; (cls api >/dev/null) || mkdir api; put -O api '$UPLOAD_CONFIG' -o config.php; chmod 600 api/config.php"
  echo "готово. Проверьте: deploy/check-site.sh $SITE_URL"
  exit 0
fi

if [ "$SKIP_BUILD" -eq 0 ]; then
  echo "== сборка"
  (cd "$ROOT/frontend" && npm ci --silent && npm run build --silent)
fi
[ -f "$DIST/index.html" ] || die "нет $DIST/index.html — сначала соберите (без --skip-build)"
[ ! -e "$DIST/api/config.php" ] || die "в сборке оказался api/config.php — это секрет, заливать нельзя"

MIRROR_OPTS="-R --no-perms --parallel=4 --exclude-glob api/config.php"

# --- что уже лежит на хостинге ---
# Любая ошибка связи/входа/TLS должна останавливать деплой, а не выглядеть как «каталог пуст»:
# иначе защита от удаления чужих файлов молча отключилась бы.
ERR_FILE="$(mktemp)"
trap 'rm -f "$ERR_FILE"' EXIT
# (pwd для проверки не годится: lftp подключается лениво, pwd соединения не требует; cls читает каталог)
lftp_run "cls -1 -a ." >/dev/null 2>"$ERR_FILE" || die "не удалось подключиться к FTP ($FTP_HOST:$FTP_PORT): $(tr '\n' ' ' <"$ERR_FILE")"

REMOTE_LISTING=""
OURS=0
if lftp_run "cd '$FTP_REMOTE_DIR'" >/dev/null 2>&1; then
  # Каталог есть — список обязан прочитаться. Без служебных . и .. и без api/ (там может быть только config.php).
  RAW_LISTING="$(lftp_run "cd '$FTP_REMOTE_DIR' && cls -1 -a" 2>"$ERR_FILE")" \
    || die "не удалось прочитать содержимое $FTP_REMOTE_DIR: $(tr '\n' ' ' <"$ERR_FILE")"
  REMOTE_LISTING="$(printf '%s\n' "$RAW_LISTING" | sed 's#/$##' | grep -Ev '^(\.|\.\.|api|)$' || true)"
  # Маркер «каталог наш» — файл, который кладёт только этот скрипт (frontend/public/.kupol-site).
  # api/index.php для этого не годится: он есть и у чужих приложений.
  if printf '%s\n' "$RAW_LISTING" | grep -qx '\.kupol-site'; then OURS=1; fi
fi
if [ "$OURS" -eq 0 ] && [ -n "$REMOTE_LISTING" ]; then
  echo "В каталоге сайта ($FTP_REMOTE_DIR) уже есть файлы, а сборки КУПОЛ там ещё нет:" >&2
  echo "$REMOTE_LISTING" | head -50 | sed 's/^/    /' >&2
  [ "$(echo "$REMOTE_LISTING" | wc -l)" -gt 50 ] && echo "    ... (показаны первые 50)" >&2
  if [ "$WIPE" -eq 0 ] && [ "$DRY_RUN" -eq 0 ]; then
    die "всё перечисленное будет УДАЛЕНО. Посмотреть план: --dry-run --wipe; удалить и залить: --wipe"
  fi
  echo "Это будет удалено (--wipe)." >&2
fi

if [ "$DRY_RUN" -eq 1 ]; then
  echo "== dry-run: план заливки и удаления в $FTP_HOST:$FTP_REMOTE_DIR (ничего не меняется)"
  lftp_run "mirror $MIRROR_OPTS --delete --dry-run '$DIST' '$FTP_REMOTE_DIR'"
  exit 0
fi

# Порядок важен: сначала всё, кроме index.html (новые хэшированные файлы), потом index.html,
# и только затем удаление устаревших файлов — сайт не «ломается» на время заливки.
echo "== заливаю на $FTP_HOST:$FTP_REMOTE_DIR"
lftp_run "mirror $MIRROR_OPTS --exclude-glob index.html '$DIST' '$FTP_REMOTE_DIR'"
lftp_run "cd '$FTP_REMOTE_DIR'; put '$DIST/index.html' -o index.html"
echo "== удаляю устаревшие файлы"
lftp_run "mirror $MIRROR_OPTS --delete '$DIST' '$FTP_REMOTE_DIR'"

echo "== проверка сайта"
"$ROOT/deploy/check-site.sh" "$SITE_URL"

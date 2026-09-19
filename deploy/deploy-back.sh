#!/usr/bin/env bash
# Сборка Go API, доставка на VPS и перезапуск сервиса. При неудачном старте — автоматический откат.
#
#   deploy/deploy-back.sh              сборка + доставка + перезапуск + проверка
#   deploy/deploy-back.sh --skip-build использовать уже собранный backend/bin/kupol
#   deploy/deploy-back.sh --rollback   вернуть предыдущую версию бинарника
#
# Настройки — в deploy/.env.deploy (образец: deploy/env.deploy.example).
# Схема БД мигрируется самим API при старте (kupol serve применяет ожидающие миграции).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONF="${KUPOL_DEPLOY_ENV:-$ROOT/deploy/.env.deploy}"
BIN="${KUPOL_BIN:-$ROOT/backend/bin/kupol}"

die() { echo "ошибка: $*" >&2; exit 1; }

SKIP_BUILD=0
ROLLBACK=0
while [ $# -gt 0 ]; do
  case "$1" in
    --skip-build) SKIP_BUILD=1 ;;
    --rollback) ROLLBACK=1 ;;
    -h|--help) sed -n '2,9p' "$0"; exit 0 ;;
    *) die "неизвестный параметр: $1" ;;
  esac
  shift
done

for bin in ssh scp; do command -v "$bin" >/dev/null || die "нужен $bin"; done
[ -f "$CONF" ] || die "нет $CONF — скопируйте deploy/env.deploy.example и заполните"
set -a
# shellcheck disable=SC1090
. "$CONF"
set +a
: "${VPS_HOST:?не задан VPS_HOST}" "${VPS_USER:?не задан VPS_USER}" "${VPS_DIR:?не задан VPS_DIR}"
VPS_PORT="${VPS_PORT:-22}"
VPS_RESTART_CMD="${VPS_RESTART_CMD:-sudo systemctl restart kupol}"
VPS_HEALTH_URL="${VPS_HEALTH_URL:-http://127.0.0.1:8081/health}"
API_URL="${API_URL:-}"
VPS_HEALTH_WAIT="${VPS_HEALTH_WAIT:-30}" # сколько секунд ждать старта API после перезапуска

SSH_OPTS=(-p "$VPS_PORT" -o BatchMode=yes -o ConnectTimeout=15 ${VPS_SSH_OPTS:-})
SCP_OPTS=(-P "$VPS_PORT" -o BatchMode=yes -o ConnectTimeout=15 ${VPS_SSH_OPTS:-})
remote() { ssh "${SSH_OPTS[@]}" "$VPS_USER@$VPS_HOST" "$@"; }

# Ждёт, пока API на VPS ответит status=ok (до VPS_HEALTH_WAIT секунд). Возвращает 0/1.
wait_healthy() {
  remote "for i in \$(seq 1 $((VPS_HEALTH_WAIT * 2))); do
            if curl -fsS --max-time 3 '$VPS_HEALTH_URL' 2>/dev/null | grep -q '\"status\":\"ok\"'; then exit 0; fi
            sleep 0.5
          done
          exit 1"
}

restore_previous() {
  echo "== откат на предыдущую версию" >&2
  remote "set -e; cd '$VPS_DIR'; [ -f kupol.prev ] || { echo 'нет kupol.prev — откатываться некуда' >&2; exit 1; }
          mv -f kupol kupol.failed; cp -p kupol.prev kupol" || return 1
  remote "$VPS_RESTART_CMD" || return 1
  wait_healthy
}

if [ "$ROLLBACK" -eq 1 ]; then
  restore_previous && echo "откат выполнен, API отвечает" || die "откат не удался — смотрите journalctl -u kupol на VPS"
  exit 0
fi

if [ "$SKIP_BUILD" -eq 0 ]; then
  echo "== сборка Go (linux/amd64)"
  (cd "$ROOT" && make build-back)
fi
[ -x "$BIN" ] || die "нет $BIN — соберите (make build-back)"
VERSION="$("$BIN" version 2>/dev/null || true)"
echo "== версия: ${VERSION:-неизвестна}"

echo "== доставляю на $VPS_USER@$VPS_HOST:$VPS_DIR"
remote true || die "не удалось подключиться по SSH к $VPS_USER@$VPS_HOST:$VPS_PORT (ключ, порт, файрвол?)"
remote "test -d '$VPS_DIR'" || die "на VPS нет каталога $VPS_DIR — см. docs/deploy.md, «Подготовка VPS»"
scp "${SCP_OPTS[@]}" "$BIN" "$VPS_USER@$VPS_HOST:$VPS_DIR/kupol.new"

echo "== подменяю бинарник и перезапускаю"
# Предыдущая версия сохраняется как kupol.prev — для отката.
remote "set -e; cd '$VPS_DIR'; chmod 755 kupol.new
        if [ -f kupol ]; then cp -p kupol kupol.prev; fi
        mv -f kupol.new kupol"
remote "$VPS_RESTART_CMD"

if wait_healthy; then
  echo "== API на VPS отвечает"
else
  echo "API не поднялся за ${VPS_HEALTH_WAIT} с. Журнал:" >&2
  remote "journalctl -u kupol -n 30 --no-pager 2>/dev/null || true" >&2
  if restore_previous; then
    die "новая версия не стартовала; выполнен откат на предыдущую, API отвечает"
  fi
  die "новая версия не стартовала, и откат не удался — срочно смотрите journalctl -u kupol на VPS"
fi

if [ -n "$API_URL" ] && command -v curl >/dev/null; then
  echo "== публичная проверка $API_URL/health"
  curl -fsS --max-time 15 "$API_URL/health" && echo || die "публичный адрес $API_URL/health не отвечает (nginx/DNS/TLS?)"
fi
echo "готово"

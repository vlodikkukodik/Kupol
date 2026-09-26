#!/bin/bash
# Подготовка сервера под установку приложений «в один клик» (WordPress). Идемпотентна, от root.
# Нужны среды выполнения (prepare-runtime.sh) и базы данных (prepare-db.sh). Здесь только каталог для скачанных архивов: панель хранит
# в нём проверенные дистрибутивы (по sha256) и не скачивает их заново для каждого сайта.
set -euo pipefail

ENVF=/etc/vladhost/env
[ -f "$ENVF" ] || { echo "нет $ENVF: сначала deploy/prepare.sh" >&2; exit 1; }
grep -q '^VLADHOST_RUNTIME_DIR=' "$ENVF" || echo "предупреждение: нет VLADHOST_RUNTIME_DIR — запустите deploy/prepare-runtime.sh" >&2
grep -q '^VLADHOST_DB_MARIADB_ADMIN_DSN=' "$ENVF" || echo "предупреждение: нет VLADHOST_DB_MARIADB_ADMIN_DSN — запустите deploy/prepare-db.sh" >&2

install -d -o vladhost -g vladhost -m 0755 /data/vladhost/cache /data/vladhost/cache/cms
grep -q '^VLADHOST_CMS_CACHE_DIR=' "$ENVF" || echo 'VLADHOST_CMS_CACHE_DIR=/data/vladhost/cache/cms' >>"$ENVF"
grep -q '^VLADHOST_GATEWAY_URL=' "$ENVF" || echo 'VLADHOST_GATEWAY_URL=http://127.0.0.1:8091' >>"$ENVF"
chown root:vladhost "$ENVF"
chmod 0640 "$ENVF"

# Панель должна дойти до сайта-источника: проверяем адрес дистрибутива с этого сервера.
if curl -fsSI --max-time 15 https://downloads.wordpress.org/release/wordpress-7.1.2.zip >/dev/null; then
    echo "готово: кэш /data/vladhost/cache/cms, дистрибутив WordPress доступен"
else
    echo "готово, но downloads.wordpress.org с этого сервера недоступен: установка не сможет скачать WordPress" >&2
fi

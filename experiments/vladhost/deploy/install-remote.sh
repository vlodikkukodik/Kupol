#!/bin/bash
# Установка на VPS от root. Запускается из deploy.sh; лежит в распакованном /tmp/vh-up/deploy.
# Порядок важен: сначала всё раскладываем, потом проверяем nginx, и только потом перезагружаем.
set -euo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
NGX_AVAIL=/etc/nginx/sites-available
NGX_ENABLED=/etc/nginx/sites-enabled

# --- бинарь и фронтенд: подмена одним rename, панель не остаётся в полусобранном состоянии ---
install -m 0755 "$SRC/bin/vladhost" /opt/vladhost/bin/vladhost.new
mv -f /opt/vladhost/bin/vladhost.new /opt/vladhost/bin/vladhost

rm -rf /opt/vladhost/frontend.new /opt/vladhost/frontend.old
cp -r "$SRC/frontend/dist" /opt/vladhost/frontend.new
chmod -R a+rX /opt/vladhost/frontend.new
[ -d /opt/vladhost/frontend ] && mv /opt/vladhost/frontend /opt/vladhost/frontend.old
mv /opt/vladhost/frontend.new /opt/vladhost/frontend
rm -rf /opt/vladhost/frontend.old

# --- выпускатель сертификатов и systemd ---
install -m 0755 "$SRC/deploy/bin/certs.sh" "$SRC/deploy/bin/cert-hook.sh" /usr/local/lib/vladhost/
install -m 0644 "$SRC"/deploy/systemd/* /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now vladhost-certs.path >/dev/null 2>&1
systemctl enable vladhost.service >/dev/null 2>&1
systemctl restart vladhost.service

# --- nginx: чужие конфиги не трогаем, добавляем только свои ---
added=()
enable_conf() { # имя
    install -m 0644 "$SRC/deploy/nginx/$1" "$NGX_AVAIL/$1"
    if [ ! -e "$NGX_ENABLED/$1" ]; then
        ln -s "$NGX_AVAIL/$1" "$NGX_ENABLED/$1"
        added+=("$NGX_ENABLED/$1")
    fi
}
enable_conf vladhost-http.conf
# https-часть включаем только когда есть сертификат панели, иначе nginx -t не пройдёт.
if [ -f /etc/letsencrypt/live/app.vladinc.ru/fullchain.pem ]; then
    enable_conf vladhost-https.conf
else
    echo "ВНИМАНИЕ: сертификата app.vladinc.ru ещё нет — https-часть nginx не включена"
fi

if nginx -t 2>/tmp/vh-nginx-test.log; then
    systemctl reload nginx
else
    echo "nginx -t не прошёл, откатываю свои изменения:" >&2
    cat /tmp/vh-nginx-test.log >&2
    for l in "${added[@]}"; do rm -f "$l"; done
    exit 1
fi

# --- проверка ---
for _ in $(seq 1 20); do
    curl -fsS http://127.0.0.1:8090/api/healthz >/dev/null 2>&1 && { echo "vladhost: работает"; exit 0; }
    sleep 1
done
echo "vladhost не ответил на /api/healthz:" >&2
journalctl -u vladhost.service -n 30 --no-pager >&2
exit 1

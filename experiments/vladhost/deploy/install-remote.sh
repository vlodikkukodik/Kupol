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
install -d -m 0755 /etc/nginx/snippets /etc/nginx/vladhost-domains
install -m 0644 "$SRC/deploy/nginx/vladhost-site-proxy.conf" /etc/nginx/snippets/vladhost-site-proxy.conf
install -m 0755 "$SRC/deploy/bin/certs.sh" "$SRC/deploy/bin/cert-hook.sh" "$SRC/deploy/bin/cert-info.sh" "$SRC/deploy/bin/cron-run.sh" "$SRC/deploy/bin/runtime.sh" /usr/local/lib/vladhost/
install -m 0644 "$SRC"/deploy/systemd/* /etc/systemd/system/
systemctl daemon-reload
# Сведения о ранее выпущенных сертификатах (срок, издатель) для панели: дальше их обновляет хук certbot.
install -d -m 0755 /var/lib/vladhost/certs/info
/usr/local/lib/vladhost/cert-info.sh --all || true
systemctl enable --now vladhost-certs.path >/dev/null 2>&1
# Команды планировщика: путь включается, только если сервер подготовлен (prepare-cron.sh создал очередь).
[ -d /var/lib/vladhost/cron/queue ] && systemctl enable --now vladhost-cron.path >/dev/null 2>&1
# Посредник оболочек (SSH и веб-терминал): перезапускается с новым бинарём, только если сервер подготовлен prepare-ssh.sh.
if [ -f /etc/systemd/system/vladhost-shell.service ]; then
    systemctl restart vladhost-shell.service
fi
# Среды выполнения (PHP, Node.js, Python): то же — только если сервер подготовлен prepare-runtime.sh.
if [ -d /var/lib/vladhost/runtime/queue ]; then
    systemctl enable --now vladhost-runtime.path vladhost-runtime-perms.timer >/dev/null 2>&1
fi
systemctl enable vladhost.service vladhost-web.service >/dev/null 2>&1
systemctl restart vladhost.service

# --- веб-шлюз сайтов: поднимаем и проверяем до перезагрузки nginx, иначе сайты на миг получат 502 ---
install -d -m 0755 /opt/vladhost/pages
install -m 0644 "$SRC/deploy/pages/__vh_down.html" /opt/vladhost/pages/__vh_down.html
systemctl restart vladhost-web.service
gw_ok=0
for _ in $(seq 1 20); do
    [ "$(curl -s -o /dev/null -w '%{http_code}' -H 'Host: probe.probe.vladinc.ru' http://127.0.0.1:8091/)" = 404 ] && { gw_ok=1; break; }
    sleep 1
done
if [ "$gw_ok" != 1 ]; then
    echo "веб-шлюз не ответил на проверку:" >&2
    journalctl -u vladhost-web.service -n 30 --no-pager >&2
    exit 1
fi

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
enable_conf vladhost-domains.conf
# https-часть включаем только когда есть сертификат панели, иначе nginx -t не пройдёт.
if [ -f /etc/letsencrypt/live/app.vladinc.ru/fullchain.pem ]; then
    enable_conf vladhost-https.conf
else
    echo "ВНИМАНИЕ: сертификата app.vladinc.ru ещё нет — https-часть nginx не включена"
fi

# Веб-клиент баз данных: только если сервер уже подготовлен (prepare-db.sh поставил Adminer и выпустил сертификат).
if [ -d /opt/vladadminer ]; then
    install -m 0644 -o root -g root "$SRC/deploy/adminer/index.php" /opt/vladadminer/index.php
    [ -f /etc/letsencrypt/live/db.vladinc.ru/fullchain.pem ] && enable_conf vladhost-db.conf
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

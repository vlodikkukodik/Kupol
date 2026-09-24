#!/bin/bash
# Первичная подготовка VPS (Ubuntu 24.04), от root. Идемпотентна: повторный запуск ничего не ломает.
# Не трогает чужие службы: только swap, пользователь vladhost, свои каталоги и своя БД.
set -euo pipefail

# --- swap 4 ГБ: на 4 ГБ RAM с соседними проектами без него OOM-killer рано или поздно убьёт чужой процесс ---
if ! swapon --show --noheadings | grep -q .; then
    fallocate -l 4G /swapfile
    chmod 600 /swapfile
    mkswap /swapfile >/dev/null
    swapon /swapfile
    grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >>/etc/fstab
fi
echo 'vm.swappiness=10' >/etc/sysctl.d/99-vladhost-swap.conf
sysctl -q -p /etc/sysctl.d/99-vladhost-swap.conf

# --- пользователь и каталоги ---
id vladhost >/dev/null 2>&1 || useradd --system --home-dir /opt/vladhost --shell /usr/sbin/nologin vladhost
# Веб-шлюз сайтов работает под отдельным пользователем: только чтение каталога сайтов, без доступа к секретам панели.
id vladhost-web >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin vladhost-web

install -d -m 0755 /opt/vladhost /opt/vladhost/bin /opt/vladhost/frontend /opt/vladhost/pages /usr/local/lib/vladhost /var/www/vladhost-acme
install -d -o vladhost -g vladhost -m 0755 /data/vladhost /data/vladhost/sites
# Привязки своих доменов ({домен} → адрес сайта): пишет панель, читает веб-шлюз.
install -d -o vladhost -g vladhost -m 0755 /data/vladhost/domains
install -d -m 0755 /etc/nginx/snippets /etc/nginx/vladhost-domains
# Обмен с выпускателем сертификатов: queue пишет панель, status пишет только root (защита от подмены симлинком).
install -d -m 0755 /var/lib/vladhost /var/lib/vladhost/certs /var/lib/vladhost/certs/status
install -d -o vladhost -g vladhost -m 0755 /var/lib/vladhost/certs/queue
# /etc/vladhost открыт на вход (nginx идёт в certs), секреты защищены правами самого файла env.
install -d -m 0755 /etc/vladhost
install -d -o root -g www-data -m 0750 /etc/vladhost/certs

# --- БД и секреты (только при первом запуске; пароль нигде не печатается) ---
if [ ! -f /etc/vladhost/env ]; then
    pgpw=$(openssl rand -hex 24)
    jwt=$(openssl rand -hex 32)
    if ! sudo -u postgres psql -Atc "select 1 from pg_roles where rolname='vladhost'" | grep -q 1; then
        sudo -u postgres psql -qc "create role vladhost login password '$pgpw'"
    else
        sudo -u postgres psql -qc "alter role vladhost password '$pgpw'"
    fi
    sudo -u postgres psql -Atc "select 1 from pg_database where datname='vladhost'" | grep -q 1 ||
        sudo -u postgres psql -qc "create database vladhost owner vladhost encoding 'UTF8' template template0 lc_collate 'C.UTF-8' lc_ctype 'C.UTF-8'"
    umask 027
    cat >/etc/vladhost/env <<EOF
VLADHOST_ADDR=127.0.0.1:8090
VLADHOST_DATABASE_URL=postgres://vladhost:${pgpw}@127.0.0.1:5432/vladhost?sslmode=disable
VLADHOST_JWT_SECRET=${jwt}
VLADHOST_COOKIE_SECURE=true
VLADHOST_PANEL_ORIGIN=https://app.vladinc.ru
VLADHOST_SITES_ROOT=/data/vladhost/sites
VLADHOST_BASE_DOMAIN=vladinc.ru
VLADHOST_CERTS_DIR=/var/lib/vladhost/certs
EOF
    chown root:vladhost /etc/vladhost/env
    chmod 0640 /etc/vladhost/env
fi

# --- FTP (добавлено в фазе 5): настройки дописываются к существующему env, если их ещё нет ---
install -d -o root -g vladhost -m 0750 /etc/vladhost/ftp
grep -q '^VLADHOST_FTP_ADDR=' /etc/vladhost/env || cat >>/etc/vladhost/env <<'EOF'
VLADHOST_FTP_ADDR=:2121
VLADHOST_FTP_HOST=ftp.vladinc.ru
VLADHOST_FTP_PUBLIC_IP=37.153.70.33
VLADHOST_FTP_PASSIVE_PORTS=50000-50100
VLADHOST_FTP_CERT=/etc/vladhost/ftp/fullchain.pem
VLADHOST_FTP_KEY=/etc/vladhost/ftp/privkey.pem
EOF

# --- свои домены (добавлено позже): IP сервера для проверки DNS и для выпускателя сертификатов ---
server_ip=$(curl -fsS --max-time 10 https://api.ipify.org 2>/dev/null || true)
[ -n "${VLADHOST_SERVER_IP:-}" ] && server_ip=$VLADHOST_SERVER_IP
if [ -n "$server_ip" ]; then
    [ -f /etc/vladhost/certs.conf ] || echo "SERVER_IPS=\"$server_ip\"" >/etc/vladhost/certs.conf
    grep -q '^VLADHOST_SERVER_IPS=' /etc/vladhost/env || echo "VLADHOST_SERVER_IPS=$server_ip" >>/etc/vladhost/env
fi
grep -q '^VLADHOST_DOMAINS_DIR=' /etc/vladhost/env || echo 'VLADHOST_DOMAINS_DIR=/data/vladhost/domains' >>/etc/vladhost/env

echo "готово: swap=$(swapon --show --noheadings | awk '{print $3}' | tr '\n' ' ') env=$(stat -c '%a %U:%G' /etc/vladhost/env)"

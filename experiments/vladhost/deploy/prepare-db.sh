#!/bin/bash
# Подготовка VPS под пользовательские базы данных (Ubuntu 24.04), от root. Идемпотентна: повторный запуск ничего не ломает
# и не меняет уже выданные пароли. Запускается из распакованного deploy: bash deploy/prepare-db.sh
#
# Что ставится, всё отдельно от чужих проектов сервера (их PostgreSQL на 5432, Redis и остальное не трогаем):
#   * второй кластер PostgreSQL 16 «vladhost» на порту 5433 под своим пользователем vhdb;
#   * MariaDB на порту 3306 (соседи её не используют);
#   * Adminer на db.vladinc.ru (PHP-FPM с отдельным пулом и пользователем vhadminer);
#   * сертификат db.vladinc.ru: им пользуются и HTTPS веб-клиента, и TLS обеих СУБД (внешние подключения только по TLS).
# В /etc/vladhost/env дописываются VLADHOST_DB_*; панель подхватит их при следующей выкладке (deploy.sh перезапускает её).
set -euo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
DOMAIN=db.vladinc.ru
PGVER=16
CLUSTER=vladhost
PGPORT=5433
ADMINER_VERSION=4.17.1
ADMINER_SHA256=8cef8dcac2bb4598fb821859deb61c9b6889b4bdad00498a38d113954334bbb3
ADMINER_URL="https://github.com/vrana/adminer/releases/download/v${ADMINER_VERSION}/adminer-${ADMINER_VERSION}-en.php"
ENVF=/etc/vladhost/env

[ -f "$ENVF" ] || { echo "нет $ENVF: сначала deploy/prepare.sh" >&2; exit 1; }
rand() { openssl rand -hex "$1"; }
set_env() { # ключ значение — только если ключа ещё нет (пароли при повторном запуске не меняются)
    grep -q "^$1=" "$ENVF" || echo "$1=$2" >>"$ENVF"
}
get_env() { grep "^$1=" "$ENVF" | head -1 | cut -d= -f2- || true; }

# --- пакеты ---
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq mariadb-server php-fpm php-pgsql php-mysql php-curl >/dev/null
# Стандартный кластер main остаётся как был; MariaDB и php-fpm при установке стартуют сами.
phpv=$(ls /etc/php | sort -V | tail -1)

# --- пользователи ---
id vhdb >/dev/null 2>&1 || useradd --system --home-dir /var/lib/vhdb --create-home --shell /usr/sbin/nologin vhdb
usermod -aG postgres vhdb # сокет кластера кладётся в общий /var/run/postgresql
id vhadminer >/dev/null 2>&1 || useradd --system --home-dir /var/lib/vhadminer --create-home --shell /usr/sbin/nologin vhadminer

# --- nginx: порт 80 для выпуска сертификата ---
install -m 0644 "$SRC/deploy/nginx/vladhost-http.conf" /etc/nginx/sites-available/vladhost-http.conf
[ -e /etc/nginx/sites-enabled/vladhost-http.conf ] || ln -s /etc/nginx/sites-available/vladhost-http.conf /etc/nginx/sites-enabled/vladhost-http.conf
nginx -t
systemctl reload nginx

# --- сертификат db.vladinc.ru и его копии для СУБД (хук раскладывает их и при каждом продлении) ---
install -m 0755 "$SRC/deploy/bin/cert-hook.sh" /usr/local/lib/vladhost/cert-hook.sh
if [ ! -f /etc/letsencrypt/live/$DOMAIN/fullchain.pem ]; then
    certbot certonly --webroot -w /var/www/vladhost-acme -d $DOMAIN --cert-name $DOMAIN --non-interactive --agree-tos \
        --register-unsafely-without-email --deploy-hook /usr/local/lib/vladhost/cert-hook.sh
fi
RENEWED_LINEAGE=/etc/letsencrypt/live/$DOMAIN /usr/local/lib/vladhost/cert-hook.sh

# --- PostgreSQL: отдельный кластер ---
if ! pg_lsclusters -h | awk '{print $2}' | grep -qx $CLUSTER; then
    pg_createcluster $PGVER $CLUSTER -u vhdb -g vhdb -p $PGPORT --start-conf=auto -e UTF8 --locale=C.UTF-8 -- \
        --auth-local=peer --auth-host=scram-sha-256
fi
cdir=/etc/postgresql/$PGVER/$CLUSTER
install -d -o vhdb -g vhdb -m 0755 "$cdir/conf.d"
# Каталог правил доступа: пишет панель (владелец vladhost), читает сервер (группа vhdb); setgid — файлы наследуют группу.
install -d -o vladhost -g vhdb -m 2750 "$cdir/hba.d"
cat >"$cdir/conf.d/50-vladhost.conf" <<EOF
listen_addresses = '*'
unix_socket_directories = '/var/run/postgresql'
max_connections = 60
shared_buffers = 64MB
password_encryption = scram-sha-256
ssl = on
ssl_cert_file = '/etc/vladhost/db/pg/fullchain.pem'
ssl_key_file = '/etc/vladhost/db/pg/privkey.pem'
ssl_min_protocol_version = 'TLSv1.2'
EOF
chown vhdb:vhdb "$cdir/conf.d/50-vladhost.conf"
# Правила: своя служебная запись и вход с этого же сервера (Adminer) — по паролю; внешние адреса добавляет панель
# файлами в hba.d (hostssl, по одному IP на запись); всё остальное отклоняется.
cat >"$cdir/pg_hba.conf" <<EOF
local   all   vhdb                    peer
host    all   vhadmin  127.0.0.1/32   scram-sha-256
host    all   all      127.0.0.1/32   scram-sha-256
include_dir hba.d
host    all   all      all            reject
EOF
chown vhdb:vhdb "$cdir/pg_hba.conf"
install -d -m 0755 /etc/systemd/system/postgresql@$PGVER-$CLUSTER.service.d
printf '[Service]\nMemoryMax=512M\n' >/etc/systemd/system/postgresql@$PGVER-$CLUSTER.service.d/vladhost.conf
systemctl daemon-reload
pg_ctlcluster $PGVER $CLUSTER status >/dev/null 2>&1 || pg_ctlcluster $PGVER $CLUSTER start
systemctl enable postgresql@$PGVER-$CLUSTER >/dev/null 2>&1 || true
pg_ctlcluster $PGVER $CLUSTER restart

set_env VLADHOST_DB_PG_PASSWORD "$(rand 24)"
pgpw=$(get_env VLADHOST_DB_PG_PASSWORD)
sudo -u vhdb psql -p $PGPORT -d postgres -qv ON_ERROR_STOP=1 -v pw="$pgpw" <<'EOF'
select exists(select 1 from pg_roles where rolname = 'vhadmin') as have \gset
\if :have
  alter role vhadmin login superuser password :'pw';
\else
  create role vhadmin login superuser password :'pw';
\endif
EOF

# --- MariaDB ---
cat >/etc/mysql/mariadb.conf.d/60-vladhost.cnf <<EOF
[mysqld]
bind-address = 0.0.0.0
skip-name-resolve
max_connections = 60
innodb_buffer_pool_size = 128M
performance_schema = OFF
character-set-server = utf8mb4
collation-server = utf8mb4_general_ci
# Внешние подключения только по TLS (учётные записи с внешних адресов ещё и REQUIRE SSL)
ssl_cert = /etc/vladhost/db/maria/fullchain.pem
ssl_key = /etc/vladhost/db/maria/privkey.pem
tls_version = TLSv1.2,TLSv1.3
EOF
install -d -m 0755 /etc/systemd/system/mariadb.service.d
printf '[Service]\nMemoryMax=512M\n' >/etc/systemd/system/mariadb.service.d/vladhost.conf
systemctl daemon-reload
systemctl enable mariadb >/dev/null 2>&1
systemctl restart mariadb
set_env VLADHOST_DB_MARIADB_PASSWORD "$(rand 24)"
mypw=$(get_env VLADHOST_DB_MARIADB_PASSWORD)
# Служебная запись панели: вход только с этого сервера. GRANT OPTION нужен, чтобы выдавать права пользовательским базам.
mariadb <<EOF
create or replace user 'vhadmin'@'127.0.0.1' identified by '$mypw';
create or replace user 'vhadmin'@'localhost' identified by '$mypw';
grant all privileges on *.* to 'vhadmin'@'127.0.0.1' with grant option;
grant all privileges on *.* to 'vhadmin'@'localhost' with grant option;
flush privileges;
EOF

# --- Adminer ---
key=$(get_env VLADHOST_DB_INTERNAL_KEY)
if [ -z "$key" ]; then key=$(rand 24); set_env VLADHOST_DB_INTERNAL_KEY "$key"; fi
install -d -o root -g vhadminer -m 0750 /opt/vladadminer
install -d -o vhadminer -g vhadminer -m 0700 /var/lib/vhadminer/sessions
if [ ! -f /opt/vladadminer/adminer.php ] || ! echo "$ADMINER_SHA256  /opt/vladadminer/adminer.php" | sha256sum -c --quiet 2>/dev/null; then
    curl -fsSL --max-time 60 -o /tmp/adminer.php.new "$ADMINER_URL"
    echo "$ADMINER_SHA256  /tmp/adminer.php.new" | sha256sum -c --quiet || { echo "контрольная сумма Adminer не совпала" >&2; rm -f /tmp/adminer.php.new; exit 1; }
    install -m 0644 -o root -g root /tmp/adminer.php.new /opt/vladadminer/adminer.php
    rm -f /tmp/adminer.php.new
fi
install -m 0644 -o root -g root "$SRC/deploy/adminer/index.php" /opt/vladadminer/index.php
cat >/opt/vladadminer/config.php <<EOF
<?php
// Создан prepare-db.sh: секрет обмена токена с панелью и единственные серверы, к которым допускается Adminer.
return [
    'key' => '$key',
    'api' => 'http://127.0.0.1:8090/api/internal/db-session',
    'servers' => ['pgsql' => '127.0.0.1:$PGPORT', 'server' => 'localhost'],
];
EOF
chown root:vhadminer /opt/vladadminer/config.php
chmod 0640 /opt/vladadminer/config.php
chmod 0755 /opt/vladadminer
# nginx (www-data) должен пройти в каталог и достучаться до сокета, но читать config.php ему незачем: читает только php-fpm.

cat >/etc/php/$phpv/fpm/pool.d/vhadminer.conf <<EOF
[vhadminer]
user = vhadminer
group = vhadminer
listen = /run/php/vhadminer.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = ondemand
pm.max_children = 5
pm.process_idle_timeout = 30s
pm.max_requests = 200
security.limit_extensions = .php
php_admin_value[open_basedir] = /opt/vladadminer:/var/lib/vhadminer:/tmp
php_admin_value[session.save_path] = /var/lib/vhadminer/sessions
php_admin_value[upload_tmp_dir] = /tmp
php_admin_value[upload_max_filesize] = 64M
php_admin_value[post_max_size] = 64M
php_admin_value[memory_limit] = 256M
php_admin_value[max_execution_time] = 120
php_admin_value[display_errors] = 0
php_admin_value[expose_php] = 0
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec,mail
EOF
systemctl enable php$phpv-fpm >/dev/null 2>&1
systemctl reload php$phpv-fpm 2>/dev/null || systemctl restart php$phpv-fpm

# --- nginx: HTTPS веб-клиента ---
install -m 0644 "$SRC/deploy/nginx/vladhost-db.conf" /etc/nginx/sites-available/vladhost-db.conf
[ -e /etc/nginx/sites-enabled/vladhost-db.conf ] || ln -s /etc/nginx/sites-available/vladhost-db.conf /etc/nginx/sites-enabled/vladhost-db.conf
if nginx -t 2>/tmp/vh-nginx-db.log; then
    systemctl reload nginx
else
    cat /tmp/vh-nginx-db.log >&2
    rm -f /etc/nginx/sites-enabled/vladhost-db.conf
    exit 1
fi

# --- настройки панели (подхватит при следующей выкладке) ---
set_env VLADHOST_DB_PG_ADMIN_URL "postgres://vhadmin:$pgpw@127.0.0.1:$PGPORT/postgres?sslmode=disable"
set_env VLADHOST_DB_PG_HBA_DIR "$cdir/hba.d"
set_env VLADHOST_DB_MARIADB_ADMIN_DSN "vhadmin:$mypw@tcp(127.0.0.1:3306)/"
set_env VLADHOST_DB_MARIADB_EXTERNAL true
set_env VLADHOST_DB_HOST $DOMAIN
set_env VLADHOST_DB_WEB_URL "https://$DOMAIN"
chown root:vladhost "$ENVF"
chmod 0640 "$ENVF"

echo "готово: PostgreSQL $(pg_lsclusters -h | awk -v c=$CLUSTER '$2==c{print $1" порт "$3" "$4}'), MariaDB $(mariadb --version | awk '{print $5}' | tr -d ,), php $phpv"

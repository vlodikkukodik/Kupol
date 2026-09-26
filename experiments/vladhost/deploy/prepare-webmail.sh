#!/bin/bash
# Webmail Roundcube на webmail.vladinc.ru (Ubuntu 24.04), от root, после prepare-mail.sh. Идемпотентна.
#
#   * Roundcube 1.7.4 закреплённой версии (архив проверяется по sha256), каталог /opt/roundcube, документный корень public_html;
#   * отдельный пользователь vhwebmail и пул PHP-FPM с open_basedir; база SQLite и временные файлы — в /var/lib/vhwebmail;
#   * вход по адресу и паролю ящика, IMAP/SMTP/ManageSieve — через тот же почтовый сервер (mail.vladinc.ru) с проверкой сертификата;
#   * сертификат webmail.vladinc.ru (Let's Encrypt), nginx vladhost-webmail.conf; ночная чистка базы.
# Откат: $BACKUP/rollback-webmail.sh убирает виртуальный хост и пул (файлы и данные остаются).
set -euo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
DOMAIN=webmail.vladinc.ru
ENVF=/etc/vladhost/env
BACKUP=/root/vladhost-mail-backup
VERSION=1.7.4
SHA256=2c6c878f0093f1bf7fb6086781d2dd9269d652c016b86939c157c5f1729139a2
URL="https://github.com/roundcube/roundcubemail/releases/download/$VERSION/roundcubemail-$VERSION-complete.tar.gz"
PHPV=8.3
[ -f "$BACKUP/rollback.sh" ] || { echo "сначала deploy/prepare-mail.sh" >&2; exit 1; }
HOST=$(grep '^VLADHOST_MAIL_HOST=' "$ENVF" | cut -d= -f2-)
[ -n "$HOST" ] || { echo "в $ENVF нет VLADHOST_MAIL_HOST: сначала deploy/prepare-mail.sh" >&2; exit 1; }
ss -ltn | grep -q ':4190 ' || { echo "ManageSieve не слушает 4190: выполните заново deploy/prepare-mail.sh" >&2; exit 1; }

cat >"$BACKUP/rollback-webmail.sh" <<'EOF'
#!/bin/bash
# Отключает webmail: виртуальный хост и пул PHP-FPM убираются, файлы Roundcube и база остаются на месте.
rm -f /etc/nginx/sites-enabled/vladhost-webmail.conf /etc/php/8.3/fpm/pool.d/vhwebmail.conf /etc/cron.d/vladhost-webmail
nginx -t && systemctl reload nginx
systemctl reload php8.3-fpm 2>/dev/null || true
echo "webmail отключён"
EOF
chmod 0700 "$BACKUP/rollback-webmail.sh"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq || echo "ВНИМАНИЕ: apt-get update завершился с ошибкой: продолжаем с имеющимся кэшем пакетов" >&2
apt-get install -y -qq --no-install-recommends "php$PHPV-fpm" "php$PHPV-cli" "php$PHPV-mbstring" "php$PHPV-intl" "php$PHPV-xml" "php$PHPV-zip" "php$PHPV-gd" "php$PHPV-curl" "php$PHPV-sqlite3" >/dev/null

# --- пользователь и каталоги ---
id vhwebmail >/dev/null 2>&1 || useradd --system --home-dir /var/lib/vhwebmail --create-home --shell /usr/sbin/nologin vhwebmail
install -d -o vhwebmail -g vhwebmail -m 0750 /var/lib/vhwebmail /var/lib/vhwebmail/temp /var/lib/vhwebmail/sessions
install -d -o vhwebmail -g vhwebmail -m 0750 /var/log/vhwebmail

# --- Roundcube: закреплённая версия; при обновлении настройки и база сохраняются ---
if [ ! -f /opt/roundcube/.vh-version ] || [ "$(cat /opt/roundcube/.vh-version)" != "$VERSION" ]; then
    tmp=$(mktemp -d)
    curl -fsSL --max-time 180 -o "$tmp/rc.tgz" "$URL"
    echo "$SHA256  $tmp/rc.tgz" | sha256sum -c --quiet || { echo "контрольная сумма Roundcube не совпала" >&2; rm -rf "$tmp"; exit 1; }
    tar -xzf "$tmp/rc.tgz" -C "$tmp"
    new="$tmp/roundcubemail-$VERSION"
    [ -f "$new/public_html/index.php" ] || { echo "в архиве нет public_html/index.php" >&2; rm -rf "$tmp"; exit 1; }
    rm -rf "$new/installer" "$new/public_html/installer.php" # установщик не нужен: настройки ставит этот скрипт
    echo "$VERSION" >"$new/.vh-version"
    rm -rf /opt/roundcube.old
    [ ! -d /opt/roundcube ] || mv /opt/roundcube /opt/roundcube.old
    mv "$new" /opt/roundcube
    rm -rf "$tmp"
    UPGRADED=1
fi
chown -R root:root /opt/roundcube
chmod -R go-w /opt/roundcube
install -d -o root -g root -m 0755 /opt/roundcube /opt/roundcube/public_html

# --- настройки: ключ шифрования сессий создаётся один раз и хранится в самом файле ---
DES_KEY=""
[ -f /opt/roundcube/config/config.inc.php ] && DES_KEY=$(sed -n "s/^\$config\['des_key'\] = '\([A-Za-z0-9]\{24\}\)';.*/\1/p" /opt/roundcube/config/config.inc.php | head -1)
[ -n "$DES_KEY" ] || DES_KEY=$(openssl rand -hex 12)
sed -e "s|@MAIL_HOST@|$HOST|g" -e "s|@DES_KEY@|$DES_KEY|g" "$SRC/deploy/webmail/config.inc.php.tpl" >/opt/roundcube/config/config.inc.php
chown root:vhwebmail /opt/roundcube/config /opt/roundcube/config/config.inc.php
chmod 0750 /opt/roundcube/config
chmod 0640 /opt/roundcube/config/config.inc.php

# --- база: создаётся при первой установке, при обновлении версии — обновляется ---
db_ready() { runuser -u vhwebmail -- "php$PHPV" -r 'try { $r = (new PDO("sqlite:/var/lib/vhwebmail/roundcube.db"))->query("SELECT count(*) FROM sqlite_master WHERE name = \"users\""); exit((int) $r->fetchColumn() > 0 ? 0 : 1); } catch (Throwable $e) { exit(1); }'; }
if ! db_ready; then
    # initdb.sh может сообщить об ошибке уже после создания схемы: судим по тому, что таблицы действительно есть
    runuser -u vhwebmail -- "php$PHPV" /opt/roundcube/bin/initdb.sh --dir /opt/roundcube/SQL || true
    db_ready || { echo "база Roundcube не создана" >&2; exit 1; }
else
    runuser -u vhwebmail -- "php$PHPV" /opt/roundcube/bin/updatedb.sh --package=roundcube --dir /opt/roundcube/SQL || echo "ВНИМАНИЕ: updatedb.sh сообщил об ошибке (схема не менялась, если версия та же)" >&2
fi

# --- PHP-FPM: свой пул, работает только с каталогами Roundcube ---
cat >"/etc/php/$PHPV/fpm/pool.d/vhwebmail.conf" <<EOF
[vhwebmail]
user = vhwebmail
group = vhwebmail
listen = /run/php/vhwebmail.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
pm = ondemand
pm.max_children = 8
pm.process_idle_timeout = 30s
pm.max_requests = 300
security.limit_extensions = .php
php_admin_value[open_basedir] = /opt/roundcube:/var/lib/vhwebmail:/var/log/vhwebmail:/etc/ssl/certs:/usr/share/ca-certificates:/tmp
php_admin_value[session.save_path] = /var/lib/vhwebmail/sessions
php_admin_value[upload_tmp_dir] = /var/lib/vhwebmail/temp
php_admin_value[upload_max_filesize] = 26M
php_admin_value[post_max_size] = 28M
php_admin_value[memory_limit] = 192M
php_admin_value[expose_php] = Off
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen,pcntl_exec
php_admin_flag[session.cookie_secure] = on
php_admin_flag[session.cookie_httponly] = on
EOF
"php-fpm$PHPV" -t 2>/tmp/vh-webmail-fpm.log || { cat /tmp/vh-webmail-fpm.log >&2; rm -f "/etc/php/$PHPV/fpm/pool.d/vhwebmail.conf"; exit 1; }
systemctl enable "php$PHPV-fpm" >/dev/null 2>&1
systemctl reload "php$PHPV-fpm" 2>/dev/null || systemctl restart "php$PHPV-fpm"

# --- чистка базы ночью ---
cat >/etc/cron.d/vladhost-webmail <<EOF
# Roundcube: удаление устаревших записей (удалённые письма, кэш). Создан prepare-webmail.sh.
17 4 * * * vhwebmail php$PHPV /opt/roundcube/bin/cleandb.sh >/dev/null 2>&1
EOF
chmod 0644 /etc/cron.d/vladhost-webmail

# --- сертификат и nginx ---
install -m 0644 "$SRC/deploy/nginx/vladhost-http.conf" /etc/nginx/sites-available/vladhost-http.conf
[ -e /etc/nginx/sites-enabled/vladhost-http.conf ] || ln -s /etc/nginx/sites-available/vladhost-http.conf /etc/nginx/sites-enabled/vladhost-http.conf
nginx -t
systemctl reload nginx
if [ ! -f /etc/letsencrypt/live/$DOMAIN/fullchain.pem ]; then
    certbot certonly --webroot -w /var/www/vladhost-acme -d $DOMAIN --cert-name $DOMAIN --non-interactive --agree-tos \
        --register-unsafely-without-email --deploy-hook "systemctl reload nginx"
fi
install -m 0644 "$SRC/deploy/nginx/vladhost-webmail.conf" /etc/nginx/sites-available/vladhost-webmail.conf
[ -e /etc/nginx/sites-enabled/vladhost-webmail.conf ] || ln -s /etc/nginx/sites-available/vladhost-webmail.conf /etc/nginx/sites-enabled/vladhost-webmail.conf
if nginx -t 2>/tmp/vh-nginx-webmail.log; then
    systemctl reload nginx
else
    cat /tmp/vh-nginx-webmail.log >&2
    rm -f /etc/nginx/sites-enabled/vladhost-webmail.conf
    echo "nginx не принял настройки webmail: виртуальный хост отключён" >&2
    exit 1
fi

# --- настройки панели: адрес webmail для кнопки в разделе «Почта» ---
grep -q '^VLADHOST_WEBMAIL_URL=' "$ENVF" || echo "VLADHOST_WEBMAIL_URL=https://$DOMAIN" >>"$ENVF"
chown root:vladhost "$ENVF"
chmod 0640 "$ENVF"

echo "готово: https://$DOMAIN (Roundcube $VERSION), откат: $BACKUP/rollback-webmail.sh"
[ "${UPGRADED:-0}" = 1 ] && echo "прежняя версия сохранена в /opt/roundcube.old (можно удалить после проверки)"
exit 0

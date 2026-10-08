#!/bin/bash
# Подготовка сервера под почту на своих доменах (Ubuntu 24.04), от root. Идемпотентна.
#
# На сервере уже стоят exim4 и dovecot, настроенные шаблоном ISPmanager, но самого ISPmanager нет (нет /usr/local/mgr5), поэтому входящая
# почта в прежней конфигурации работать не могла. Скрипт:
#   * сохраняет прежние настройки в /root/vladhost-mail-backup (там же rollback.sh — откат одной командой);
#   * ставит чистую конфигурацию exim (deploy/mail/exim4.conf; прежняя отправка с 127.0.0.1 сохраняется) и файл настроек dovecot
#     (conf.d/99-vladhost.conf; системные пользователи продолжают работать);
#   * заводит пользователя vmail, каталоги /etc/vladhost/mail и /var/vmail, сертификат mail.vladinc.ru (SMTP, IMAP, POP3 используют его);
#   * ставит sieve и ManageSieve (фильтры, автоответчик, пересылка с копией), секрет SRS (пересылки без потери SPF), общие правила «спам → Спам».
# Проверку спама и вирусов (rspamd, ClamAV) добавляет prepare-mailfilter.sh; без них письма проходят без проверки.
# Домены, ящики и алиасы дальше пишет панель (deploy/bin/mail-sync.py).
set -euo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
HOST=mail.vladinc.ru
ENVF=/etc/vladhost/env
BACKUP=/root/vladhost-mail-backup
MAILDIR=/etc/vladhost/mail
[ -f "$ENVF" ] || { echo "нет $ENVF: сначала deploy/prepare.sh" >&2; exit 1; }
grep -q '^VLADHOST_RUNTIME_DIR=' "$ENVF" || { echo "нет VLADHOST_RUNTIME_DIR: сначала deploy/prepare-runtime.sh" >&2; exit 1; }
RT=$(grep '^VLADHOST_RUNTIME_DIR=' "$ENVF" | cut -d= -f2-)
command -v exim4 >/dev/null && command -v doveconf >/dev/null || { echo "нужны exim4 и dovecot" >&2; exit 1; }
SYSTEM_HOSTNAME=$(hostname -f 2>/dev/null || hostname)
export DEBIAN_FRONTEND=noninteractive
apt-get install -y -qq --no-install-recommends dovecot-sieve dovecot-managesieved >/dev/null

# --- бэкап прежних настроек (архив создаётся один раз и не перезаписывается; откат-скрипт обновляется при каждом запуске) ---
install -d -m 0700 "$BACKUP"
[ -f "$BACKUP/etc-exim4-dovecot.tar.gz" ] || tar -czf "$BACKUP/etc-exim4-dovecot.tar.gz" -C / etc/exim4 etc/dovecot 2>/dev/null
cat >"$BACKUP/rollback.sh" <<'EOF'
#!/bin/bash
# Возвращает почту к состоянию до prepare-mail.sh: убирает конфигурацию Vladhost и перезапускает exim и dovecot.
set -e
rm -f /etc/exim4/exim4.conf /etc/dovecot/conf.d/99-vladhost.conf
# Прежний шаблон exim возвращается из бэкапа, рабочая конфигурация пересобирается, службы перезапускаются.
tar -xzf /root/vladhost-mail-backup/etc-exim4-dovecot.tar.gz -C / etc/exim4/exim4.conf.template etc/exim4/update-exim4.conf.conf
update-exim4.conf
systemctl restart dovecot exim4
echo "откат выполнен: прежние настройки (шаблон exim и dovecot) снова в силе"
EOF
chmod 0700 "$BACKUP/rollback.sh"

# --- пользователь, каталоги ---
getent group vmail >/dev/null || groupadd -g 5000 vmail
getent passwd vmail >/dev/null || useradd -u 5000 -g vmail -M -d /var/vmail -s /usr/sbin/nologin -c "vladhost virtual mail" vmail
install -d -o vmail -g vmail -m 0755 /var/vmail
install -d -m 0755 "$MAILDIR" "$MAILDIR/tls"
install -d -o root -g Debian-exim -m 0750 "$MAILDIR/dkim"
install -d -o vladhost -g vladhost -m 0750 "$RT/mail"
install -d -m 0755 /usr/local/lib/vladhost
install -d -m 0755 /usr/local/lib/vladhost/sieve-pipe "$MAILDIR/sieve" "$MAILDIR/sieve/before.d"
install -m 0755 "$SRC/deploy/bin/sieve-pipe/"*.sh /usr/local/lib/vladhost/sieve-pipe/
install -m 0644 "$SRC/deploy/mail/sieve/10-spam.sieve" "$MAILDIR/sieve/before.d/10-spam.sieve"
install -m 0644 "$SRC/deploy/mail/sieve/learn-spam.sieve" "$SRC/deploy/mail/sieve/learn-ham.sieve" "$MAILDIR/sieve/"
# Секрет SRS создаётся один раз: по нему принимаются уведомления о недоставке пересланных писем, менять его нельзя.
[ -f "$MAILDIR/srs_secret" ] || { (umask 077; openssl rand -hex 24 >"$MAILDIR/srs_secret"); }
chown root:Debian-exim "$MAILDIR/srs_secret"
chmod 0640 "$MAILDIR/srs_secret"
install -m 0755 "$SRC/deploy/bin/mail-sync.py" "$SRC/deploy/bin/mail-log.py" "$SRC/deploy/bin/runtime.sh" "$SRC/deploy/bin/cert-hook.sh" /usr/local/lib/vladhost/

# --- сертификат mail.vladinc.ru (SMTP, IMAP, POP3): выпуск по HTTP-01, копии и перезагрузка служб делает cert-hook.sh ---
install -m 0644 "$SRC/deploy/nginx/vladhost-http.conf" /etc/nginx/sites-available/vladhost-http.conf
[ -e /etc/nginx/sites-enabled/vladhost-http.conf ] || ln -s /etc/nginx/sites-available/vladhost-http.conf /etc/nginx/sites-enabled/vladhost-http.conf
nginx -t
systemctl reload nginx
if [ ! -f /etc/letsencrypt/live/$HOST/fullchain.pem ]; then
    certbot certonly --webroot -w /var/www/vladhost-acme -d $HOST --cert-name $HOST --non-interactive --agree-tos \
        --register-unsafely-without-email --deploy-hook /usr/local/lib/vladhost/cert-hook.sh
fi
# Копия нужна до запуска служб; сами службы хук не перезапускает, пока нет наших настроек (см. cert-hook.sh).
RENEWED_LINEAGE=/etc/letsencrypt/live/$HOST VH_MAIL_NO_RELOAD=1 /usr/local/lib/vladhost/cert-hook.sh

# --- пустое состояние: файлы списков должны существовать до перезапуска exim ---
[ -f "$RT/mail/state.json" ] || { echo '{"version":1,"domains":[]}' >"$RT/mail/state.json"; chown vladhost:vladhost "$RT/mail/state.json"; chmod 0640 "$RT/mail/state.json"; }
python3 /usr/local/lib/vladhost/mail-sync.py --state "$RT/mail/state.json" --usage "$RT/mail/usage.json" >/dev/null

# --- проверка новой конфигурации до подмены ---
sed -e "s|@MAIL_HOST@|$HOST|g" -e "s|@SYSTEM_HOSTNAME@|$SYSTEM_HOSTNAME|g" "$SRC/deploy/mail/exim4.conf" >/tmp/vladhost-exim4.conf.new
exim4 -C /tmp/vladhost-exim4.conf.new -bV >/dev/null
# прежняя отправка с сервера должна маршрутизироваться так же: во внешний домен через DNS
exim4 -C /tmp/vladhost-exim4.conf.new -bt someone@gmail.com 2>&1 | grep -q 'router = dnslookup' || { echo "проверка маршрута наружу не прошла" >&2; exit 1; }
exim4 -C /tmp/vladhost-exim4.conf.new -bt root@localhost 2>&1 | grep -qE 'router = (localuser|system_aliases)' || { echo "проверка локального маршрута не прошла" >&2; exit 1; }
install -m 0644 "$SRC/deploy/mail/dovecot-99-vladhost.conf" /tmp/99-vladhost.conf.new
# sieve-скрипты должны компилироваться до подмены конфигурации (для этого нужен уже готовый файл настроек dovecot)

# --- установка ---
# exim собирает рабочую конфигурацию из шаблона (update-exim4.conf при каждом запуске службы), поэтому подменяется шаблон; прежний — в бэкапе.
install -m 0644 /tmp/vladhost-exim4.conf.new /etc/exim4/exim4.conf.template
install -m 0644 /tmp/99-vladhost.conf.new /etc/dovecot/conf.d/99-vladhost.conf
update-exim4.conf
exim4 -bV >/dev/null 2>&1 || { "$BACKUP/rollback.sh"; echo "exim не принял конфигурацию: откат выполнен" >&2; exit 1; }
if ! doveconf -n >/dev/null 2>/tmp/doveconf.err; then
    cat /tmp/doveconf.err >&2
    "$BACKUP/rollback.sh"
    echo "dovecot не принял настройки: откат выполнен" >&2
    exit 1
fi
# Компиляция заодно проверяет скрипты. Скриптам обучения нужны плагины imapsieve и extprograms: sievec получает их ключами (из настроек dovecot он их не берёт).
sievec "$MAILDIR/sieve/before.d/10-spam.sieve" || { "$BACKUP/rollback.sh"; echo "sieve-скрипт 10-spam.sieve не компилируется: откат выполнен" >&2; exit 1; }
for f in "$MAILDIR/sieve/learn-spam.sieve" "$MAILDIR/sieve/learn-ham.sieve"; do
    sievec -P sieve_imapsieve -P sieve_extprograms -x "+vnd.dovecot.pipe +vnd.dovecot.environment" "$f" || { "$BACKUP/rollback.sh"; echo "sieve-скрипт $f не компилируется: откат выполнен" >&2; exit 1; }
done
systemctl restart dovecot
systemctl restart exim4
sleep 2
systemctl is-active --quiet dovecot && systemctl is-active --quiet exim4 || { "$BACKUP/rollback.sh"; echo "службы не поднялись: откат выполнен" >&2; exit 1; }
[ "$(exim4 -bP primary_hostname | awk '{print $3}')" = "$HOST" ] || { "$BACKUP/rollback.sh"; echo "exim не подхватил имя сервера: откат выполнен" >&2; exit 1; }
ss -ltn | grep -q ':4190 ' || { "$BACKUP/rollback.sh"; echo "ManageSieve не слушает 4190: откат выполнен" >&2; exit 1; }
grep -q 'vladhost_mailbox' "$(exim4 -bP config_file)" || { "$BACKUP/rollback.sh"; echo "exim не подхватил новую конфигурацию: откат выполнен" >&2; exit 1; }
rm -f /tmp/vladhost-exim4.conf.new /tmp/99-vladhost.conf.new /tmp/doveconf.err

# --- настройки панели ---
set_env() { grep -q "^$1=" "$ENVF" || echo "$1=$2" >>"$ENVF"; }
set_env VLADHOST_MAIL_HOST $HOST
set_env VLADHOST_MAIL_SERVER_IP "$(curl -fsS --max-time 10 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')"
chown root:vladhost "$ENVF"
chmod 0640 "$ENVF"

echo "готово: exim $(systemctl is-active exim4), dovecot $(systemctl is-active dovecot), откат: $BACKUP/rollback.sh"

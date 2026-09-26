#!/bin/bash
# Защита почты от спама и вирусов (Ubuntu 24.04), от root, после prepare-mail.sh. Идемпотентна.
#
#   * rspamd: SPF, DKIM, DMARC, содержимое, самообучение и обучение по папке «Спам» (вызывается из exim и из sieve);
#   * собственный Redis для rspamd: только unix-сокет, память ограничена, Redis других проектов сервера не используется;
#   * ClamAV: clamd + freshclam (вирусы отклоняются прямо в SMTP-сессии).
# Настройки exim и dovecot, которые обращаются к этим службам, ставит prepare-mail.sh. Пока служб нет, письма проходят без проверки.
# Откат: rollback-filters.sh рядом с бэкапом почты останавливает и отключает эти службы (пакеты остаются).
set -euo pipefail

BACKUP=/root/vladhost-mail-backup
[ -f "$BACKUP/rollback.sh" ] || { echo "сначала deploy/prepare-mail.sh" >&2; exit 1; }
grep -q 'variant=rspamd' /etc/exim4/exim4.conf.template || { echo "в exim нет настроек проверки: выполните заново deploy/prepare-mail.sh" >&2; exit 1; }

cat >"$BACKUP/rollback-filters.sh" <<'EOF'
#!/bin/bash
# Отключает проверку спама и вирусов: почта продолжает идти без неё (exim при недоступных службах пропускает письма).
systemctl disable --now rspamd clamav-daemon clamav-freshclam vladhost-redis vladhost-clamd-ready.timer 2>/dev/null || true
echo "rspamd, ClamAV и Redis для rspamd остановлены и отключены"
EOF
chmod 0700 "$BACKUP/rollback-filters.sh"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq || echo "ВНИМАНИЕ: apt-get update завершился с ошибкой: продолжаем с имеющимся кэшем пакетов" >&2
apt-get install -y -qq --no-install-recommends rspamd clamav-daemon clamav-freshclam >/dev/null

# --- собственный Redis для rspamd ---
install -d -m 0755 /etc/vladhost
cat >/etc/vladhost/redis-rspamd.conf <<'EOF'
# Redis для rspamd (обучение, кэш, лимиты). Только сокет, без сети.
port 0
unixsocket /run/vladhost-redis/redis.sock
unixsocketperm 770
dir /var/lib/vladhost-redis
maxmemory 160mb
maxmemory-policy allkeys-lru
save 900 1
appendonly no
EOF
cat >/etc/systemd/system/vladhost-redis.service <<'EOF'
[Unit]
Description=Vladhost: Redis для rspamd
After=network.target

[Service]
User=redis
Group=redis
RuntimeDirectory=vladhost-redis
RuntimeDirectoryMode=0755
StateDirectory=vladhost-redis
ExecStart=/usr/bin/redis-server /etc/vladhost/redis-rspamd.conf
Restart=on-failure
ProtectSystem=full
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
EOF
usermod -aG redis _rspamd
systemctl daemon-reload
systemctl enable vladhost-redis >/dev/null 2>&1
systemctl restart vladhost-redis
for _ in $(seq 1 30); do [ -S /run/vladhost-redis/redis.sock ] && break; sleep 0.2; done
[ -S /run/vladhost-redis/redis.sock ] || { echo "Redis для rspamd не поднялся" >&2; journalctl -u vladhost-redis -n 20 --no-pager >&2; exit 1; }

# --- rspamd ---
install -d -m 0755 /etc/rspamd/local.d
cat >/etc/rspamd/local.d/worker-normal.inc <<'EOF'
# Проверки от exim (протокол rspamd) принимаются только с самого сервера.
enabled = true;
bind_socket = "127.0.0.1:11333";
count = 1;
EOF
cat >/etc/rspamd/local.d/redis.conf <<'EOF'
servers = "/run/vladhost-redis/redis.sock";
EOF
cat >/etc/rspamd/local.d/actions.conf <<'EOF'
# От 6 баллов — заголовок X-Spam (письмо уходит в «Спам»), от 15 — отказ в SMTP-сессии. Отложенная доставка (greylisting) не используется.
reject = 15;
add_header = 6;
greylist = 100;
EOF
cat >/etc/rspamd/local.d/classifier-bayes.conf <<'EOF'
backend = "redis";
autolearn = true;
new_schema = true;
EOF
cat >/etc/rspamd/local.d/dkim_signing.conf <<'EOF'
# Исходящие письма подписывает exim (ключи доменов у него), rspamd только проверяет входящие.
enabled = false;
EOF
systemctl enable rspamd >/dev/null 2>&1
systemctl restart rspamd
for _ in $(seq 1 60); do (echo >/dev/tcp/127.0.0.1/11333) 2>/dev/null && break; sleep 0.5; done
(echo >/dev/tcp/127.0.0.1/11333) 2>/dev/null || { echo "rspamd не слушает 11333" >&2; journalctl -u rspamd -n 30 --no-pager >&2; exit 1; }

# --- ClamAV: база вирусов, затем демон; без удвоения памяти при обновлении базы ---
grep -q '^# vladhost' /etc/clamav/clamd.conf || cat >>/etc/clamav/clamd.conf <<'EOF'
# vladhost: экономия памяти на небольшом сервере
ConcurrentDatabaseReload no
MaxThreads 4
StreamMaxLength 26M
MaxFileSize 26M
EOF
have_db() { ls /var/lib/clamav/*.cvd /var/lib/clamav/*.cld >/dev/null 2>&1; }
if ! have_db; then
    systemctl stop clamav-freshclam 2>/dev/null || true
    timeout 600 freshclam --quiet || echo "ВНИМАНИЕ: базу ClamAV скачать не удалось (у CDN бывает ограничение по числу загрузок с одного адреса): clamd запустится сам, когда база появится" >&2
fi
systemctl enable clamav-freshclam >/dev/null 2>&1
systemctl restart clamav-freshclam || true
systemctl enable clamav-daemon >/dev/null 2>&1
# clamd без базы не стартует (условие в его unit-файле); таймер запускает его, как только freshclam скачает базу, и затем отключается сам
cat >/etc/systemd/system/vladhost-clamd-ready.service <<'EOF'
[Unit]
Description=Vladhost: запуск clamd, когда появилась база вирусов

[Service]
Type=oneshot
ExecStart=/bin/sh -c 'ls /var/lib/clamav/*.cvd /var/lib/clamav/*.cld >/dev/null 2>&1 && systemctl start clamav-daemon && systemctl disable --now vladhost-clamd-ready.timer || true'
EOF
cat >/etc/systemd/system/vladhost-clamd-ready.timer <<'EOF'
[Unit]
Description=Vladhost: ждём базу вирусов для clamd

[Timer]
OnBootSec=2min
OnUnitActiveSec=15min

[Install]
WantedBy=timers.target
EOF
systemctl daemon-reload
if have_db; then
    systemctl restart clamav-daemon
    for _ in $(seq 1 180); do [ -S /var/run/clamav/clamd.ctl ] && break; sleep 1; done
    [ -S /var/run/clamav/clamd.ctl ] || { echo "clamd не поднялся" >&2; journalctl -u clamav-daemon -n 30 --no-pager >&2; exit 1; }
else
    systemctl enable --now vladhost-clamd-ready.timer >/dev/null 2>&1
    echo "ClamAV: базы пока нет, письма проходят без проверки на вирусы; clamd запустится автоматически после загрузки базы (таймер vladhost-clamd-ready)" >&2
fi
usermod -aG clamav Debian-exim
systemctl restart exim4

echo "готово: rspamd $(systemctl is-active rspamd), clamd $(systemctl is-active clamav-daemon || true), Redis $(systemctl is-active vladhost-redis); откат: $BACKUP/rollback-filters.sh"
free -m | sed -n 1,2p

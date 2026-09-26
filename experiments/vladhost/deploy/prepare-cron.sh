#!/bin/bash
# Подготовка VPS под команды планировщика (Ubuntu 24.04), от root. Идемпотентна. HTTP-задачи работают и без неё.
# Создаёт очередь заявок и результатов, ставит исполнитель (cron-run.sh, вызывается по появлению заявки) и включает команды
# в панели (VLADHOST_CRON_* в /etc/vladhost/env; подхватит при следующей выкладке).
set -euo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
ENVF=/etc/vladhost/env
[ -f "$ENVF" ] || { echo "нет $ENVF: сначала deploy/prepare.sh" >&2; exit 1; }

# Панель кладёт заявки (владелец vladhost); результаты пишет только root, панель их читает; work — только root.
install -d -m 0755 /var/lib/vladhost/cron
install -d -o vladhost -g vladhost -m 0750 /var/lib/vladhost/cron/queue
install -d -m 0755 /var/lib/vladhost/cron/results
install -d -m 0700 /var/lib/vladhost/cron/work

install -d -m 0755 /usr/local/lib/vladhost
install -m 0755 "$SRC/deploy/bin/cron-run.sh" /usr/local/lib/vladhost/cron-run.sh
install -m 0644 "$SRC/deploy/systemd/vladhost-cron.path" "$SRC/deploy/systemd/vladhost-cron.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now vladhost-cron.path >/dev/null 2>&1

grep -q '^VLADHOST_CRON_QUEUE=' "$ENVF" || echo 'VLADHOST_CRON_QUEUE=/var/lib/vladhost/cron/queue' >>"$ENVF"
grep -q '^VLADHOST_CRON_RESULTS=' "$ENVF" || echo 'VLADHOST_CRON_RESULTS=/var/lib/vladhost/cron/results' >>"$ENVF"
chown root:vladhost "$ENVF"
chmod 0640 "$ENVF"

echo "готово: vladhost-cron.path $(systemctl is-active vladhost-cron.path), песочница: $(systemd-run --version | head -1)"

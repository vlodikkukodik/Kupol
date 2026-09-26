#!/bin/bash
# Подготовка VPS под SSH и веб-терминал сайтов (Ubuntu 24.04), от root. Идемпотентна.
# Нужны среды выполнения (prepare-runtime.sh): исполнитель создаёт пользователя сайта и ставит метку доступа. Здесь: посредник оболочек
# (служба от root), инструменты для работы в оболочке и настройки панели. Системный sshd на порту 22 не трогается: SSH сайтов
# принимает сама панель на порту 2222.
set -euo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
ENVF=/etc/vladhost/env
[ -f "$ENVF" ] || { echo "нет $ENVF: сначала deploy/prepare.sh" >&2; exit 1; }
grep -q '^VLADHOST_RUNTIME_DIR=' "$ENVF" || { echo "нет VLADHOST_RUNTIME_DIR: сначала deploy/prepare-runtime.sh" >&2; exit 1; }
[ -x /opt/vladhost/bin/vladhost ] || { echo "нет /opt/vladhost/bin/vladhost: сначала выкладка (deploy.sh)" >&2; exit 1; }

# --- инструменты для оболочки пользователя и SFTP ---
export DEBIAN_FRONTEND=noninteractive
apt-get install -y -qq git rsync openssh-sftp-server less nano vim-tiny unzip zip curl ca-certificates >/dev/null
[ -x /usr/lib/openssh/sftp-server ] || { echo "нет /usr/lib/openssh/sftp-server" >&2; exit 1; }

# --- служба-посредник ---
install -d -m 0755 /usr/local/lib/vladhost
install -m 0644 "$SRC/deploy/systemd/vladhost-shell.service" /etc/systemd/system/
systemctl daemon-reload
systemctl enable vladhost-shell.service >/dev/null 2>&1
systemctl restart vladhost-shell.service
for _ in $(seq 1 20); do [ -S /run/vladhost/shell.sock ] && break; sleep 0.5; done
[ -S /run/vladhost/shell.sock ] || { journalctl -u vladhost-shell -n 20 --no-pager >&2; echo "сокет посредника не появился" >&2; exit 1; }

# --- настройки панели (подхватит при следующей выкладке) ---
install -d -o vladhost -g vladhost -m 0700 /data/vladhost/ssh
set_env() { grep -q "^$1=" "$ENVF" || echo "$1=$2" >>"$ENVF"; }
set_env VLADHOST_SHELL_SOCKET /run/vladhost/shell.sock
set_env VLADHOST_SSH_ADDR :2222
set_env VLADHOST_SSH_HOST ssh.vladinc.ru
chown root:vladhost "$ENVF"
chmod 0640 "$ENVF"

echo "готово: vladhost-shell $(systemctl is-active vladhost-shell), сокет $(stat -c '%a %U:%G' /run/vladhost/shell.sock), SSH панели — порт 2222"

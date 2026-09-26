#!/bin/bash
# Сборка и выкладка на VPS: ./deploy/deploy.sh
# Доступ: VLADHOST_SSH (по умолчанию root@37.153.70.33). Если задан SSHPASS — вход по паролю через sshpass,
# иначе обычный ssh (ключ). Рекомендуется перейти на ключ и отключить вход root по паролю.
set -euo pipefail
cd "$(dirname "$0")/.."

TARGET=${VLADHOST_SSH:-root@37.153.70.33}
# accept-new: в свежем окружении (новый Codespace, пустой known_hosts) ключ сервера запоминается при первом входе;
# без этого sshpass молча выходит с кодом 6, и видна только ошибка tar «Wrote only … / status 141».
SSH_OPTS=(-o ConnectTimeout=15 -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=15)
remote() {
    if [ -n "${SSHPASS:-}" ]; then sshpass -e ssh "${SSH_OPTS[@]}" "$TARGET" "$@"; else ssh "${SSH_OPTS[@]}" "$TARGET" "$@"; fi
}

# sshpass сам ничего не пишет при отказе — переводим его коды в понятные сообщения.
explain() {
    case $1 in
        5) echo "sshpass: неверный пароль (проверьте SSHPASS)" ;;
        6) echo "sshpass: ключ сервера неизвестен или изменился — проверьте ~/.ssh/known_hosts" ;;
        255) echo "ssh: не удалось подключиться к $TARGET" ;;
        *) echo "удалённая команда завершилась с кодом $1" ;;
    esac
}

rc=0; remote true || rc=$?
if [ "$rc" != 0 ]; then echo "нет доступа к серверу: $(explain "$rc")" >&2; exit 1; fi

make build

# Архив сначала собираем в файл: так ошибка упаковки не смешивается с ошибкой передачи.
PKG=$(mktemp --suffix=.tgz)
trap 'rm -f "$PKG"' EXIT
tar -czf "$PKG" bin/vladhost frontend/dist deploy
echo "архив: $(du -h "$PKG" | cut -f1), передаю…"

rc=0
remote 'rm -rf /tmp/vh-up && mkdir /tmp/vh-up && tar -xzf - -C /tmp/vh-up' <"$PKG" || rc=$?
if [ "$rc" != 0 ]; then echo "передача не удалась: $(explain "$rc")" >&2; exit 1; fi
remote 'bash /tmp/vh-up/deploy/install-remote.sh; rc=$?; rm -rf /tmp/vh-up; exit $rc'

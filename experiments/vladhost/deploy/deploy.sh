#!/bin/bash
# Сборка и выкладка на VPS: ./deploy/deploy.sh
# Доступ: VLADHOST_SSH (по умолчанию root@37.153.70.33). Если задан SSHPASS — вход по паролю через sshpass,
# иначе обычный ssh (ключ). Рекомендуется перейти на ключ и отключить вход root по паролю.
set -euo pipefail
cd "$(dirname "$0")/.."

TARGET=${VLADHOST_SSH:-root@37.153.70.33}
remote() {
    if [ -n "${SSHPASS:-}" ]; then sshpass -e ssh -o ConnectTimeout=15 "$TARGET" "$@"; else ssh -o ConnectTimeout=15 "$TARGET" "$@"; fi
}

make build
tar -czf - bin/vladhost frontend/dist deploy |
    remote 'rm -rf /tmp/vh-up && mkdir /tmp/vh-up && tar -xzf - -C /tmp/vh-up'
remote 'bash /tmp/vh-up/deploy/install-remote.sh; rc=$?; rm -rf /tmp/vh-up; exit $rc'

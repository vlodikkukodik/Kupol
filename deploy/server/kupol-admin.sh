#!/usr/bin/env bash
# /usr/local/bin/kupol-admin — административные команды КУПОЛ на VPS.
#
#   sudo kupol-admin user show <логин>
#   sudo kupol-admin user set-level <логин> <1-6>
#   sudo kupol-admin user set-directorate <логин> on|off
#   sudo kupol-admin user set-role <логин> <author|editor|moderator|archivist> on|off
#   sudo kupol-admin user reset-password <логин>
#   sudo kupol-admin migrate status
#   sudo kupol-admin doc import [--dry-run] файл.json...   (файл должен быть читаем пользователем kupol)
#   sudo kupol-admin doc list | export <шифр> | set-status <шифр> <статус> | delete <шифр> --yes
#   sudo kupol-admin audit [--limit N] [--action событие] [--doc <номер документа>]   журнал событий
#
# Читает настройки из /etc/kupol/kupol.env (доступен только root) и запускает бинарник
# под пользователем kupol, как и сам сервис.
set -euo pipefail

ENV_FILE="${KUPOL_ENV_FILE:-/etc/kupol/kupol.env}"
BIN="${KUPOL_BIN:-/opt/kupol/kupol}"
RUN_AS="${KUPOL_USER:-kupol}"

[ "$(id -u)" -eq 0 ] || { echo "запускайте от root: sudo kupol-admin $*" >&2; exit 1; }
[ -r "$ENV_FILE" ] || { echo "нет $ENV_FILE" >&2; exit 1; }
[ -x "$BIN" ] || { echo "нет $BIN" >&2; exit 1; }

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
exec runuser -u "$RUN_AS" -- "$BIN" "$@"

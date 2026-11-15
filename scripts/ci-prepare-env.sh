#!/usr/bin/env bash
# Дополняет боевой .env бэкенда тем, что CI может сгенерировать сам, и готовит api/config.php прокси.
#
#   scripts/ci-prepare-env.sh OLD_ENV NEW_ENV OLD_CONFIG_PHP NEW_CONFIG_PHP
#
# OLD_* — файлы, скачанные с хостинга (могут отсутствовать). Ничего, что уже задано в .env, не меняется:
# добавляются только недостающие ключи. Остальное (KUPOL_DATABASE_URL, SMTP и т.п.) вы ставите сами.
# Если в .env нет секрета, а config.php на сайте есть — секрет берётся из config.php.
# config.php генерируется, только если его нет на хостинге или секрет пришлось создать заново.
# Переменные окружения (необязательные): API_URL (upstream для прокси), SITE_ORIGIN.
# На stdout печатает "env_changed=0|1" и "config_changed=0|1" (для $GITHUB_OUTPUT).
set -euo pipefail

old_env="$1" new_env="$2" old_cfg="$3" new_cfg="$4"
env_changed=0 config_changed=0 secret_created=0

if [ -f "$old_env" ]; then cp "$old_env" "$new_env"; else : >"$new_env"; fi
# Гарантируем перевод строки в конце, чтобы дописывание не склеило строки.
[ -s "$new_env" ] && [ -n "$(tail -c1 "$new_env")" ] && echo >>"$new_env"

has() { grep -Eq "^[[:space:]]*(export[[:space:]]+)?$1=" "$new_env"; }
get() { grep -E "^[[:space:]]*(export[[:space:]]+)?$1=" "$new_env" | tail -1 | sed -E "s/^[^=]*=//; s/^['\"]//; s/['\"]\$//"; }
add() {
  if ! has "$1"; then
    [ "$env_changed" -eq 1 ] || printf '\n# --- добавлено CI %s ---\n' "$(date -u +%F)" >>"$new_env"
    echo "$1=$2" >>"$new_env"; env_changed=1
  fi
}

if ! has KUPOL_PROXY_SECRET; then
  # Если на сайте уже есть config.php — берём секрет из него, чтобы прокси и API совпали.
  cfg_secret=""
  [ -s "$old_cfg" ] && cfg_secret="$(sed -nE "s/.*'secret'[[:space:]]*=>[[:space:]]*'([^']+)'.*/\1/p" "$old_cfg" | head -1)"
  if [ -n "$cfg_secret" ] && [ "${#cfg_secret}" -ge 32 ] && [[ "$cfg_secret" != *ЗАМЕНИТЬ* ]]; then
    add KUPOL_PROXY_SECRET "$cfg_secret"
  else
    add KUPOL_PROXY_SECRET "$(openssl rand -hex 32)"; secret_created=1
  fi
fi
add KUPOL_ENV prod
add KUPOL_LOG_LEVEL info
add KUPOL_UPLOADS_DIR uploads
[ -n "${SITE_ORIGIN:-}" ] && add KUPOL_SITE_ORIGIN "${SITE_ORIGIN%/}"

# Подсказки для того, что CI сгенерировать не может (закомментированы — заполните сами).
if ! grep -q '^# KUPOL_DATABASE_URL=' "$new_env" && ! has KUPOL_DATABASE_URL; then
  cat >>"$new_env" <<'HINT'
# Заполните сами:
# KUPOL_DATABASE_URL=postgres://user:password@host:5432/kupol?sslmode=require
# KUPOL_TRUSTED_PROXIES=
# KUPOL_SMTP_HOST=
# KUPOL_SMTP_PORT=587
# KUPOL_SMTP_USERNAME=
# KUPOL_SMTP_PASSWORD=
# KUPOL_SMTP_FROM=
HINT
  env_changed=1
fi

secret="$(get KUPOL_PROXY_SECRET)"
if [ -n "${GITHUB_ACTIONS:-}" ]; then echo "::add-mask::$secret" >&2; fi

if [ ! -s "$old_cfg" ] || [ "$secret_created" -eq 1 ]; then
  upstream="${API_URL:-https://ЗАМЕНИТЬ_НА_АДРЕС_API}"
  esc() { printf '%s' "$1" | sed "s/\\\\/\\\\\\\\/g; s/'/\\\\'/g"; }
  cat >"$new_cfg" <<PHP
<?php
// Сгенерировано CI. Секрет совпадает с KUPOL_PROXY_SECRET в .env бэкенда.
return [
    'upstream' => '$(esc "${upstream%/}")',
    'secret' => '$(esc "$secret")',
    'ip_header' => null,
    'connect_timeout' => 5,
    'timeout' => 30,
];
PHP
  config_changed=1
fi

echo "env_changed=$env_changed"
echo "config_changed=$config_changed"

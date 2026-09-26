#!/bin/bash
# Подготовка VPS под среды выполнения сайтов: PHP (8.2, 8.3, 8.4), Node.js и Python (Ubuntu 24.04), от root. Идемпотентна.
# Не трогает чужие службы и правила брандмауэра: PHP ставится из репозитория Ondřej Surý (отдельные пакеты по версиям, рядом с
# системным PHP), Node.js и Python берутся уже установленные, правила пишутся в свою таблицу nftables.
# Заявки от панели принимает vladhost-runtime.path (deploy/bin/runtime.sh); файл caps говорит панели, что установлено.
set -euo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
ENVF=/etc/vladhost/env
RT=/var/lib/vladhost/runtime
PHP_VERSIONS=${VH_PHP_VERSIONS:-"8.2 8.3 8.4"}
PHP_EXT="fpm cli mysql pgsql sqlite3 mbstring xml curl gd zip intl bcmath soap"

[ -f "$ENVF" ] || { echo "нет $ENVF: сначала deploy/prepare.sh" >&2; exit 1; }
export DEBIAN_FRONTEND=noninteractive

# --- пакеты ---
apt-get update -qq
apt-get install -y -qq acl libfcgi-bin software-properties-common python3 python3-venv python3-pip >/dev/null
if ! grep -rqs 'ondrej/php' /etc/apt/sources.list /etc/apt/sources.list.d 2>/dev/null; then
    add-apt-repository -y ppa:ondrej/php >/dev/null
    apt-get update -qq
fi
# Пакеты новых версий переключают «php» в системе на самую новую: возвращаем прежнюю, чтобы не менять поведение чужих проектов.
prev_php=$(readlink -f /usr/bin/php 2>/dev/null || true)
for v in $PHP_VERSIONS; do
    pkgs=()
    for e in $PHP_EXT; do pkgs+=("php$v-$e"); done
    apt-get install -y -qq "${pkgs[@]}" >/dev/null
    # Стандартный пул www не нужен: сайты получают свои пулы (vhs{ID}). Файл переименовывается, а не удаляется, и только если он
    # стандартный (пул www-data): чужие пулы в этих версиях не трогаем.
    pool=/etc/php/$v/fpm/pool.d/www.conf
    if [ -f "$pool" ] && grep -q '^\[www\]' "$pool" && grep -q '^user = www-data' "$pool"; then
        mv "$pool" "$pool.vladhost-disabled"
    fi
    install -d -m 0755 /etc/systemd/system/php$v-fpm.service.d
    printf '[Service]\nMemoryMax=1500M\n' >/etc/systemd/system/php$v-fpm.service.d/vladhost.conf
done

if [ -n "$prev_php" ] && [ -x "$prev_php" ] && [ "$(readlink -f /usr/bin/php)" != "$prev_php" ]; then
    update-alternatives --set php "$prev_php" >/dev/null
fi

# --- каталоги и пользователи ---
install -d -m 0755 /var/lib/vladhost /usr/local/lib/vladhost
install -d -m 0755 "$RT" "$RT/results" "$RT/sites"
install -d -o vladhost -g vladhost -m 0750 "$RT/queue"
install -d -m 0700 "$RT/work"
install -d -m 0755 /usr/local/lib/vladhost/apps
# /run очищается при перезагрузке: каталог сокетов пулов создаётся заново systemd-tmpfiles.
printf 'd /run/vhphp 0755 root root -\n' >/etc/tmpfiles.d/vladhost-php.conf
systemd-tmpfiles --create /etc/tmpfiles.d/vladhost-php.conf

# --- исполнитель, правила и службы ---
install -m 0755 "$SRC/deploy/bin/runtime.sh" /usr/local/lib/vladhost/runtime.sh
install -m 0644 "$SRC/deploy/nft/vladhost-rt.nft" /etc/vladhost/rt.nft
nft -c -f /etc/vladhost/rt.nft
install -m 0644 "$SRC"/deploy/systemd/vladhost-runtime.path "$SRC"/deploy/systemd/vladhost-runtime.service \
    "$SRC"/deploy/systemd/vladhost-runtime-perms.service "$SRC"/deploy/systemd/vladhost-runtime-perms.timer \
    "$SRC"/deploy/systemd/vladhost-rt-nft.service /etc/systemd/system/
systemctl daemon-reload
for v in $PHP_VERSIONS; do systemctl enable --now "php$v-fpm" >/dev/null 2>&1; done
systemctl enable --now vladhost-rt-nft.service >/dev/null 2>&1
systemctl enable --now vladhost-runtime.path vladhost-runtime-perms.timer >/dev/null 2>&1

# --- что установлено (читает панель) ---
installed=()
for v in $PHP_VERSIONS; do
    [ -x "/usr/sbin/php-fpm$v" ] && installed+=("$v")
done
{
    echo "php=$(IFS=,; echo "${installed[*]}")"
    if command -v node >/dev/null 2>&1; then echo "node=$(node -v | sed 's/^v//')"; else echo "node="; fi
    echo "python=$(python3 -c 'import platform; print(platform.python_version())')"
} >"$RT/caps.new"
chmod 0644 "$RT/caps.new"
mv -f "$RT/caps.new" "$RT/caps"

grep -q '^VLADHOST_RUNTIME_DIR=' "$ENVF" || echo "VLADHOST_RUNTIME_DIR=$RT" >>"$ENVF"
chown root:vladhost "$ENVF"
chmod 0640 "$ENVF"

echo "готово: $(tr '\n' ' ' <"$RT/caps")"

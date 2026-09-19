#!/usr/bin/env bash
# Проверка самих скриптов деплоя на настоящих протоколах, но локально:
#   бэкенд  — настоящий sshd (порт на loopback) + scp + перезапуск процесса;
#   фронтенд — настоящий FTP-сервер (pyftpdlib) + lftp, а результат раздаёт Apache + PHP 8.3.
# Порядок как в проде: сначала бэкенд, потом фронтенд, затем deploy/check-site.sh.
#
# Требуется: docker, lftp, sshd, ssh-keygen, jq, curl, go, npm, python3 (venv с pyftpdlib создаётся сам).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
[ -f .env ] || { echo "нет .env — выполните make env"; exit 1; }
set -a; . ./.env; set +a
export XDEBUG_MODE=off

for bin in docker lftp ssh ssh-keygen jq curl go npm python3; do command -v "$bin" >/dev/null || { echo "нужен $bin"; exit 1; }; done
SSHD="$(command -v sshd || echo /usr/sbin/sshd)"
[ -x "$SSHD" ] || { echo "нужен sshd"; exit 1; }
docker compose up -d --wait postgres >/dev/null

TMP="$(mktemp -d)"
DB="kupol_deploy_$(openssl rand -hex 4)"
CONTAINER="kupol-deploy-test-$$"
PIDS=()
cleanup() {
  [ -f "$TMP/vps/kupol.pid" ] && kill "$(cat "$TMP/vps/kupol.pid")" 2>/dev/null || true
  for p in "${PIDS[@]:-}"; do kill "$p" 2>/dev/null || true; done
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  wait 2>/dev/null || true
  docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$TMP" 2>/dev/null || true
}
trap cleanup EXIT

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }
SSH_PORT="$(free_port)"; FTP_PORT="$(free_port)"; API_PORT="$(free_port)"; WEB_PORT="$(free_port)"

FAILS=0
expect() { if [ "$2" = "$3" ]; then echo "  ok   $1"; else echo "  FAIL $1: получено '$2', ожидалось '$3'"; FAILS=$((FAILS+1)); fi; }
expect_fail() { # описание, команда...: должна завершиться с ненулевым кодом
  local d="$1"; shift
  if "$@" >"$TMP/last.out" 2>&1; then echo "  FAIL $d: команда прошла, а должна была упасть"; FAILS=$((FAILS+1)); else echo "  ok   $d"; fi
}

docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d postgres -qc \
  "CREATE DATABASE $DB ENCODING 'UTF8' LOCALE_PROVIDER icu ICU_LOCALE 'ru-RU' LOCALE 'C' TEMPLATE template0"

echo "== сборка Go и фронтенда"
make -s build-back
(cd frontend && npm ci --silent && npm run build --silent)

# ---------------------------------------------------------------- «VPS»: настоящий sshd
echo "== sshd на :$SSH_PORT"
ssh-keygen -q -t ed25519 -N '' -f "$TMP/host_key"
ssh-keygen -q -t ed25519 -N '' -f "$TMP/client_key"
cp "$TMP/client_key.pub" "$TMP/authorized_keys"
# umask в некоторых окружениях открытый, а ssh/sshd отвергают ключи с широкими правами
chmod 600 "$TMP/authorized_keys" "$TMP/host_key" "$TMP/client_key"
cat >"$TMP/sshd_config" <<EOF
Port $SSH_PORT
ListenAddress 127.0.0.1
HostKey $TMP/host_key
PidFile $TMP/sshd.pid
AuthorizedKeysFile $TMP/authorized_keys
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
UsePAM no
StrictModes no
Subsystem sftp $(ls /usr/lib/openssh/sftp-server /usr/libexec/openssh/sftp-server 2>/dev/null | head -1)
EOF
"$SSHD" -D -e -f "$TMP/sshd_config" 2>"$TMP/sshd.log" &
PIDS+=($!)
for _ in $(seq 1 50); do (exec 3<>"/dev/tcp/127.0.0.1/$SSH_PORT") 2>/dev/null && break; sleep 0.1; done

VPS="$TMP/vps"
mkdir -p "$VPS"

# «systemctl restart kupol»: останавливает процесс по pid-файлу и запускает бинарник заново.
cat >"$TMP/restart.sh" <<EOF
#!/usr/bin/env bash
cd "$VPS"
if [ -f kupol.pid ]; then
  pid=\$(cat kupol.pid)
  kill "\$pid" 2>/dev/null || true
  for _ in \$(seq 1 100); do kill -0 "\$pid" 2>/dev/null || break; sleep 0.1; done
  rm -f kupol.pid
fi
export KUPOL_ENV=prod KUPOL_LOG_LEVEL=info
export KUPOL_DATABASE_URL='postgres://$KUPOL_DB_USER:$KUPOL_DB_PASSWORD@127.0.0.1:$KUPOL_DB_PORT/$DB?sslmode=disable'
export KUPOL_HTTP_ADDR='127.0.0.1:$API_PORT'
export KUPOL_PROXY_SECRET='$KUPOL_PROXY_SECRET'
export KUPOL_SITE_ORIGIN='http://127.0.0.1:$WEB_PORT'
nohup ./kupol serve >>kupol.log 2>&1 &
echo \$! >kupol.pid
EOF
chmod +x "$TMP/restart.sh"

cat >"$TMP/env.deploy" <<EOF
VPS_HOST=127.0.0.1
VPS_PORT=$SSH_PORT
VPS_USER=$(id -un)
VPS_DIR=$VPS
VPS_RESTART_CMD=$TMP/restart.sh
VPS_HEALTH_URL=http://127.0.0.1:$API_PORT/health
VPS_HEALTH_WAIT=6
VPS_SSH_OPTS="-i $TMP/client_key -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o IdentitiesOnly=yes -o LogLevel=ERROR"
API_URL=http://127.0.0.1:$API_PORT
FTP_HOST=127.0.0.1
FTP_PORT=$FTP_PORT
FTP_USER=deployer
FTP_PASSWORD=s3cret-pass
FTP_REMOTE_DIR=kupol.test/www
FTP_TLS=none
SITE_URL=http://127.0.0.1:$WEB_PORT
EOF
export KUPOL_DEPLOY_ENV="$TMP/env.deploy"
DEPLOY_BACK=(deploy/deploy-back.sh --skip-build)
health_version() { curl -fsS "http://127.0.0.1:$API_PORT/health" | jq -r .status; }

echo
echo "== бэкенд: первая доставка (предыдущей версии нет)"
"${DEPLOY_BACK[@]}"
expect "API отвечает после деплоя" "$(health_version)" ok
expect "бинарник на месте и исполняемый" "$([ -x "$VPS/kupol" ] && echo yes || echo no)" yes
expect "kupol.prev ещё нет" "$([ -e "$VPS/kupol.prev" ] && echo yes || echo no)" no
expect "схема БД мигрирована самим API при старте (версия = числу файлов миграций)" \
  "$(docker compose exec -T postgres psql -U "$KUPOL_DB_USER" -d "$DB" -Atc 'SELECT max(version_id) FROM goose_db_version')" \
  "$(ls backend/internal/database/migrations/*.sql | wc -l | tr -d ' ')"
GOOD_SUM="$(sha256sum "$VPS/kupol" | cut -d' ' -f1)"

echo
echo "== бэкенд: повторный деплой сохраняет предыдущую версию"
"${DEPLOY_BACK[@]}"
expect "kupol.prev создан" "$([ -f "$VPS/kupol.prev" ] && echo yes || echo no)" yes
expect "API отвечает" "$(health_version)" ok

echo
echo "== бэкенд: сломанная версия не стартует -> автоматический откат"
printf '#!/bin/sh\necho "новая версия сломана" >&2\nexit 1\n' >"$TMP/bad-kupol"
chmod +x "$TMP/bad-kupol"
if KUPOL_BIN="$TMP/bad-kupol" "${DEPLOY_BACK[@]}" >"$TMP/bad.out" 2>&1; then
  echo "  FAIL деплой сломанной версии не должен считаться успешным"; FAILS=$((FAILS+1))
else
  echo "  ok   деплой сломанной версии завершился ошибкой"
fi
grep -q "выполнен откат" "$TMP/bad.out" && echo "  ok   скрипт сообщил об откате" || { echo "  FAIL нет сообщения об откате"; cat "$TMP/bad.out"; FAILS=$((FAILS+1)); }
expect "после отката API снова отвечает" "$(health_version)" ok
expect "на месте прежний рабочий бинарник" "$(sha256sum "$VPS/kupol" | cut -d' ' -f1)" "$GOOD_SUM"
expect "сломанный бинарник сохранён как kupol.failed" "$([ -f "$VPS/kupol.failed" ] && echo yes || echo no)" yes

echo
echo "== бэкенд: ручной --rollback"
deploy/deploy-back.sh --rollback
expect "API отвечает после --rollback" "$(health_version)" ok

echo
echo "== бэкенд: ошибки конфигурации"
# Файл настроек с ошибкой в одном поле: остальное как в рабочем.
variant() { sed "s#^$1=.*#$1=$2#" "$TMP/env.deploy" >"$TMP/env.$3"; }
variant VPS_DIR "$TMP/no-such-dir" novps
expect_fail "нет каталога на VPS -> ошибка" env KUPOL_DEPLOY_ENV="$TMP/env.novps" "${DEPLOY_BACK[@]}"
grep -q "нет каталога" "$TMP/last.out" && echo "  ok   понятное сообщение об отсутствии каталога" || { echo "  FAIL сообщение: $(cat "$TMP/last.out")"; FAILS=$((FAILS+1)); }
variant VPS_PORT 1 nossh
expect_fail "SSH недоступен -> ошибка" env KUPOL_DEPLOY_ENV="$TMP/env.nossh" "${DEPLOY_BACK[@]}"
grep -q "не удалось подключиться по SSH" "$TMP/last.out" && echo "  ok   понятное сообщение об ошибке SSH (а не «нет каталога»)" || { echo "  FAIL сообщение: $(cat "$TMP/last.out")"; FAILS=$((FAILS+1)); }
expect_fail "нет файла настроек -> ошибка" env KUPOL_DEPLOY_ENV="$TMP/nope" "${DEPLOY_BACK[@]}"

# ---------------------------------------------------------------- «хостинг»: настоящий FTP + Apache
echo
echo "== FTP-сервер на :$FTP_PORT и Apache на :$WEB_PORT"
python3 -m venv "$TMP/venv" >/dev/null 2>&1
"$TMP/venv/bin/pip" install -q pyftpdlib 2>&1 | grep -v notice || true
FTP_ROOT="$TMP/ftp"
SITE="$FTP_ROOT/kupol.test/www" # как на реальном хостинге: <домен>/www
mkdir -p "$SITE/assets"
"$TMP/venv/bin/python" -m pyftpdlib -i 127.0.0.1 -p "$FTP_PORT" -d "$FTP_ROOT" -u deployer -P s3cret-pass -w >"$TMP/ftp.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 1 50); do (exec 3<>"/dev/tcp/127.0.0.1/$FTP_PORT") 2>/dev/null && break; sleep 0.1; done

printf 'Listen %s\n' "$WEB_PORT" >"$TMP/ports.conf"
cat >"$TMP/vhost.conf" <<EOF
<VirtualHost *:$WEB_PORT>
  DocumentRoot /var/www/html
  <Directory /var/www/html>
    AllowOverride All
    Require all granted
  </Directory>
  ErrorLog /proc/self/fd/2
</VirtualHost>
EOF
chmod -R a+rX "$FTP_ROOT"
docker run -d --rm --name "$CONTAINER" --network host \
  -v "$SITE:/var/www/html:ro" \
  -v "$TMP/ports.conf:/etc/apache2/ports.conf:ro" \
  -v "$TMP/vhost.conf:/etc/apache2/sites-enabled/000-default.conf:ro" \
  php:8.3-apache sh -c 'a2enmod rewrite headers deflate >/dev/null 2>&1 && echo "ServerName localhost" >/etc/apache2/conf-enabled/servername.conf && exec apache2-foreground' >/dev/null

for _ in $(seq 1 100); do curl -sS -o /dev/null "http://127.0.0.1:$WEB_PORT/" 2>/dev/null && break; sleep 0.2; done
curl -sS -o /dev/null "http://127.0.0.1:$WEB_PORT/" 2>/dev/null || { echo "Apache не поднялся"; docker logs "$CONTAINER" 2>&1 | tail -20; exit 1; }

# В каталоге сайта уже лежат устаревшие файлы прошлых деплоев.
echo 'old bundle' >"$SITE/assets/stale-abc123.js"
echo 'old page' >"$SITE/old-page.html"

cat >"$TMP/proxy.config.php" <<EOF
<?php
return [
    'upstream' => 'http://127.0.0.1:$API_PORT',
    'secret'   => '$KUPOL_PROXY_SECRET',
];
EOF

echo
echo "== фронтенд: --upload-config кладёт боевой config.php"
deploy/deploy-front.sh --upload-config "$TMP/proxy.config.php"
expect "config.php на хостинге совпадает с локальным" "$(sha256sum "$SITE/api/config.php" | cut -d' ' -f1)" "$(sha256sum "$TMP/proxy.config.php" | cut -d' ' -f1)"
expect "права на config.php" "$(stat -c %a "$SITE/api/config.php")" 600
CONFIG_SUM="$(sha256sum "$SITE/api/config.php" | cut -d' ' -f1)"
chmod a+r "$SITE/api/config.php"  # Apache в контейнере читает как другой пользователь; на реальном хостинге владелец совпадает

expect_fail "заглушка ЗАМЕНИТЬ_НА_СЕКРЕТ не заливается" bash -c "
  printf '<?php return [\"secret\" => \"ЗАМЕНИТЬ_НА_СЕКРЕТ\"];' >'$TMP/bad.config.php'
  deploy/deploy-front.sh --upload-config '$TMP/bad.config.php'"

echo
echo "== фронтенд: --dry-run показывает, что будет удалено, и ничего не меняет"
deploy/deploy-front.sh --skip-build --dry-run >"$TMP/dry.out" 2>&1 || { echo "  FAIL dry-run упал"; cat "$TMP/dry.out"; FAILS=$((FAILS+1)); }
grep -q "old-page.html" "$TMP/dry.out" && echo "  ok   в плане указан посторонний файл old-page.html" || { echo "  FAIL в плане нет old-page.html"; cat "$TMP/dry.out"; FAILS=$((FAILS+1)); }
expect "index.html не появился" "$([ -e "$SITE/index.html" ] && echo yes || echo no)" no
expect "устаревший файл на месте" "$([ -e "$SITE/assets/stale-abc123.js" ] && echo yes || echo no)" yes
expect "config.php на месте" "$([ -f "$SITE/api/config.php" ] && echo yes || echo no)" yes

echo
echo "== фронтенд: в каталоге чужие файлы, нашей сборки нет -> без --wipe отказ, ничего не удалено"
expect_fail "деплой без --wipe отказывается" deploy/deploy-front.sh --skip-build
grep -q "old-page.html" "$TMP/last.out" && grep -q "УДАЛЕНО" "$TMP/last.out" \
  && echo "  ok   показан список файлов и предупреждение об удалении" \
  || { echo "  FAIL нет списка/предупреждения: $(cat "$TMP/last.out")"; FAILS=$((FAILS+1)); }
expect "чужая страница не тронута" "$([ -e "$SITE/old-page.html" ] && echo yes || echo no)" yes
expect "чужой ассет не тронут" "$([ -e "$SITE/assets/stale-abc123.js" ] && echo yes || echo no)" yes
expect "index.html не залит" "$([ -e "$SITE/index.html" ] && echo yes || echo no)" no

echo
echo "== фронтенд: первый деплой с --wipe (внутри — deploy/check-site.sh по SITE_URL)"
chmod -R a+rX "$FTP_ROOT"
deploy/deploy-front.sh --skip-build --wipe | tee "$TMP/front.out" | sed -n '/== проверка сайта/,$p'
grep -q "ИТОГ: всё в порядке" "$TMP/front.out" && echo "  ok   check-site.sh прошёл на развёрнутом сайте" || { echo "  FAIL check-site.sh не прошёл"; FAILS=$((FAILS+1)); }
expect "устаревший ассет удалён" "$([ -e "$SITE/assets/stale-abc123.js" ] && echo yes || echo no)" no
expect "устаревшая страница удалена" "$([ -e "$SITE/old-page.html" ] && echo yes || echo no)" no
expect "боевой config.php не тронут и не удалён" "$(sha256sum "$SITE/api/config.php" | cut -d' ' -f1)" "$CONFIG_SUM"
DIFF="$(diff -r --exclude=config.php frontend/dist "$SITE" 2>&1 || true)"
expect "содержимое хостинга == frontend/dist (кроме config.php)" "$DIFF" ""
expect ".htaccess доехал (dot-файлы)" "$([ -f "$SITE/.htaccess" ] && [ -f "$SITE/api/.htaccess" ] && echo yes || echo no)" yes

echo
echo "== фронтенд: повторный деплой идемпотентен и уже не требует --wipe (каталог «наш»)"
chmod -R a+rX "$FTP_ROOT"
deploy/deploy-front.sh --skip-build >"$TMP/front2.out" 2>&1 && echo "  ok   второй деплой прошёл" || { echo "  FAIL второй деплой упал"; tail -20 "$TMP/front2.out"; FAILS=$((FAILS+1)); }
expect "config.php по-прежнему на месте" "$(sha256sum "$SITE/api/config.php" | cut -d' ' -f1)" "$CONFIG_SUM"

echo
echo "== фронтенд: защита от ошибок"
mkdir -p "$TMP/keep" && cp -r frontend/dist "$TMP/keep/"
cp "$TMP/proxy.config.php" frontend/dist/api/config.php
expect_fail "config.php в сборке -> деплой отказывается" deploy/deploy-front.sh --skip-build
grep -q "секрет" "$TMP/last.out" && echo "  ok   понятное сообщение про секрет" || { echo "  FAIL сообщение: $(cat "$TMP/last.out")"; FAILS=$((FAILS+1)); }
rm -f frontend/dist/api/config.php
variant FTP_PASSWORD wrong-pass badftp
expect_fail "неверный пароль FTP -> ошибка" env KUPOL_DEPLOY_ENV="$TMP/env.badftp" deploy/deploy-front.sh --skip-build
grep -q "не удалось подключиться к FTP" "$TMP/last.out" \
  && echo "  ok   сбой входа не принят за «каталог пуст»: понятная ошибка подключения" \
  || { echo "  FAIL сбой входа обработан не как ошибка подключения: $(cat "$TMP/last.out")"; FAILS=$((FAILS+1)); }
variant FTP_PORT 1 noftp
expect_fail "FTP недоступен -> ошибка, ничего не удаляется" env KUPOL_DEPLOY_ENV="$TMP/env.noftp" deploy/deploy-front.sh --skip-build --wipe
grep -q "не удалось подключиться к FTP" "$TMP/last.out" && echo "  ok   недоступный FTP: понятная ошибка" || { echo "  FAIL: $(cat "$TMP/last.out")"; FAILS=$((FAILS+1)); }
grep -q "wrong-pass" "$TMP/last.out" && { echo "  FAIL пароль попал в вывод"; FAILS=$((FAILS+1)); } || echo "  ok   пароль не печатается"

echo
if [ "$FAILS" -ne 0 ]; then echo "test-deploy: упало $FAILS"; exit 1; fi
echo "test-deploy: всё прошло"

#!/bin/bash
# Тест deploy/bin/runtime.sh: проверка заявок, файлы пулов PHP-FPM и служб приложений, очистка, права.
# systemctl, systemd-run, useradd и прочие системные команды подменены заглушками (они записывают вызовы), поэтому сама песочница
# и настоящий PHP-FPM здесь не проверяются — их проверяет prepare-runtime.sh на сервере. Запуск: bash deploy/test/runtime_test.sh
set -u
cd "$(dirname "$0")/../.." || exit 1
SCRIPT=$PWD/deploy/bin/runtime.sh
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "ПРОВАЛ: $*" >&2; }
check() { local d=$1; shift; if "$@" >/dev/null 2>&1; then ok; else bad "$d"; fi; }

export VH_RT_DIR=$T/rt VH_RT_SITES=$T/sites VH_RT_PHP_ETC=$T/php VH_RT_UNIT_DIR=$T/units VH_RT_APPS_DIR=$T/apps VH_RT_SOCK_DIR=$T/run
export VH_RT_SYSTEMCTL=$T/bin/systemctl VH_RT_SYSTEMD_RUN=$T/bin/systemd-run VH_RT_JOURNALCTL=$T/bin/journalctl VH_RT_SETPRIV=$T/bin/setpriv
export VH_RT_PHPFPM_TEST=$T/bin/phpfpmtest VH_RT_CHOWN=$T/bin/chown VH_RT_SETTLE=0
export CALLS=$T/calls.log ACTIVE=$T/active
mkdir -p "$T/bin" "$T/units" "$T/php/8.2/fpm/pool.d" "$T/php/8.3/fpm/pool.d" "$T/php/8.4/fpm/pool.d"
: >"$CALLS"
: >"$T/passwd"
: >"$T/group"

stub() { printf '#!/bin/bash\n%s\n' "$2" >"$T/bin/$1"; chmod +x "$T/bin/$1"; }
stub systemctl 'echo "systemctl $*" >>"$CALLS"
if [ "$1" = is-active ]; then
    shift; [ "$1" = --quiet ] && shift
    st=$(cat "$ACTIVE/$1" 2>/dev/null || echo inactive); echo "$st"; [ "$st" = active ] || exit 3
fi
exit 0'
stub chown 'echo "chown $*" >>"$CALLS"'
stub phpfpmtest 'echo "phpfpm-t $*" >>"$CALLS"; [ -e "$PHPFPM_BROKEN" ] && { echo "ERROR: bad pool" >&2; exit 1; }; exit 0'
stub groupadd 'echo "groupadd $*" >>"$CALLS"; echo "${@: -1}" >>"$T_GROUP"'
stub useradd 'echo "useradd $*" >>"$CALLS"; echo "${@: -1}" >>"$T_PASSWD"'
stub userdel 'echo "userdel $*" >>"$CALLS"; grep -vx "$1" "$T_PASSWD" >"$T_PASSWD.n"; mv "$T_PASSWD.n" "$T_PASSWD"'
stub groupdel 'echo "groupdel $*" >>"$CALLS"; grep -vx "$1" "$T_GROUP" >"$T_GROUP.n"; mv "$T_GROUP.n" "$T_GROUP"'
stub getent 'case $1 in passwd) grep -qx "$2" "$T_PASSWD" ;; group) grep -qx "$2" "$T_GROUP" ;; esac'
stub setfacl 'echo "setfacl $*" >>"$CALLS"'
stub getfacl 'exit 1'
stub journalctl 'echo "journal $*" >>"$CALLS"; echo "app output line"'
stub setpriv 'echo "setpriv $*" >>"$CALLS"; shift 3; exec "$@"'
# systemd-run: записывает вызов и выполняет команду из песочницы, подставляя настоящий путь вместо /data/vladhost/site.
stub systemd-run 'echo "systemd-run $*" >>"$CALLS"
src=""; args=("$@")
for a in "$@"; do case $a in BindPaths=*) src=${a#BindPaths=}; src=${src%%:*} ;; esac; done
while [ "${1:-}" != "--" ]; do shift; done; shift
script=${*: -1}
script=${script//\/data\/vladhost\/site/$src}
exec /bin/bash -c "$script"'
export T_PASSWD=$T/passwd T_GROUP=$T/group PHPFPM_BROKEN=$T/broken
export PATH=$T/bin:$PATH
mkdir -p "$ACTIVE"

mksite() { mkdir -p "$T/sites/$1/public"; echo "hello" >"$T/sites/$1/public/index.php"; }
H=blog.vlad.vladinc.ru
mksite $H

request() { # rid поля… (key=value)
    local rid=$1
    shift
    printf '%s\n' "$@" >"$VH_RT_DIR/queue/.$rid.tmp"
    mv "$VH_RT_DIR/queue/.$rid.tmp" "$VH_RT_DIR/queue/$rid.req"
}
b64() { printf '%s' "$1" | base64 -w0; }
run() { bash "$SCRIPT"; }
res() { sed -n "s/^$2=//p" "$VH_RT_DIR/results/$1.res"; }
calls() { cat "$CALLS"; }
reset() { : >"$CALLS"; }
has() { grep -qF -- "$1" "$2"; }

mkdir -p "$VH_RT_DIR/queue" "$VH_RT_DIR/results"
POOL=$T/php/8.3/fpm/pool.d/vhs7.conf
UNIT=$T/units/vhapp-7.service

echo "== apply PHP"
request a1 action=apply host=$H id=7 runtime=php version=8.3 port=0 cmd=
run
check "ответ: успех" test "$(res a1 ok)" = 1
check "состояние есть" test -n "$(res a1 state)"
check "пул создан" test -f "$POOL"
check "пользователь пула" has "user = vhs7" "$POOL"
check "сокет пула" has "listen = $T/run/vhs7.sock" "$POOL"
check "сокет доступен шлюзу" has "listen.owner = vladhost-web" "$POOL"
check "open_basedir только папка сайта" has "open_basedir] = $T/sites/$H/public:$T/sites/$H/tmp:/usr/share/php" "$POOL"
check "журнал ошибок в tmp сайта" has "error_log] = $T/sites/$H/tmp/php-error.log" "$POOL"
check "опасные функции выключены" has "disable_functions] = exec,passthru,shell_exec,system,proc_open,popen" "$POOL"
check "память ограничена" has "memory_limit] = 192M" "$POOL"
check "пул ondemand" has "pm = ondemand" "$POOL"
check "конфигурация проверена" has "phpfpm-t 8.3" "$CALLS"
check "PHP-FPM перечитан" has "systemctl reload php8.3-fpm" "$CALLS"
check "пользователь создан" has "useradd -u 60007 -g vhs7" "$CALLS"
check "tmp создан" test -d "$T/sites/$H/tmp"
check "tmp отдан пользователю сайта" has "chown vhs7:vhs7 $T/sites/$H/tmp" "$CALLS"
check "права tmp 0700" test "$(stat -c %a "$T/sites/$H/tmp")" = 700
check "сайт записан в список" test "$(cat "$VH_RT_DIR/sites/7")" = "$H"
check "права ACL заданы" has "u:vladhost:rwX,u:vladhost-web:rX,u:vhs7:rwX,m::rwX,o::---" "$CALLS"
check "ACL по умолчанию для новых файлов" has "setfacl -R -d -m" "$CALLS"
check "папка сайта нужна только как public" has "BindPaths=$T/sites/$H/public:/data/vladhost/site" "$CALLS"
check "песочница без сети" has "PrivateNetwork=yes" "$CALLS"
check "песочница с ограниченными возможностями" has "CapabilityBoundingSet=CAP_FOWNER CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN" "$CALLS"
check "ключевые файлы вне песочницы: настройки не в public" test ! -e "$T/sites/$H/public/runtime.json"

echo "== смена версии PHP убирает старый пул"
request a2 action=apply host=$H id=7 runtime=php version=8.4 port=0 cmd=
reset
run
check "пул 8.3 убран" test ! -f "$POOL"
check "пул 8.4 создан" test -f "$T/php/8.4/fpm/pool.d/vhs7.conf"
check "8.3 перечитан (пул исчез)" has "systemctl reload php8.3-fpm" "$CALLS"
check "8.4 перечитан" has "systemctl reload php8.4-fpm" "$CALLS"

echo "== сломанный пул откатывается"
touch "$PHPFPM_BROKEN"
request a3 action=apply host=$H id=7 runtime=php version=8.2 port=0 cmd=
run
rm -f "$PHPFPM_BROKEN"
check "ответ: ошибка pool" test "$(res a3 error)" = pool
check "сломанный пул удалён" test ! -f "$T/php/8.2/fpm/pool.d/vhs7.conf"
check "текст ошибки в выводе" grep -q "bad pool" "$VH_RT_DIR/results/a3.out"

echo "== apply Node.js"
request b1 action=apply host=$H id=7 runtime=node version= port=20123 "cmd=$(b64 'node server.js --flag "a b"')"
echo active >"$ACTIVE/vhapp-7.service"
reset
run
check "успех" test "$(res b1 ok)" = 1
check "состояние active" test "$(res b1 state)" = active
check "служба создана" test -f "$UNIT"
check "пользователь службы" has "User=vhs7" "$UNIT"
check "порт в окружении" has "Environment=PORT=20123" "$UNIT"
check "слушаем только loopback" has "Environment=HOST=127.0.0.1" "$UNIT"
check "песочница: ProtectSystem" has "ProtectSystem=strict" "$UNIT"
check "песочница: чужие сайты скрыты" has "TemporaryFileSystem=/data/vladhost:mode=0755,size=1m,nosuid,nodev" "$UNIT"
check "песочница: секреты панели закрыты" has "InaccessiblePaths=-/etc/vladhost -/var/lib/vladhost" "$UNIT"
check "песочница: только свой public и tmp" has "BindPaths=$T/sites/$H/public:/data/vladhost/site/public $T/sites/$H/tmp:/data/vladhost/site/tmp" "$UNIT"
check "лимит памяти" has "MemoryMax=256M" "$UNIT"
check "лимит процессов" has "TasksMax=64" "$UNIT"
check "перезапуск при сбое" has "Restart=on-failure" "$UNIT"
check "команда в файле root" has 'node server.js --flag "a b"' "$T/apps/7.sh"
check "команда запускается в папке сайта" has "cd /data/vladhost/site/public" "$T/apps/7.sh"
check "команда не попала в файл службы" bash -c "! grep -q 'node server' '$UNIT'"
check "пул PHP убран" test ! -f "$T/php/8.4/fpm/pool.d/vhs7.conf"
check "служба включена и запущена" has "systemctl restart vhapp-7.service" "$CALLS"
check "автозапуск" has "systemctl enable vhapp-7.service" "$CALLS"

echo "== состояния"
for st in failed inactive; do
    echo $st >"$ACTIVE/vhapp-7.service"
    request s$st action=status host=$H id=7 runtime=node version= port=20123 cmd=
    run
    check "status $st" test "$(res s$st state)" = $st
done
echo active >"$ACTIVE/php8.4-fpm"
request s3 action=status host=$H id=7 runtime=php version=8.4 port=0 cmd=
touch "$T/php/8.4/fpm/pool.d/vhs7.conf"
run
check "status php" test "$(res s3 state)" = active
rm -f "$T/php/8.4/fpm/pool.d/vhs7.conf"

echo "== журналы"
request l1 action=logs host=$H id=7 runtime=node version= port=0 cmd=
run
check "журнал приложения" grep -q "app output line" "$VH_RT_DIR/results/l1.out"
check "журнал по службе" has "journal -u vhapp-7.service" "$CALLS"
printf 'PHP Warning: boom\n' >"$T/sites/$H/tmp/php-error.log"
request l2 action=logs host=$H id=7 runtime=php version=8.3 port=0 cmd=
reset
run
check "журнал PHP" grep -q "PHP Warning: boom" "$VH_RT_DIR/results/l2.out"
check "журнал PHP читается от имени пользователя сайта" has "setpriv --reuid=60007 --regid=60007 --clear-groups" "$CALLS"

echo "== перезапуск"
echo active >"$ACTIVE/vhapp-7.service"
request r1 action=restart host=$H id=7 runtime=node version= port=0 cmd=
reset
run
check "перезапуск приложения" has "systemctl restart vhapp-7.service" "$CALLS"

echo "== Node.js → PHP: служба убирается"
request c1 action=apply host=$H id=7 runtime=php version=8.3 port=0 cmd=
reset
run
check "служба удалена" test ! -f "$UNIT"
check "файл команды удалён" test ! -f "$T/apps/7.sh"
check "служба остановлена" has "systemctl disable --now vhapp-7.service" "$CALLS"
check "пул PHP создан" test -f "$POOL"

echo "== отказы"
mkdir -p "$T/outside" "$T/sites/link.vlad.vladinc.ru"
ln -s "$T/outside" "$T/sites/link.vlad.vladinc.ru/public"
bigcmd=$(head -c 600 /dev/zero | tr '\0' a)
request e1 action=apply host=../etc id=7 runtime=php version=8.3 port=0 cmd=
request e2 action=apply host=$H id=0 runtime=php version=8.3 port=0 cmd=
request e3 action=apply host=$H id=10000 runtime=php version=8.3 port=0 cmd=
request e4 action=format host=$H id=7 runtime=php version=8.3 port=0 cmd=
request e5 action=apply host=$H id=7 runtime=node version= port=80 "cmd=$(b64 'node a.js')"
request e6 action=apply host=$H id=7 runtime=php version=8.3x port=0 cmd=
request e7 action=apply host=$H id=7 runtime=node version= port=20001 "cmd=$(b64 $'node a.js\nrm -rf /')"
request e8 action=apply host=$H id=7 runtime=node version= port=20001 "cmd=$(b64 "$bigcmd")"
request e9 action=apply host=nosuch.vlad.vladinc.ru id=8 runtime=php version=8.3 port=0 cmd=
request e10 action=apply host=link.vlad.vladinc.ru id=9 runtime=php version=8.3 port=0 cmd=
request e11 action=apply host=$H id=7 runtime=php version=7.4 port=0 cmd=
request e12 action=apply host=$H id=7 runtime=ruby version= port=0 cmd=
request e13 action=apply host=$H id=7 runtime=node version= port=20001 "cmd=!!!не base64!!!"
request e14 action=apply host=other.vlad.vladinc.ru id=7 runtime=php version=8.3 port=0 cmd=
mksite other.vlad.vladinc.ru
reset
run
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14; do
    check "заявка e$i отклонена" test "$(res e$i ok)" = 0
done
check "e1: host" test "$(res e1 error)" = host
check "e2: id" test "$(res e2 error)" = id
check "e5: port" test "$(res e5 error)" = port
check "e7: command" test "$(res e7 error)" = command
check "e8: command" test "$(res e8 error)" = command
check "e9: site_not_found" test "$(res e9 error)" = site_not_found
check "e10: public-ссылка не годится" test "$(res e10 error)" = site_not_found
check "e11: версия не установлена" test "$(res e11 error)" = version
check "e14: номер занят другим сайтом" test "$(res e14 error)" = site_mismatch
check "после отказов пользователей не создавали" bash -c "! grep -q 'useradd' '$CALLS'"

echo "== подмена заявки ссылкой и мусор"
echo "action=purge" >"$T/decoy"
ln -s "$T/decoy" "$VH_RT_DIR/queue/zz1.req"
: >"$VH_RT_DIR/queue/junk.txt"
touch -d '5 minutes ago' "$VH_RT_DIR/queue/junk.txt"
run
check "ссылка вместо заявки отклонена" test "$(res zz1 error)" = request
check "мусор убран" test ! -e "$VH_RT_DIR/queue/junk.txt"

echo "== права всех сайтов"
mksite second.vlad.vladinc.ru
echo second.vlad.vladinc.ru >"$VH_RT_DIR/sites/8"
echo '../etc' >"$VH_RT_DIR/sites/9"
echo 'gone.vlad.vladinc.ru' >"$VH_RT_DIR/sites/10"
reset
bash "$SCRIPT" perms-all
check "права выданы сайту 7" has "u:vhs7:rwX" "$CALLS"
check "права выданы сайту 8" has "u:vhs8:rwX" "$CALLS"
check "неверный адрес пропущен" bash -c "! grep -q 'u:vhs9:' '$CALLS'"
check "исчезнувший сайт пропущен" bash -c "! grep -q 'u:vhs10:' '$CALLS'"
rm -f "$VH_RT_DIR/sites/8" "$VH_RT_DIR/sites/9" "$VH_RT_DIR/sites/10"

echo "== stop"
request t1 action=stop host=$H id=7 runtime=php version=8.3 port=0 cmd=
reset
run
check "пул убран" test ! -f "$POOL"
check "PHP-FPM перечитан" has "systemctl reload php8.3-fpm" "$CALLS"
check "ACL сняты (сайт снова статика)" has "setfacl -R -b" "$CALLS"
check "сайт убран из списка" test ! -e "$VH_RT_DIR/sites/7"
check "пользователь остаётся" bash -c "! grep -q 'userdel' '$CALLS'"

echo "== purge"
request p1 action=apply host=$H id=7 runtime=node version= port=20123 "cmd=$(b64 'node a.js')"
run
request p2 action=purge host=$H id=7 runtime=node version= port=20123 cmd=
reset
run
check "служба убрана" test ! -f "$UNIT"
check "пользователь удалён" has "userdel vhs7" "$CALLS"
check "группа удалена" has "groupdel vhs7" "$CALLS"
check "tmp удалён" test ! -e "$T/sites/$H/tmp"
check "файлы сайта на месте (их удаляет панель)" test -f "$T/sites/$H/public/index.php"
check "сайт убран из списка" test ! -e "$VH_RT_DIR/sites/7"

echo "== доступ к оболочке (shell-on / shell-off)"
SH=shelly.vlad.vladinc.ru
mksite $SH
request sh1 action=shell-on host=$SH id=12 runtime=static version= port=0 cmd=
reset
run
check "shell-on: успех" test "$(res sh1 ok)" = 1
check "shell-on: пользователь создан" has "useradd -u 60012 -g vhs12" "$CALLS"
check "shell-on: tmp создан и отдан пользователю" has "chown vhs12:vhs12 $T/sites/$SH/tmp" "$CALLS"
check "shell-on: права ACL выданы" has "u:vhs12:rwX" "$CALLS"
check "shell-on: метка для посредника содержит адрес сайта" test "$(cat "$VH_RT_DIR/shell/12")" = "$SH"
check "shell-on: сайт в списке (права ведутся таймером)" test "$(cat "$VH_RT_DIR/sites/12")" = "$SH"
check "shell-on: пул PHP не создан" bash -c "! ls $T/php/*/fpm/pool.d/vhs12.conf"
check "shell-on: службы приложения нет" test ! -e "$T/units/vhapp-12.service"

request sh2b action=shell-on host=$SH id=12 runtime= version= port=0 cmd=
run
check "shell-on без runtime в заявке (так шлёт панель)" test "$(res sh2b ok)" = 1
request sh2 action=shell-on host=$SH id=12 runtime=static version= port=0 cmd=
reset
run
check "shell-on повторно: успех и без нового пользователя" bash -c "[ \"$(res sh2 ok)\" = 1 ] && ! grep -q useradd '$CALLS'"

request sh3 action=shell-on host=other.vlad.vladinc.ru id=12 runtime=static version= port=0 cmd=
run
check "shell-on: номер занят другим сайтом" test "$(res sh3 error)" = site_mismatch
request sh4 action=shell-on host=nosuch.vlad.vladinc.ru id=13 runtime=static version= port=0 cmd=
run
check "shell-on: нет папки сайта" test "$(res sh4 error)" = site_not_found
check "shell-on: метка не создана при отказе" test ! -e "$VH_RT_DIR/shell/13"

request sh5 action=shell-off host=$SH id=12 runtime=static version= port=0 cmd=
reset
run
check "shell-off: метка снята" test ! -e "$VH_RT_DIR/shell/12"
check "shell-off без среды: права сняты (сайт снова статика)" has "setfacl -R -b" "$CALLS"
check "shell-off без среды: сайт убран из списка" test ! -e "$VH_RT_DIR/sites/12"
check "shell-off: пользователь остаётся до удаления сайта" bash -c "! grep -q userdel '$CALLS'"

# Сайт со средой выполнения: выключение оболочки права не трогает
request sh6 action=apply host=$SH id=12 runtime=php version=8.3 port=0 cmd=
run
request sh7 action=shell-on host=$SH id=12 runtime=static version= port=0 cmd=
run
request sh8 action=shell-off host=$SH id=12 runtime=static version= port=0 cmd=
reset
run
check "shell-off при PHP: права остаются" bash -c "! grep -q 'setfacl -R -b' '$CALLS'"
check "shell-off при PHP: сайт остаётся в списке" test -e "$VH_RT_DIR/sites/12"
# Возврат к статике при включённой оболочке: права остаются
request shk1 action=shell-on host=$SH id=12 runtime=static version= port=0 cmd=
run
request shk2 action=stop host=$SH id=12 runtime=php version=8.3 port=0 cmd=
reset
run
check "stop при включённой оболочке: пул убран" test ! -f "$T/php/8.3/fpm/pool.d/vhs12.conf"
check "stop при включённой оболочке: права остаются" bash -c "! grep -q 'setfacl -R -b' '$CALLS'"
check "stop при включённой оболочке: сайт остаётся в списке" test -e "$VH_RT_DIR/sites/12"
check "stop при включённой оболочке: метка на месте" test -e "$VH_RT_DIR/shell/12"
request sh11 action=purge host=$SH id=12 runtime=static version= port=0 cmd=
reset
run
check "purge: метка удалена" test ! -e "$VH_RT_DIR/shell/12"
check "purge: пользователь удалён" has "userdel vhs12" "$CALLS"
check "purge: tmp удалён" test ! -e "$T/sites/$SH/tmp"

echo "== почта (mail-sync)"
mkdir -p "$VH_RT_DIR/mail"
cat >"$T/bin/mailsync" <<'STUB'
#!/bin/bash
echo "mailsync $*" >>"$CALLS"
case $(cat "$MAIL_MODE" 2>/dev/null) in bad) echo "error=domain_name" ;; *) echo ok ;; esac
STUB
chmod +x "$T/bin/mailsync"
export VH_RT_MAIL_SYNC=$T/bin/mailsync MAIL_MODE=$T/mailmode
# скрипт запускается через python3; в тесте подменяем python3 заглушкой, которая просто выполняет наш скрипт
cat >"$T/bin/python3" <<'STUB'
#!/bin/bash
exec "$@"
STUB
chmod +x "$T/bin/python3"
request m1 action=mail-sync
reset
run
check "mail-sync без состояния: отказ no_state" test "$(res m1 error)" = no_state
echo '{"version":1,"domains":[]}' >"$VH_RT_DIR/mail/state.json"
request m2 action=mail-sync
reset
run
check "mail-sync: успех" test "$(res m2 ok)" = 1
check "mail-sync: скрипту передан файл состояния" has "--state $VH_RT_DIR/mail/state.json" "$CALLS"
echo bad >"$MAIL_MODE"
request m3 action=mail-sync
run
check "mail-sync: код ошибки скрипта попадает в ответ" test "$(res m3 error)" = domain_name
rm -f "$MAIL_MODE" "$VH_RT_DIR/mail/state.json"
ln -s /etc/passwd "$VH_RT_DIR/mail/state.json"
request m4 action=mail-sync
run
check "mail-sync: ссылка вместо файла состояния отвергается" test "$(res m4 error)" = no_state
rm -f "$VH_RT_DIR/mail/state.json"

echo "== почта (mail-log)"
cat >"$T/bin/maillog" <<'STUB'
#!/bin/bash
echo "maillog $*" >>"$CALLS"
case $(cat "$MAIL_MODE" 2>/dev/null) in bad) echo "error=bad_args"; exit 2 ;; *) echo '{"events":[{"kind":"received"}],"queue":[]}' ;; esac
STUB
chmod +x "$T/bin/maillog"
export VH_RT_MAIL_LOG=$T/bin/maillog
request l1 action=mail-log host=example.com id=50
reset
run
check "mail-log: успех" test "$(res l1 ok)" = 1
check "mail-log: домен и число событий переданы скрипту" has "--domain example.com --limit 50" "$CALLS"
check "mail-log: ответ лежит в .out" grep -q '"events"' "$VH_RT_DIR/results/l1.out"
for bad in "host=Example.com id=50" "host=a..com id=50" "host=x;rm.com id=50" "host=localhost id=50" "host=example.com id=0" "host=example.com id=121" "host=example.com id=abc" "host=example.com"; do
    request l2 action=mail-log $bad
    reset
    run
    check "mail-log: отказ для «$bad»" test "$(res l2 ok)" = 0
    check "mail-log: скрипт для «$bad» не запускался" test ! -s "$CALLS"
done
echo bad >"$MAIL_MODE"
request l3 action=mail-log host=example.com id=10
run
check "mail-log: код ошибки скрипта попадает в ответ" test "$(res l3 error)" = bad_args
rm -f "$MAIL_MODE"

echo "== DNS (dns-sync)"
mkdir -p "$VH_RT_DIR/dns"
cat >"$T/bin/dnssync" <<'STUB'
#!/bin/bash
echo "dnssync $*" >>"$CALLS"
case $(cat "$DNS_MODE" 2>/dev/null) in bad) echo "error=record_ttl" ;; *) echo ok ;; esac
STUB
chmod +x "$T/bin/dnssync"
export VH_RT_DNS_SYNC=$T/bin/dnssync DNS_MODE=$T/dnsmode
request d1 action=dns-sync
reset
run
check "dns-sync без состояния: отказ no_state" test "$(res d1 error)" = no_state
echo '{"version":1}' >"$VH_RT_DIR/dns/state.json"
request d2 action=dns-sync
reset
run
check "dns-sync: успех" test "$(res d2 ok)" = 1
check "dns-sync: скрипту передан файл состояния" has "--state $VH_RT_DIR/dns/state.json" "$CALLS"
echo bad >"$DNS_MODE"
request d3 action=dns-sync
run
check "dns-sync: код ошибки скрипта попадает в ответ" test "$(res d3 error)" = record_ttl
rm -f "$DNS_MODE" "$VH_RT_DIR/dns/state.json"
ln -s /etc/passwd "$VH_RT_DIR/dns/state.json"
request d4 action=dns-sync
run
check "dns-sync: ссылка вместо файла состояния отвергается" test "$(res d4 error)" = no_state
rm -f "$VH_RT_DIR/dns/state.json"

echo
echo "успешно: $pass, провалено: $fail"
[ "$fail" -eq 0 ]

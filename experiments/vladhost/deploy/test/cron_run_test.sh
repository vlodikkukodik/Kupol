#!/bin/bash
# Тест deploy/bin/cron-run.sh: очередь заявок, проверки, результат и набор ограничений песочницы.
# systemd-run подменён заглушкой, которая запоминает свойства и выполняет команду как есть, поэтому сама песочница здесь
# не проверяется — её на сервере проверяет prepare-cron.sh. Запуск: bash deploy/test/cron_run_test.sh
set -u
cd "$(dirname "$0")/../.." || exit 1
SCRIPT=$PWD/deploy/bin/cron-run.sh
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "ПРОВАЛ: $*" >&2; }
check() { local d=$1; shift; if "$@" >/dev/null 2>&1; then ok; else bad "$d"; fi; }

export VH_CRON_QUEUE=$T/queue VH_CRON_WORK=$T/work VH_CRON_RESULTS=$T/results VH_CRON_SITES=$T/sites
export VH_CRON_SYSTEMD_RUN=$T/systemd-run VH_CRON_USER=vladhost
mkdir -p "$VH_CRON_QUEUE" "$VH_CRON_RESULTS" "$VH_CRON_SITES/blog.vlad.vladinc.ru/public" "$T/secret"
echo "тайна" >"$T/secret/file"
ln -s "$T/secret" "$VH_CRON_SITES/evil.vlad.vladinc.ru"
mkdir -p "$VH_CRON_SITES/link.vlad.vladinc.ru" && ln -s "$T/secret" "$VH_CRON_SITES/link.vlad.vladinc.ru/public"

# Заглушка: сохраняет аргументы, переходит в папку из BindPaths и выполняет команду после «--».
cat >"$T/systemd-run" <<'STUB'
#!/bin/bash
args=("$@")
dir=$PWD
echo "$*" >>"$VH_CRON_TEST_LOG"
for a in "$@"; do case $a in BindPaths=*) dir=${a#BindPaths=}; dir=${dir%%:*} ;; esac; done
while [ "${1:-}" != "--" ]; do shift; done
shift
cd "$dir" && exec "$@"
STUB
chmod +x "$T/systemd-run"
export VH_CRON_TEST_LOG=$T/calls.log
: >"$VH_CRON_TEST_LOG"

# request <id> <host> <timeout> <команда>
request() {
    printf 'host=%s\ntimeout=%s\ncmd=%s\n' "$2" "$3" "$(printf '%s' "$4" | base64 -w0)" >"$VH_CRON_QUEUE/.$1.tmp"
    mv "$VH_CRON_QUEUE/.$1.tmp" "$VH_CRON_QUEUE/$1.req"
}
res() { sed -n "s/^$2=//p" "$VH_CRON_RESULTS/$1.res"; }
run() { bash "$SCRIPT"; }

echo "== обычная команда"
request 1 blog.vlad.vladinc.ru 10 'echo привет; pwd; exit 3'
run
check "результат записан" test -f "$VH_CRON_RESULTS/1.res"
check "код выхода 3" test "$(res 1 exit)" = 3
check "не таймаут" test "$(res 1 timeout)" = 0
check "вывод содержит текст" grep -q "привет" "$VH_CRON_RESULTS/1.out"
check "команда шла в папке сайта" grep -q "blog.vlad.vladinc.ru/public" "$VH_CRON_RESULTS/1.out"
check "заявка забрана из очереди" test -z "$(ls -A "$VH_CRON_QUEUE")"
check "рабочая папка пуста" test -z "$(ls -A "$VH_CRON_WORK")"

echo "== ограничения песочницы попали в вызов"
log=$(cat "$VH_CRON_TEST_LOG")
for want in "User=vladhost" "NoNewPrivileges=yes" "ProtectSystem=strict" "ProtectProc=invisible" "PrivateTmp=yes" "InaccessiblePaths=-/etc/vladhost" \
    "TemporaryFileSystem=/data/vladhost" "BindPaths=$VH_CRON_SITES/blog.vlad.vladinc.ru/public:/data/vladhost/site" "MemoryMax=256M" \
    "TasksMax=64" "CPUQuota=50%" "IPAddressDeny=127.0.0.0/8" "RuntimeMaxSec=20" "timeout -k 3 10 /bin/bash -c"; do
    case $log in *"$want"*) ok ;; *) bad "в вызове systemd-run нет: $want" ;; esac
done

echo "== таймаут"
request 2 blog.vlad.vladinc.ru 1 'sleep 5; echo не дошли'
run
check "признак таймаута" test "$(res 2 timeout)" = 1
check "код 124" test "$(res 2 exit)" = 124
check "после таймаута вывода нет" test ! -s "$VH_CRON_RESULTS/2.out"

echo "== отказы"
request 3 '../etc' 10 'true'
request 4 nosuch.vlad.vladinc.ru 10 'true'
request 5 evil.vlad.vladinc.ru 10 'cat file'
request 6 link.vlad.vladinc.ru 10 'cat file'
request 7 blog.vlad.vladinc.ru 0 'true'
request 8 blog.vlad.vladinc.ru 301 'true'
request 9 blog.vlad.vladinc.ru abc 'true'
printf 'host=blog.vlad.vladinc.ru\ntimeout=5\ncmd=!!!не base64!!!\n' >"$VH_CRON_QUEUE/10.req"
run
for i in 3 4 5 6 7 8 9 10; do
    check "заявка $i отклонена с ошибкой" grep -q '^error=' "$VH_CRON_RESULTS/$i.res"
    check "заявка $i: код 1" test "$(res "$i" exit)" = 1
done
check "чужая папка не открылась" test ! -s "$VH_CRON_RESULTS/5.out"
check "ссылка на /public не пройдена" test ! -s "$VH_CRON_RESULTS/6.out"

echo "== подмена заявки ссылкой"
echo "host=blog.vlad.vladinc.ru" >"$T/decoy"
ln -s "$T/decoy" "$VH_CRON_QUEUE/11.req"
run
check "ссылка вместо заявки отклонена" grep -q '^error=invalid request' "$VH_CRON_RESULTS/11.res"
check "ссылка не осталась в очереди" test -z "$(ls -A "$VH_CRON_QUEUE")"

echo "== большой вывод обрезается"
request 12 blog.vlad.vladinc.ru 20 'head -c 200000 /dev/zero | tr "\0" x'
run
size=$(stat -c %s "$VH_CRON_RESULTS/12.out")
check "вывод не больше 32 КБ (получилось $size)" test "$size" -le 32768 -a "$size" -gt 30000

echo "== несколько заявок сразу, параллельно"
for i in 21 22 23 24 25 26; do request $i blog.vlad.vladinc.ru 10 "sleep 1; echo $i"; done
start=$(date +%s)
VH_CRON_PARALLEL=3 run
took=$(($(date +%s) - start))
for i in 21 22 23 24 25 26; do check "заявка $i выполнена" grep -qx "$i" "$VH_CRON_RESULTS/$i.out"; done
check "по три одновременно: 6 заявок по секунде за 2–4 с (получилось $took)" test "$took" -ge 2 -a "$took" -le 4

echo "== мусор в очереди и порядок"
: >"$VH_CRON_QUEUE/junk.txt"
touch -d '5 minutes ago' "$VH_CRON_QUEUE/junk.txt"
: >"$VH_CRON_QUEUE/.fresh.tmp"
run
check "старый мусор убран" test ! -e "$VH_CRON_QUEUE/junk.txt"
check "свежий недописанный файл не тронут" test -e "$VH_CRON_QUEUE/.fresh.tmp"
touch -d '30 minutes ago' "$VH_CRON_RESULTS/1.res" "$VH_CRON_RESULTS/1.out"
run
check "старые результаты убраны" test ! -e "$VH_CRON_RESULTS/1.res"
check "свежие результаты остались" test -e "$VH_CRON_RESULTS/12.res"

echo
echo "успешно: $pass, провалено: $fail"
[ "$fail" -eq 0 ]

#!/bin/bash
# Исполнитель сред выполнения сайтов (root): PHP-FPM, приложения Node.js и Python, пользователи и права, доступ к оболочке,
# а также обмен с почтовым сервером и DNS. Панель работает без прав и сама ничего не настраивает, а кладёт заявку файлом
# в queue/{rid}.req (ключ=значение по строке: action, host, id, runtime, version, port, cmd в base64) и ждёт results/{rid}.res
# (ok=1|0, error=код, state=…) и results/{rid}.out (журнал или подробности). Запускается vladhost-runtime.path по появлению заявки;
# «runtime.sh perms-all» раз в 15 секунд запускает vladhost-runtime-perms.timer.
# Заявка не доверенная: значения проходят строгие шаблоны, а всё, что делается над файлами пользователя, идёт в песочнице systemd-run
# (видна только папка сайта, нет сети, урезанные возможности): root не идёт по ссылкам, которые пользователь мог подложить.
set -uo pipefail

RT=${VH_RT_DIR:-/var/lib/vladhost/runtime}
SITES=${VH_RT_SITES:-/data/vladhost/sites}
PHP_ETC=${VH_RT_PHP_ETC:-/etc/php}
UNITS=${VH_RT_UNIT_DIR:-/etc/systemd/system}
APPS=${VH_RT_APPS_DIR:-/usr/local/lib/vladhost/apps}
SOCK=${VH_RT_SOCK_DIR:-/run/vhphp}
SYSTEMCTL=${VH_RT_SYSTEMCTL:-systemctl}
SYSTEMD_RUN=${VH_RT_SYSTEMD_RUN:-systemd-run}
JOURNALCTL=${VH_RT_JOURNALCTL:-journalctl}
SETPRIV=${VH_RT_SETPRIV:-setpriv}
CHOWN=${VH_RT_CHOWN:-chown}
SETTLE=${VH_RT_SETTLE:-2}
MAIL_SYNC=${VH_RT_MAIL_SYNC:-/usr/local/lib/vladhost/mail-sync.py}
MAIL_LOG=${VH_RT_MAIL_LOG:-/usr/local/lib/vladhost/mail-log.py}
DNS_SYNC=${VH_RT_DNS_SYNC:-/usr/local/lib/vladhost/dns-sync.py}

UID_BASE=60000
HOST_RE='^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?){2,3}$'
DOMAIN_RE='^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$'
MAX_CMD=500
MAX_OUT=65536

mkdir -p "$RT/results" "$RT/sites" "$RT/shell"

# ---------- ответ ----------

# reply rid ok [ошибка] [состояние] [вывод] — .out пишется раньше .res: панель берёт вывод сразу, как увидит .res.
reply() {
    local rid=$1 tmp
    tmp=$(mktemp "$RT/results/.out.XXXXXX") || return
    printf '%s' "${5:-}" | head -c "$MAX_OUT" >"$tmp"
    chmod 0644 "$tmp"
    mv -f "$tmp" "$RT/results/$rid.out"
    tmp=$(mktemp "$RT/results/.res.XXXXXX") || return
    printf 'ok=%s\n' "$2" >"$tmp"
    [ -n "${3:-}" ] && printf 'error=%s\n' "$3" >>"$tmp"
    [ -n "${4:-}" ] && printf 'state=%s\n' "$4" >>"$tmp"
    chmod 0644 "$tmp"
    mv -f "$tmp" "$RT/results/$rid.res"
}

# ---------- пользователи, папки, права ----------

user_of() { echo "vhs$1"; }
uid_of() { echo $((UID_BASE + $1)); }

ensure_user() {
    local id=$1 u uid
    u=$(user_of "$id")
    uid=$(uid_of "$id")
    getent group "$u" >/dev/null 2>&1 || groupadd -g "$uid" "$u" || return 1
    getent passwd "$u" >/dev/null 2>&1 || useradd -u "$uid" -g "$u" -M -d /nonexistent -s /usr/sbin/nologin "$u" || return 1
}

ensure_tmp() { # host id — tmp сайта принадлежит пользователю сайта; создаёт его root, поэтому ссылкой на чужое место он стать не может
    local tmp=$SITES/$1/tmp u
    u=$(user_of "$2")
    [ -L "$tmp" ] && rm -f -- "$tmp"
    mkdir -p -- "$tmp" && "$CHOWN" "$u:$u" "$tmp" && chmod 0700 "$tmp"
}

# Записи «номер → адрес сайта»: по ним таймер выдаёт права и не даёт двум сайтам занять один номер.
write_record() { # id host
    local tmp
    tmp=$(mktemp "$RT/sites/.rec.XXXXXX") || return 1
    printf '%s\n' "$2" >"$tmp"
    chmod 0644 "$tmp"
    mv -f "$tmp" "$RT/sites/$1"
}

# sandbox папка_public скрипт — работа с файлами пользователя. Внутри видна только папка сайта (как /data/vladhost/site),
# сети нет, из возможностей — только смена владельца и прав; ссылки, подложенные пользователем, дальше песочницы не уведут.
sandbox() {
    "$SYSTEMD_RUN" --quiet --wait --collect --pipe \
        -p PrivateNetwork=yes -p NoNewPrivileges=yes -p PrivateTmp=yes -p ProtectSystem=strict -p ProtectHome=yes \
        -p CapabilityBoundingSet="CAP_FOWNER CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN" \
        -p TemporaryFileSystem=/data/vladhost:mode=0755,size=1m,nosuid,nodev \
        -p BindPaths="$1:/data/vladhost/site" -p ReadWritePaths=/data/vladhost/site \
        -p MemoryMax=256M -p TasksMax=32 -p RuntimeMaxSec=300 \
        -- /bin/bash -c "$2" >/dev/null 2>&1
}

# Права на файлы сайта: панель (vladhost) пишет, шлюз (vladhost-web) читает, приложение (vhsN) пишет, остальным ничего.
# Второй вызов задаёт то же по умолчанию, чтобы новые файлы получали те же права.
fix_perms() { # host id
    local spec
    spec="u:vladhost:rwX,u:vladhost-web:rX,u:$(user_of "$2"):rwX,m::rwX,o::---"
    sandbox "$SITES/$1/public" "setfacl -R -m $spec /data/vladhost/site && setfacl -R -d -m $spec /data/vladhost/site"
}

drop_perms() { # host — сайт снова обычная статика
    sandbox "$SITES/$1/public" "setfacl -R -b /data/vladhost/site"
}

site_ok() { # host — настоящая папка public: ни сайт, ни public не ссылки
    local base=$SITES/$1
    [ -L "$base" ] || [ -L "$base/public" ] || [ ! -d "$base/public" ] && return 1
    [ "$(readlink -f "$base/public")" = "$(readlink -f "$SITES")/$1/public" ]
}

shell_on() { [ -e "$RT/shell/$1" ]; }

# ---------- PHP-FPM ----------

fpm_test() { # версия
    if [ -n "${VH_RT_PHPFPM_TEST:-}" ]; then "$VH_RT_PHPFPM_TEST" "$1" 2>&1; else "/usr/sbin/php-fpm$1" -t 2>&1; fi
}
fpm_reload() { "$SYSTEMCTL" reload "php$1-fpm" >/dev/null 2>&1 || "$SYSTEMCTL" restart "php$1-fpm" >/dev/null 2>&1; }
pool_path() { echo "$PHP_ETC/$1/fpm/pool.d/vhs$2.conf"; }

pool_conf() { # host id
    local u root=$SITES/$1
    u=$(user_of "$2")
    cat <<EOF
; Пул сайта $1. Создан исполнителем сред выполнения Vladhost — руками не править.
[$u]
user = $u
group = $u
listen = $SOCK/$u.sock
listen.owner = vladhost-web
listen.group = vladhost-web
listen.mode = 0660
pm = ondemand
pm.max_children = 5
pm.process_idle_timeout = 20s
pm.max_requests = 500
request_terminate_timeout = 60s
catch_workers_output = yes
php_admin_value[open_basedir] = $root/public:$root/tmp:/usr/share/php
php_admin_value[error_log] = $root/tmp/php-error.log
php_admin_flag[log_errors] = on
php_admin_value[upload_tmp_dir] = $root/tmp
php_admin_value[session.save_path] = $root/tmp
php_admin_value[sys_temp_dir] = $root/tmp
php_admin_value[disable_functions] = exec,passthru,shell_exec,system,proc_open,popen
php_admin_value[memory_limit] = 192M
php_admin_value[max_execution_time] = 60
EOF
}

# remove_pools id [оставить_версию] — убирает пулы сайта во всех версиях PHP и перечитывает те, где пул был.
remove_pools() {
    local d v
    shopt -s nullglob
    for d in "$PHP_ETC"/*/fpm/pool.d/vhs"$1".conf; do
        v=${d%/fpm/pool.d/*}
        v=${v##*/}
        [ "$v" = "${2:-}" ] && continue
        rm -f -- "$d"
        fpm_reload "$v"
    done
    shopt -u nullglob
}

has_pool() {
    local d
    shopt -s nullglob
    for d in "$PHP_ETC"/*/fpm/pool.d/vhs"$1".conf; do
        shopt -u nullglob
        return 0
    done
    shopt -u nullglob
    return 1
}

# ---------- приложения (Node.js, Python) ----------

unit_path() { echo "$UNITS/vhapp-$1.service"; }

# Песочница службы совпадает с песочницей оболочки (shellbroker): своя папка сайта и tmp, секреты панели и чужие сайты скрыты.
unit_conf() { # host id runtime port
    local u root=$SITES/$1 extra=""
    u=$(user_of "$2")
    [ "$3" = node ] && extra="Environment=NODE_ENV=production"
    [ "$3" = python ] && extra="Environment=PYTHONUNBUFFERED=1"
    cat <<EOF
# Приложение сайта $1. Создан исполнителем сред выполнения Vladhost — руками не править.
[Unit]
Description=Vladhost: приложение сайта $1
After=network.target

[Service]
User=$u
Group=$u
Environment=PORT=$4
Environment=HOST=127.0.0.1
Environment=HOME=/data/vladhost/site/tmp
Environment=TMPDIR=/data/vladhost/site/tmp
$extra
WorkingDirectory=/data/vladhost/site/public
ExecStart=/bin/bash $APPS/$2.sh
Restart=on-failure
RestartSec=3
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ProtectClock=yes
ProtectHostname=yes
ProtectProc=invisible
ProcSubset=pid
RestrictSUIDSGID=yes
RestrictNamespaces=yes
LockPersonality=yes
RestrictRealtime=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
InaccessiblePaths=-/etc/vladhost -/var/lib/vladhost -/var/log/vladhost -/opt/vladhost -/opt/vladadminer -/root -/etc/letsencrypt -/etc/postgresql -/etc/mysql -/etc/ssh
TemporaryFileSystem=/data/vladhost:mode=0755,size=1m,nosuid,nodev
BindPaths=$root/public:/data/vladhost/site/public $root/tmp:/data/vladhost/site/tmp
ReadWritePaths=/data/vladhost/site/public /data/vladhost/site/tmp
MemoryMax=256M
MemorySwapMax=0
TasksMax=64
CPUQuota=100%
LimitNOFILE=1024

[Install]
WantedBy=multi-user.target
EOF
}

# remove_app id — служба и файл команды.
remove_app() {
    local unit
    unit=$(unit_path "$1")
    if [ -f "$unit" ]; then
        "$SYSTEMCTL" disable --now "vhapp-$1.service" >/dev/null 2>&1
        rm -f -- "$unit"
        "$SYSTEMCTL" daemon-reload >/dev/null 2>&1
    fi
    rm -f -- "$APPS/$1.sh"
}

state_of() { # юнит
    local s
    s=$("$SYSTEMCTL" is-active "$1" 2>/dev/null)
    echo "${s:-unknown}"
}

# ---------- действия над сайтом ----------

do_apply() { # rid host id runtime version port cmd
    local rid=$1 host=$2 id=$3 rt=$4 ver=$5 port=$6 cmd=$7 pool bak out tmp unit
    ensure_user "$id" || { reply "$rid" 0 user; return; }
    ensure_tmp "$host" "$id" || { reply "$rid" 0 tmp; return; }
    write_record "$id" "$host"
    if ! fix_perms "$host" "$id"; then
        reply "$rid" 0 perms
        return
    fi
    if [ "$rt" = php ]; then
        remove_app "$id"
        pool=$(pool_path "$ver" "$id")
        bak=""
        if [ -f "$pool" ]; then bak=$pool.vhbak; cp -p -- "$pool" "$bak"; fi
        tmp=$(mktemp "$(dirname "$pool")/.vhs$id.XXXXXX") || { reply "$rid" 0 pool; return; }
        pool_conf "$host" "$id" >"$tmp"
        chmod 0644 "$tmp"
        mv -f "$tmp" "$pool"
        if ! out=$(fpm_test "$ver"); then
            # Сломанный пул не остаётся: прежний возвращается, у нового имени файла нет.
            if [ -n "$bak" ]; then mv -f "$bak" "$pool"; else rm -f -- "$pool"; fi
            reply "$rid" 0 pool "" "$out"
            return
        fi
        [ -n "$bak" ] && rm -f -- "$bak"
        fpm_reload "$ver"
        remove_pools "$id" "$ver"
        reply "$rid" 1 "" "$(state_of "php$ver-fpm")"
        return
    fi
    # Node.js и Python: служба systemd; команда лежит в файле root (в файл службы она не попадает, чтобы не менять юнит).
    remove_pools "$id"
    install -d -m 0755 "$APPS"
    tmp=$(mktemp "$APPS/.$id.XXXXXX") || { reply "$rid" 0 unit; return; }
    printf '#!/bin/bash\ncd /data/vladhost/site/public || exit 1\n%s\n' "$cmd" >"$tmp"
    chmod 0755 "$tmp"
    mv -f "$tmp" "$APPS/$id.sh"
    unit=$(unit_path "$id")
    tmp=$(mktemp "$UNITS/.vhapp-$id.XXXXXX") || { reply "$rid" 0 unit; return; }
    unit_conf "$host" "$id" "$rt" "$port" >"$tmp"
    chmod 0644 "$tmp"
    mv -f "$tmp" "$unit"
    "$SYSTEMCTL" daemon-reload >/dev/null 2>&1
    "$SYSTEMCTL" enable "vhapp-$id.service" >/dev/null 2>&1
    "$SYSTEMCTL" restart "vhapp-$id.service" >/dev/null 2>&1
    sleep "$SETTLE"
    reply "$rid" 1 "" "$(state_of "vhapp-$id.service")"
}

# stop: сайт снова статика. Права и запись о сайте остаются, если включён доступ к оболочке.
do_stop() { # rid host id
    remove_pools "$3"
    remove_app "$3"
    if ! shell_on "$3"; then
        [ -d "$SITES/$2/public" ] && drop_perms "$2"
        rm -f -- "$RT/sites/$3"
    fi
    reply "$1" 1 "" ""
}

do_restart() { # rid host id runtime version
    if [ "$4" = php ]; then
        fpm_reload "$5"
        reply "$1" 1 "" "$(state_of "php$5-fpm")"
    elif [ "$4" = node ] || [ "$4" = python ]; then
        "$SYSTEMCTL" restart "vhapp-$3.service" >/dev/null 2>&1
        sleep "$SETTLE"
        reply "$1" 1 "" "$(state_of "vhapp-$3.service")"
    else
        reply "$1" 1 "" ""
    fi
}

do_status() { # rid host id runtime version
    local st=""
    case $4 in
        php) if [ -f "$(pool_path "$5" "$3")" ]; then st=$(state_of "php$5-fpm"); else st=inactive; fi ;;
        node | python) st=$(state_of "vhapp-$3.service") ;;
    esac
    reply "$1" 1 "" "$st"
}

do_logs() { # rid host id runtime version
    local out="" log=$SITES/$2/tmp/php-error.log uid
    case $4 in
        node | python) out=$("$JOURNALCTL" -u "vhapp-$3.service" -n 200 --no-pager -o cat 2>&1) ;;
        php)
            # Файл журнала создаёт пользователь сайта, поэтому читаем его от его имени: подложенная ссылка на чужой файл не откроется.
            uid=$(uid_of "$3")
            [ -f "$log" ] && out=$("$SETPRIV" --reuid="$uid" --regid="$uid" --clear-groups tail -c 32768 -- "$log" 2>&1)
            ;;
    esac
    reply "$1" 1 "" "" "$out"
}

do_shell_on() { # rid host id
    local tmp
    ensure_user "$3" || { reply "$1" 0 user; return; }
    ensure_tmp "$2" "$3" || { reply "$1" 0 tmp; return; }
    write_record "$3" "$2"
    fix_perms "$2" "$3" || { reply "$1" 0 perms; return; }
    tmp=$(mktemp "$RT/shell/.lbl.XXXXXX") || { reply "$1" 0 label; return; }
    printf '%s\n' "$2" >"$tmp"
    chmod 0644 "$tmp"
    mv -f "$tmp" "$RT/shell/$3" # метка для посредника оболочек: номер сайта → адрес
    reply "$1" 1 "" ""
}

do_shell_off() { # rid host id
    rm -f -- "$RT/shell/$3"
    # Права нужны, пока у сайта есть среда выполнения; без неё сайт снова обычная статика.
    if ! has_pool "$3" && [ ! -f "$(unit_path "$3")" ]; then
        [ -d "$SITES/$2/public" ] && drop_perms "$2"
        rm -f -- "$RT/sites/$3"
    fi
    reply "$1" 1 "" ""
}

do_purge() { # rid host id
    local u
    u=$(user_of "$3")
    remove_pools "$3"
    remove_app "$3"
    rm -f -- "$RT/shell/$3" "$RT/sites/$3"
    [ -L "$SITES/$2" ] || rm -rf -- "${SITES:?}/$2/tmp"
    if getent passwd "$u" >/dev/null 2>&1; then
        pkill -KILL -u "$u" >/dev/null 2>&1
        userdel "$u" >/dev/null 2>&1
    fi
    getent group "$u" >/dev/null 2>&1 && groupdel "$u" >/dev/null 2>&1
    reply "$1" 1 "" ""
}

# ---------- почта и DNS: скрипты синхронизации ----------

# run_script rid скрипт аргументы… — выводит успех или код ошибки из «error=код» в первой строке.
run_script() {
    local rid=$1 script=$2 out rc
    shift 2
    out=$(python3 "$script" "$@" 2>&1)
    rc=$?
    if [[ $out == error=* ]]; then
        reply "$rid" 0 "$(head -n1 <<<"$out" | cut -d= -f2- | tr -cd 'a-z0-9_')" "" ""
    elif [ "$rc" -ne 0 ]; then
        reply "$rid" 0 script "" "$out"
    else
        reply "$rid" 1 "" "" "$out"
    fi
}

regular_file() { [ -f "$1" ] && [ ! -L "$1" ]; }

# ---------- разбор заявки ----------

handle() {
    local f=$1 rid line k v action="" host="" id="" rt="" ver="" port="" cmdb="" cmd="" have_id=0
    rid=${f##*/}
    rid=${rid%.req}
    [[ $rid =~ ^[A-Za-z0-9-]+$ ]] || { rm -f -- "$f"; return; }
    # Заявка — обычный файл: ссылкой вместо неё можно было бы подсунуть чужое содержимое.
    if [ -L "$f" ] || [ ! -f "$f" ]; then
        rm -f -- "$f"
        reply "$rid" 0 request
        return
    fi
    while IFS= read -r line; do
        k=${line%%=*}
        v=${line#*=}
        case $k in
            action) action=$v ;; host) host=$v ;; id) id=$v; have_id=1 ;; runtime) rt=$v ;;
            version) ver=$v ;; port) port=$v ;; cmd) cmdb=$v ;;
        esac
    done < <(head -c 262144 -- "$f")
    rm -f -- "$f"

    case $action in
        mail-sync)
            regular_file "$RT/mail/state.json" || { reply "$rid" 0 no_state; return; }
            run_script "$rid" "$MAIL_SYNC" --state "$RT/mail/state.json" --usage "$RT/mail/usage.json"
            return
            ;;
        dns-sync)
            regular_file "$RT/dns/state.json" || { reply "$rid" 0 no_state; return; }
            run_script "$rid" "$DNS_SYNC" --state "$RT/dns/state.json"
            return
            ;;
        mail-log)
            # host — домен клиента, id — сколько событий показать (1–120).
            [[ ${#host} -le 253 && $host =~ $DOMAIN_RE ]] || { reply "$rid" 0 domain; return; }
            [[ $have_id = 1 && $id =~ ^[1-9][0-9]{0,2}$ && $id -le 120 ]] || { reply "$rid" 0 limit; return; }
            run_script "$rid" "$MAIL_LOG" --domain "$host" --limit "$id"
            return
            ;;
        apply | stop | restart | status | logs | perms | purge | shell-on | shell-off) ;;
        *) reply "$rid" 0 action; return ;;
    esac

    [[ $host =~ $HOST_RE ]] || { reply "$rid" 0 host; return; }
    [[ $id =~ ^[1-9][0-9]{0,3}$ ]] || { reply "$rid" 0 id; return; }
    case $rt in
        php | node | python) ;;
        static | "") [ "$action" = apply ] && { reply "$rid" 0 runtime; return; } ;;
        *) reply "$rid" 0 runtime; return ;;
    esac
    if [ "$rt" = php ]; then
        [[ $ver =~ ^[0-9]{1,2}\.[0-9]{1,2}$ ]] || { reply "$rid" 0 version; return; }
        if [ "$action" = apply ] && [ ! -d "$PHP_ETC/$ver/fpm/pool.d" ]; then
            reply "$rid" 0 version
            return
        fi
    fi
    if [ "$action" = apply ] && [ "$rt" != php ]; then
        [[ $port =~ ^[0-9]{5}$ && $port -ge 20000 && $port -le 29999 ]] || { reply "$rid" 0 port; return; }
        cmd=$(printf '%s' "$cmdb" | base64 -d 2>/dev/null) || { reply "$rid" 0 command; return; }
        [[ -n $cmd && ${#cmd} -le $MAX_CMD && $cmd != *$'\n'* && $cmd != *$'\r'* ]] || { reply "$rid" 0 command; return; }
    fi
    # Номер сайта закреплён за одним адресом.
    if [ -f "$RT/sites/$id" ] && [ "$(head -n1 "$RT/sites/$id")" != "$host" ]; then
        reply "$rid" 0 site_mismatch
        return
    fi
    case $action in
        apply | shell-on | perms) site_ok "$host" || { reply "$rid" 0 site_not_found; return; } ;;
    esac

    case $action in
        apply) do_apply "$rid" "$host" "$id" "$rt" "$ver" "$port" "$cmd" ;;
        stop) do_stop "$rid" "$host" "$id" ;;
        restart) do_restart "$rid" "$host" "$id" "$rt" "$ver" ;;
        status) do_status "$rid" "$host" "$id" "$rt" "$ver" ;;
        logs) do_logs "$rid" "$host" "$id" "$rt" "$ver" ;;
        perms)
            fix_perms "$host" "$id" && reply "$rid" 1 || reply "$rid" 0 perms
            ;;
        purge) do_purge "$rid" "$host" "$id" ;;
        shell-on) do_shell_on "$rid" "$host" "$id" ;;
        shell-off) do_shell_off "$rid" "$host" "$id" ;;
    esac
}

# perms-all — вернуть права всем сайтам из списка: приложение создаёт файлы от своего имени, а панели и шлюзу они нужны.
perms_all() {
    local rec id host
    shopt -s nullglob
    for rec in "$RT"/sites/*; do
        id=${rec##*/}
        [[ $id =~ ^[1-9][0-9]{0,3}$ ]] || continue
        [ -f "$rec" ] && [ ! -L "$rec" ] || continue
        host=$(head -n1 "$rec")
        [[ $host =~ $HOST_RE ]] || continue
        site_ok "$host" || continue
        fix_perms "$host" "$id"
    done
    shopt -u nullglob
}

if [ "${1:-}" = perms-all ]; then
    exec 9>"$RT/.perms.lock"
    flock -n 9 || exit 0
    perms_all
    exit 0
fi

exec 8>"$RT/.queue.lock"
flock 8

# Уборка: недописанные заявки старше 2 минут и посторонние файлы; ответы старше 30 минут.
find "$RT/queue" -maxdepth 1 -mmin +2 \( -type f -o -type l \) ! -name '*.req' -delete 2>/dev/null
find "$RT/results" -maxdepth 1 -type f -mmin +30 -delete 2>/dev/null

while :; do
    shopt -s nullglob
    reqs=("$RT"/queue/*.req)
    shopt -u nullglob
    [ ${#reqs[@]} -eq 0 ] && break
    for f in "${reqs[@]}"; do handle "$f"; done
done

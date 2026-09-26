#!/bin/bash
# Исполнитель команд планировщика (root). Запускается vladhost-cron.path, когда панель кладёт заявку в очередь.
# Заявка queue/{id}.req: строки host=, timeout= (секунды, 1–300), cmd= (base64). Результат: results/{id}.res (exit=, timeout=,
# при отказе error=) и results/{id}.out (вывод, до 32 КБ). Панель ничего исполнять не может — только класть заявки и читать результаты.
# Команда идёт в песочнице systemd-run: пользователь vladhost, видна только папка одного сайта, без loopback и внутренних сетей.
set -uo pipefail

QUEUE=${VH_CRON_QUEUE:-/var/lib/vladhost/cron/queue}
RESULTS=${VH_CRON_RESULTS:-/var/lib/vladhost/cron/results}
WORK=${VH_CRON_WORK:-/var/lib/vladhost/cron/work}
SITES=${VH_CRON_SITES:-/data/vladhost/sites}
SRUN=${VH_CRON_SYSTEMD_RUN:-systemd-run}
USER_=${VH_CRON_USER:-vladhost}
PARALLEL=${VH_CRON_PARALLEL:-3}
MAXOUT=32768
HOST_RE='^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?){2,3}$'

mkdir -p "$RESULTS" "$WORK"

# finish id exit timeout [ошибка] — результат пишется атомарно: панель читает .res и сразу берёт .out.
finish() {
    local id=$1 tmp
    tmp=$(mktemp "$RESULTS/.res.XXXXXX")
    printf 'exit=%s\ntimeout=%s\n' "$2" "$3" >"$tmp"
    [ -n "${4:-}" ] && printf 'error=%s\n' "$4" >>"$tmp"
    chmod 0644 "$tmp"
    mv -f "$tmp" "$RESULTS/$id.res"
}

# refuse id ошибка — отказ: пустой вывод, код 1.
refuse() {
    : >"$RESULTS/$1.out"
    chmod 0644 "$RESULTS/$1.out"
    finish "$1" 1 0 "$2"
}

handle() { # путь к .req
    local f=$1 base id host timeout cmd line k v dir w out res rc to=0
    base=${f##*/}
    id=${base%.req}
    [[ $id =~ ^[0-9]+$ ]] || { rm -f -- "$f"; return; }
    # Заявка — обычный файл, не ссылка (иначе можно подсунуть чужой файл вместо заявки).
    if [ -L "$f" ] || [ ! -f "$f" ]; then
        rm -f -- "$f"
        refuse "$id" "invalid request"
        return
    fi
    host="" timeout="" cmd=""
    while IFS= read -r line; do
        k=${line%%=*}
        v=${line#*=}
        case $k in host) host=$v ;; timeout) timeout=$v ;; cmd) cmd=$v ;; esac
    done < <(head -c 262144 -- "$f")
    rm -f -- "$f"

    [[ $host =~ $HOST_RE ]] || { refuse "$id" "invalid host"; return; }
    [[ $timeout =~ ^[0-9]{1,3}$ ]] && [ "$timeout" -ge 1 ] && [ "$timeout" -le 300 ] || { refuse "$id" "invalid timeout"; return; }
    cmd=$(printf '%s' "$cmd" | base64 -d 2>/dev/null) && [ -n "$cmd" ] || { refuse "$id" "invalid command"; return; }

    # Папка сайта: только настоящий каталог, ни сам сайт, ни public не могут быть ссылкой.
    dir=$SITES/$host/public
    if [ -L "$SITES/$host" ] || [ -L "$dir" ] || [ ! -d "$dir" ]; then
        refuse "$id" "site not found"
        return
    fi
    dir=$(readlink -f "$dir")
    [[ $dir == "$(readlink -f "$SITES")/$host/public" ]] || { refuse "$id" "site not found"; return; }

    w=$(mktemp -d "$WORK/run.XXXXXX")
    out=$w/out
    # Все ограничения — на самом юните; timeout внутри — чтобы код 124 отличал таймаут, RuntimeMaxSec — страховка.
    "$SRUN" --quiet --wait --collect --pipe \
        -p User="$USER_" -p NoNewPrivileges=yes -p ProtectSystem=strict \
        -p ProtectHome=yes -p ProtectProc=invisible -p PrivateTmp=yes -p PrivateDevices=yes \
        -p InaccessiblePaths=-/etc/vladhost -p InaccessiblePaths=-/opt -p InaccessiblePaths=-/var/lib/vladhost \
        -p TemporaryFileSystem=/data/vladhost -p BindPaths="$dir:/data/vladhost/site" \
        -p MemoryMax=256M -p TasksMax=64 -p CPUQuota=50% \
        -p IPAddressDeny=127.0.0.0/8 -p IPAddressDeny=10.0.0.0/8 -p IPAddressDeny=172.16.0.0/12 \
        -p IPAddressDeny=192.168.0.0/16 -p IPAddressDeny=169.254.0.0/16 -p IPAddressDeny=::1/128 \
        -p RuntimeMaxSec=$((timeout + 10)) --working-directory=/data/vladhost/site \
        -- timeout -k 3 "$timeout" /bin/bash -c "$cmd" </dev/null 2>&1 | head -c "$MAXOUT" >"$out"
    rc=${PIPESTATUS[0]}
    if [ "$rc" = 124 ] || [ "$rc" = 137 ]; then
        to=1
        : >"$out" # после таймаута вывод не отдаём: он мог оборваться на полуслове
        rc=124
    fi
    res=$RESULTS/$id.out
    cp -- "$out" "$res"
    chmod 0644 "$res"
    rm -rf -- "$w"
    finish "$id" "$rc" "$to"
}

# Уборка: недописанные заявки старше 2 минут, посторонние файлы, результаты старше 20 минут.
find "$QUEUE" -maxdepth 1 -mmin +2 \( -type f -o -type l \) ! -name '*.req' -delete 2>/dev/null
find "$RESULTS" -maxdepth 1 -type f -mmin +20 -delete 2>/dev/null

while :; do
    shopt -s nullglob
    reqs=("$QUEUE"/*.req)
    shopt -u nullglob
    [ ${#reqs[@]} -eq 0 ] && break
    running=0
    for f in "${reqs[@]}"; do
        handle "$f" &
        running=$((running + 1))
        if [ "$running" -ge "$PARALLEL" ]; then
            wait -n
            running=$((running - 1))
        fi
    done
    wait
done

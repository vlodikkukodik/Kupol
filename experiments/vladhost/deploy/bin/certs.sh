#!/bin/bash
# Выпускатель сертификатов (root). Запускается vladhost-certs.path, когда панель кладёт заявку в очередь:
#   queue/issue-{имя}  — выпустить (свой домен: сначала проверить DNS и создать server-блок nginx);
#   queue/renew-{имя}  — перевыпустить принудительно (--force-renewal); при неудаче действующий сертификат остаётся;
#   queue/delete-{имя} — удалить сертификат, копию для nginx, сведения и server-блок своего домена.
# Результат — status/{имя}: «ok» или «error: …» (каталог status принадлежит root: панель не подложит туда симлинк).
# Имя из заявки не доверенное: до certbot и nginx доходят только имена, прошедшие строгий шаблон.
set -uo pipefail

BASE=${VH_CERTS_BASE:-/var/lib/vladhost/certs}
WEBROOT=${VH_WEBROOT:-/var/www/vladhost-acme}
CERTS=${VH_CERTS_DIR:-/etc/vladhost/certs}
HOOK=${VH_HOOK:-/usr/local/lib/vladhost/cert-hook.sh}
INFO=${VH_INFO:-/usr/local/lib/vladhost/cert-info.sh}
NGX=${VH_NGINX_ETC:-/etc/nginx}
CONF=${VH_CERTS_CONF:-/etc/vladhost/certs.conf}
LIVE=${VH_LE_LIVE:-/etc/letsencrypt/live}
BASE_DOMAIN=vladinc.ru

QUEUE=$BASE/queue
STATUS=$BASE/status
DOMAINS=$NGX/vladhost-domains

LABEL='[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?'
SITE_RE="^${LABEL}\.${LABEL}\.vladinc\.ru$"             # {сайт}.{пользователь}.vladinc.ru
SUB_RE="^${LABEL}\.${LABEL}\.${LABEL}\.vladinc\.ru$"    # {метка}.{сайт}.{пользователь}.vladinc.ru
DOMAIN_RE="^(${LABEL}\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$" # свой домен; зона начинается с буквы

# classify имя → site (адрес сайта или его поддомен) | custom (свой домен) | пусто (недопустимое).
classify() {
    local n=$1
    [ ${#n} -le 253 ] || return 0
    if [[ $n =~ $SITE_RE || $n =~ $SUB_RE ]]; then
        echo site
    elif [[ $n =~ $DOMAIN_RE && $n != "$BASE_DOMAIN" && $n != *".$BASE_DOMAIN" ]]; then
        echo custom
    fi
}

# write_status имя текст — атомарно, чтобы панель не прочитала половину.
write_status() {
    local tmp
    tmp=$(mktemp "$STATUS/.tmp.XXXXXX") || return 1
    printf '%s\n' "$2" >"$tmp"
    chmod 0644 "$tmp"
    mv -f "$tmp" "$STATUS/$1"
}

server_ips() {
    [ -r "$CONF" ] || return 0
    sed -n 's/^SERVER_IPS="\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' "$CONF" | tail -n1
}

# check_dns имя — A-записи домена должны указывать только на этот сервер. Имя спрашиваем с точкой в конце:
# иначе glibc добавит search-домен хостера, а у того wildcard на наш же IP — и чужой домен «подтвердится».
check_dns() {
    local name=$1 ips found ip bad=""
    ips=$(server_ips)
    if [ -z "${ips// /}" ]; then
        echo "server IPs are not configured (SERVER_IPS in $CONF)"
        return 1
    fi
    found=$(getent ahostsv4 "$name." 2>/dev/null | awk '{print $1}' | sort -u | tr '\n' ' ')
    found=${found% }
    if [ -z "$found" ]; then
        echo "DNS: $name has no A record pointing to this server (expected ${ips// /, })"
        return 1
    fi
    for ip in $found; do
        [[ " $ips " == *" $ip "* ]] || bad=1
    done
    if [ -n "$bad" ]; then
        echo "DNS: $name points to ${found// /, }, expected ${ips// /, }"
        return 1
    fi
}

# served_elsewhere имя — имя уже обслуживает другой проект на этом сервере (точное совпадение в server_name).
served_elsewhere() {
    local f
    for f in "$NGX"/sites-enabled/* "$NGX"/conf.d/*.conf; do
        [ -r "$f" ] || continue
        awk -v n="$1" '
            $1 == "server_name" { for (i = 2; i <= NF; i++) { t = $i; sub(/;$/, "", t); if (t == n) found = 1 } }
            END { exit !found }' "$f" && return 0
    done
    return 1
}

domain_conf() {
    cat <<EOF
# Свой домен пользователя. Файл создан выпускателем сертификатов (certs.sh) и им же удаляется — руками не править.
server {
    listen 80;
    server_name $1;

    location /.well-known/acme-challenge/ { root $WEBROOT; }
    location / { return 301 https://\$host\$request_uri; }
}

server {
    listen 443 ssl;
    http2 on;
    server_name $1;

    ssl_certificate     /etc/vladhost/certs/\$ssl_server_name/fullchain.pem;
    ssl_certificate_key /etc/vladhost/certs/\$ssl_server_name/privkey.pem;

    include /etc/nginx/snippets/vladhost-site-proxy.conf;
}
EOF
}

reload_nginx() {
    nginx -t >/dev/null 2>&1 || return 1
    systemctl reload nginx
}

# setup_domain имя — server-блок своего домена; при отказе nginx блок убирается.
setup_domain() {
    local name=$1 conf=$DOMAINS/$1.conf tmp
    mkdir -p "$DOMAINS"
    tmp=$(mktemp "$DOMAINS/.tmp.XXXXXX") || return 1
    domain_conf "$name" >"$tmp"
    chmod 0644 "$tmp"
    if [ -f "$conf" ] && cmp -s "$tmp" "$conf"; then
        rm -f "$tmp"
        return 0
    fi
    mv -f "$tmp" "$conf"
    if ! reload_nginx; then
        rm -f "$conf"
        reload_nginx >/dev/null 2>&1
        echo "nginx rejected the configuration for $name"
        return 1
    fi
}

# certbot_run имя [--force-renewal] — выпуск по HTTP-01; текст ошибки без служебных строк certbot.
certbot_run() {
    local name=$1 out
    shift
    if out=$(certbot certonly --webroot -w "$WEBROOT" -d "$name" --cert-name "$name" --non-interactive --agree-tos \
        --register-unsafely-without-email --keep-until-expiring --deploy-hook "$HOOK" "$@" 2>&1); then
        # Сертификат уже был и не продлевался — хук не срабатывал; копия для nginx всё равно нужна.
        if [ ! -e "$CERTS/$name/fullchain.pem" ] && [ -d "$LIVE/$name" ]; then
            RENEWED_LINEAGE=$LIVE/$name "$HOOK" >/dev/null 2>&1
        fi
        return 0
    fi
    out=$(printf '%s\n' "$out" | grep -vE 'Saving debug log|Ask for help|See the logfile|log file|^[[:space:]]*-*[[:space:]]*$' |
        tr '\n' ' ' | tr -s ' ' | cut -c1-500)
    echo "${out:-certbot failed}"
    return 1
}

do_issue() { # имя класс [--force-renewal]
    local name=$1 class=$2 msg
    shift 2
    if [ "$class" = custom ]; then
        msg=$(check_dns "$name") || { write_status "$name" "error: $msg"; return; }
        if [ ! -f "$DOMAINS/$name.conf" ] && served_elsewhere "$name"; then
            write_status "$name" "error: $name is already served by another site on this server"
            return
        fi
        msg=$(setup_domain "$name") || { write_status "$name" "error: $msg"; return; }
    fi
    msg=$(certbot_run "$name" "$@") || { write_status "$name" "error: $msg"; return; }
    "$INFO" "$name" >/dev/null 2>&1
    write_status "$name" ok
}

do_renew() { # имя класс
    if [ "$2" = custom ] && [ ! -f "$DOMAINS/$1.conf" ]; then
        write_status "$1" "error: $1 is not set up on this server"
        return
    fi
    do_issue "$1" "$2" --force-renewal
}

do_delete() { # имя класс
    local name=$1 target
    if [ "$2" = custom ] && [ -e "$DOMAINS/$name.conf" ]; then
        rm -f "$DOMAINS/$name.conf"
        reload_nginx >/dev/null 2>&1
    fi
    certbot delete --cert-name "$name" --non-interactive >/dev/null 2>&1
    # Копия для nginx: ссылка на каталог в .store (там закрытый ключ) — удаляем и то, и другое.
    if [ -L "$CERTS/$name" ]; then
        target=$(readlink -f "$CERTS/$name")
        rm -f "$CERTS/$name"
        [[ $target == "$(readlink -f "$CERTS")/.store/"* ]] && rm -rf -- "$target"
    elif [ -d "$CERTS/$name" ]; then
        rm -rf -- "${CERTS:?}/$name"
    fi
    rm -f "$BASE/info/$name" "$STATUS/$name"
}

mkdir -p "$STATUS"
while :; do
    shopt -s nullglob
    files=("$QUEUE"/*)
    shopt -u nullglob
    [ ${#files[@]} -eq 0 ] && break
    for f in "${files[@]}"; do
        req=${f##*/}
        rm -f -- "$f"
        kind=${req%%-*}
        name=${req#*-}
        class=$(classify "$name")
        if [ "$req" = "$kind" ] || [ -z "$class" ] || [[ $kind != issue && $kind != renew && $kind != delete ]]; then
            printf 'skip: недопустимое имя в заявке %q\n' "$req"
            continue
        fi
        case $kind in
            issue) do_issue "$name" "$class" ;;
            renew) do_renew "$name" "$class" ;;
            delete) do_delete "$name" "$class" ;;
        esac
        echo "$kind $name: $(cat "$STATUS/$name" 2>/dev/null || echo done)"
    done
done

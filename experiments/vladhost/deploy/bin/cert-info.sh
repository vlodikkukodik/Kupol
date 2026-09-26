#!/bin/bash
# Сведения о сертификате для панели (root): cert-info.sh ИМЯ | cert-info.sh --all.
# Читает копию сертификата {VH_CERTS_DIR}/{имя}/fullchain.pem и пишет {VH_CERTS_BASE}/info/{имя} строками key=value:
#   not_before=…Z, not_after=…Z (ISO 8601, UTC), issuer=Организация (имя), names=имя1,имя2
# Панель читает только этот файл: к самим сертификатам и ключам у неё доступа нет.
# Вызывают: хук certbot (после выпуска и каждого продления), выпускатель certs.sh и выкладка (--all).
set -uo pipefail

CERTS=${VH_CERTS_DIR:-/etc/vladhost/certs}
BASE=${VH_CERTS_BASE:-/var/lib/vladhost/certs}
INFO=$BASE/info
NAME_RE='^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$'

iso() { date -u -d "$1" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null; }

one() {
    local name=$1 pem nb na issuer o cn names tmp
    [[ ${#name} -le 253 && $name =~ $NAME_RE ]] || { echo "недопустимое имя: $name" >&2; return 1; }
    pem=$CERTS/$name/fullchain.pem
    [ -r "$pem" ] || { echo "нет сертификата: $pem" >&2; return 1; }
    nb=$(openssl x509 -in "$pem" -noout -startdate 2>/dev/null | sed 's/^notBefore=//')
    na=$(openssl x509 -in "$pem" -noout -enddate 2>/dev/null | sed 's/^notAfter=//')
    # Пустая дата — не сертификат (date -d "" дал бы полночь сегодняшнего дня).
    [ -n "$nb" ] && [ -n "$na" ] && nb=$(iso "$nb") && na=$(iso "$na") || { echo "не читается сертификат: $pem" >&2; return 1; }
    # Издатель: «Организация (имя)», например «Let's Encrypt (R11)».
    issuer=$(openssl x509 -in "$pem" -noout -issuer -nameopt sep_multiline,utf8 2>/dev/null)
    o=$(sed -n 's/^[[:space:]]*O=//p' <<<"$issuer" | head -n1)
    cn=$(sed -n 's/^[[:space:]]*CN=//p' <<<"$issuer" | head -n1)
    if [ -n "$o" ] && [ -n "$cn" ]; then issuer="$o ($cn)"; else issuer=${o:-$cn}; fi
    names=$(openssl x509 -in "$pem" -noout -ext subjectAltName 2>/dev/null | tail -n +2 |
        tr ',' '\n' | sed -n 's/^[[:space:]]*DNS://p' | paste -sd, -)
    mkdir -p "$INFO"
    tmp=$(mktemp "$INFO/.tmp.XXXXXX") || return 1
    printf 'not_before=%s\nnot_after=%s\nissuer=%s\nnames=%s\n' "$nb" "$na" "${issuer//$'\n'/ }" "$names" >"$tmp"
    chmod 0644 "$tmp"
    mv -f "$tmp" "$INFO/$name"
}

case ${1:-} in
    "") echo "использование: cert-info.sh ИМЯ | --all" >&2; exit 2 ;;
    --all)
        shopt -s nullglob
        for d in "$CERTS"/*/; do
            n=${d%/}
            n=${n##*/}
            [ -e "$CERTS/$n/fullchain.pem" ] && one "$n" 2>/dev/null
        done
        exit 0
        ;;
    *) one "$1" ;;
esac

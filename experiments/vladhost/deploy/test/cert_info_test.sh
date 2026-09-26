#!/bin/bash
# Тест deploy/bin/cert-info.sh на настоящем openssl и самоподписанных сертификатах во временном каталоге.
# Запуск: bash deploy/test/cert_info_test.sh
set -u
cd "$(dirname "$0")/../.." || exit 1
SCRIPT=$PWD/deploy/bin/cert-info.sh
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "ПРОВАЛ: $*" >&2; }
check() { local d=$1; shift; if "$@" >/dev/null 2>&1; then ok; else bad "$d"; fi; }

export VH_CERTS_DIR=$T/certs VH_CERTS_BASE=$T/base
mkdir -p "$VH_CERTS_DIR" "$VH_CERTS_BASE"

mkcert() { # host san... — самоподписанный сертификат в $VH_CERTS_DIR/host/fullchain.pem
    local host=$1 san=$2 days=${3:-90}
    mkdir -p "$VH_CERTS_DIR/$host"
    openssl req -x509 -newkey rsa:2048 -nodes -days "$days" -subj "/O=Test Org/CN=Test CA" \
        -addext "subjectAltName=$san" -keyout "$T/key.pem" -out "$VH_CERTS_DIR/$host/fullchain.pem" >/dev/null 2>&1
}
field() { sed -n "s/^$1=//p" "$VH_CERTS_BASE/info/$2"; }

echo "== сведения о сертификате"
mkcert a.example.com "DNS:a.example.com,DNS:www.a.example.com" 90
check "скрипт отработал" bash "$SCRIPT" a.example.com
f=$VH_CERTS_BASE/info/a.example.com
check "файл создан" test -f "$f"
check "права 0644" test "$(stat -c %a "$f")" = 644
check "срок начала в ISO 8601" grep -qE '^not_before=[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' "$f"
check "срок окончания в ISO 8601" grep -qE '^not_after=[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' "$f"
check "издатель: организация и имя" test "$(field issuer a.example.com)" = "Test Org (Test CA)"
check "имена сертификата" test "$(field names a.example.com)" = "a.example.com,www.a.example.com"
# срок окончания — примерно через 90 суток от сегодня
na=$(date -u -d "$(field not_after a.example.com)" +%s)
now=$(date -u +%s)
days=$(((na - now) / 86400))
check "до окончания ~90 суток (получилось $days)" test "$days" -ge 89 -a "$days" -le 90
check "нет временных файлов" test -z "$(ls -A "$VH_CERTS_BASE/info" | grep '^\.tmp' || true)"

echo "== перезапись при продлении"
mkcert a.example.com "DNS:a.example.com" 30
bash "$SCRIPT" a.example.com
na2=$(date -u -d "$(field not_after a.example.com)" +%s)
check "срок обновился" test "$na2" -lt "$na"
check "имена обновились" test "$(field names a.example.com)" = "a.example.com"

echo "== недопустимые имена и испорченные сертификаты"
rm -rf "$VH_CERTS_BASE/info"
for evil in '../x' 'A.example.com' 'a..b.com' '' 'a/b' '.hidden' '-a.com' 'a b.com' 'a;b.com'; do
    bash "$SCRIPT" "$evil" >/dev/null 2>&1 && bad "имя '$evil' принято"
done
check "ничего не создано" test -z "$(ls -A "$VH_CERTS_BASE/info" 2>/dev/null)"
mkdir -p "$VH_CERTS_DIR/broken.com"
echo "not a certificate" >"$VH_CERTS_DIR/broken.com/fullchain.pem"
bash "$SCRIPT" broken.com >/dev/null 2>&1 && bad "испорченный сертификат принят"
check "для испорченного файла нет" test ! -e "$VH_CERTS_BASE/info/broken.com"
bash "$SCRIPT" missing.com >/dev/null 2>&1 && bad "несуществующий сертификат принят"
bash "$SCRIPT" >/dev/null 2>&1 && bad "вызов без аргументов принят"

echo "== --all: все сертификаты, скрытые каталоги и испорченные не мешают"
mkcert b.example.com "DNS:b.example.com" 60
mkdir -p "$VH_CERTS_DIR/.store/x"
bash "$SCRIPT" --all >/dev/null 2>&1
check "--all вернул успех" bash "$SCRIPT" --all
check "b записан" test -f "$VH_CERTS_BASE/info/b.example.com"
check "a записан" test -f "$VH_CERTS_BASE/info/a.example.com"
check "скрытый каталог пропущен" test ! -e "$VH_CERTS_BASE/info/.store"
check "испорченный пропущен" test ! -e "$VH_CERTS_BASE/info/broken.com"

echo
echo "успешно: $pass, провалов: $fail"
[ "$fail" -eq 0 ]

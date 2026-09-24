#!/bin/bash
# Тест выпускателя сертификатов (deploy/bin/certs.sh) на подменённых certbot, nginx, systemctl и getent:
# всё в временном каталоге, ничего не трогает в системе. Запуск: bash deploy/test/certs_test.sh
set -u
cd "$(dirname "$0")/../.." || exit 1
SCRIPT=$PWD/deploy/bin/certs.sh
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

pass=0
fail=0
ok() { pass=$((pass + 1)); }
bad() { fail=$((fail + 1)); echo "ПРОВАЛ: $*" >&2; }
check() { # описание, команда...
    local d=$1
    shift
    if "$@" >/dev/null 2>&1; then ok; else bad "$d"; fi
}

mkdir -p "$T/bin" "$T/certs/queue" "$T/certs/status" "$T/vhcerts/.store" "$T/nginx/sites-enabled" "$T/nginx/conf.d" "$T/webroot"
LOG=$T/calls.log
: >"$LOG"

# --- подменённые команды ---
cat >"$T/bin/certbot" <<EOF
#!/bin/bash
echo "certbot \$*" >>"$LOG"
host=""; while [ \$# -gt 0 ]; do [ "\$1" = "-d" ] && host=\$2; shift; done
[ -f "$T/certbot.fail" ] && { echo "Some challenges have failed."; exit 1; }
if [ "\$1" = certonly ] || true; then
    # как deploy-hook: копия сертификата для nginx
    mkdir -p "$T/vhcerts/.store/\$host.X"; touch "$T/vhcerts/.store/\$host.X/fullchain.pem" "$T/vhcerts/.store/\$host.X/privkey.pem"
    ln -sfn "$T/vhcerts/.store/\$host.X" "$T/vhcerts/\$host"
fi
exit 0
EOF
cat >"$T/bin/nginx" <<EOF
#!/bin/bash
echo "nginx \$*" >>"$LOG"
[ -f "$T/nginx.fail" ] && exit 1
exit 0
EOF
cat >"$T/bin/systemctl" <<EOF
#!/bin/bash
echo "systemctl \$*" >>"$LOG"
exit 0
EOF
# getent ahostsv4 <host>: адреса берутся из файла $T/dns/<host>
# Ведёт себя как glibc с search-доменом хостера, у которого wildcard-запись на наш IP: имя без точки в конце
# «находится» всегда (через search-домен), абсолютное имя (с точкой) — только если есть файл записи.
cat >"$T/bin/getent" <<EOF
#!/bin/bash
name=\$2
if [[ \$name != *. ]]; then
    echo "203.0.113.10  STREAM \$name.hoster.example"
    exit 0
fi
name=\${name%.}
[ -f "$T/dns/\$name" ] && awk '{print \$1 "  STREAM " "'"\$name"'"}' "$T/dns/\$name"
exit 0
EOF
chmod +x "$T"/bin/*
mkdir -p "$T/dns"
echo 'SERVER_IPS="203.0.113.10"' >"$T/certs.conf"

export PATH="$T/bin:$PATH"
export VH_CERTS_BASE=$T/certs VH_WEBROOT=$T/webroot VH_CERTS_DIR=$T/vhcerts VH_HOOK=/bin/true
export VH_NGINX_ETC=$T/nginx VH_CERTS_CONF=$T/certs.conf

run() { bash "$SCRIPT" >"$T/out.log" 2>&1; }
enqueue() { : >"$T/certs/queue/$1"; }
status() { cat "$T/certs/status/$1" 2>/dev/null; }
reset() { : >"$LOG"; rm -f "$T/certbot.fail" "$T/nginx.fail" "$T/certs/status"/* "$T/nginx/vladhost-domains"/*.conf 2>/dev/null; }

echo "== свой домен: верный DNS → server-блок, сертификат, статус ok"
reset
echo 203.0.113.10 >"$T/dns/example.com"
enqueue issue-example.com
run
check "статус ok" test "$(status example.com)" = ok
check "конфиг создан" test -f "$T/nginx/vladhost-domains/example.com.conf"
check "очередь очищена" test -z "$(ls "$T/certs/queue")"
conf=$(cat "$T/nginx/vladhost-domains/example.com.conf")
check "server_name подставлен" grep -q 'server_name example.com;' <<<"$conf"
check "ACME-путь в блоке :80" grep -q 'acme-challenge' <<<"$conf"
check "редирект на https" grep -q 'return 301 https://$host$request_uri' <<<"$conf"
check "сертификат по SNI" grep -q 'ssl_certificate     /etc/vladhost/certs/$ssl_server_name/fullchain.pem' <<<"$conf"
check "общий фрагмент прокси" grep -q 'include /etc/nginx/snippets/vladhost-site-proxy.conf' <<<"$conf"
check "nginx проверен и перезагружен" grep -q 'systemctl reload nginx' "$LOG"
check "certbot вызван для домена" grep -q 'certonly.*-d example.com' "$LOG"
check "нет временных файлов" test -z "$(ls -A "$T/nginx/vladhost-domains" | grep '^\.tmp' || true)"

echo "== DNS указывает не на нас → отказ, certbot не вызывается"
reset
echo 198.51.100.5 >"$T/dns/wrong.com"
enqueue issue-wrong.com
run
check "статус error" grep -q '^error: DNS' "$T/certs/status/wrong.com"
check "текст называет найденный адрес" grep -q '198.51.100.5' "$T/certs/status/wrong.com"
check "certbot не вызывался" test -z "$(grep certbot "$LOG")"
check "конфиг не создан" test ! -e "$T/nginx/vladhost-domains/wrong.com.conf"

echo "== search-домен хостера с wildcard не должен подтверждать чужой домен (регрессия боевого сбоя)"
reset
enqueue issue-victim-domain.com
run
check "чужое имя без записи отклонено" grep -q '^error: DNS' "$T/certs/status/victim-domain.com"
check "certbot не вызывался" test -z "$(grep certbot "$LOG")"
check "конфиг не создан" test ! -e "$T/nginx/vladhost-domains/victim-domain.com.conf"

echo "== смесь адресов (наш и чужой) → отказ"
reset
printf '203.0.113.10\n198.51.100.5\n' >"$T/dns/mixed.com"
enqueue issue-mixed.com
run
check "смесь адресов отклонена" grep -q '^error: DNS' "$T/certs/status/mixed.com"

echo "== нет A-записи → отказ"
reset
enqueue issue-nodns.com
run
check "нет записи" grep -q '^error: DNS' "$T/certs/status/nodns.com"

echo "== имя уже обслуживается другим проектом → отказ, конфиг не создаётся"
reset
echo 203.0.113.10 >"$T/dns/taken.com"
printf 'server {\n    server_name taken.com www.taken.com;\n}\n' >"$T/nginx/conf.d/other.conf"
enqueue issue-taken.com
run
check "чужой server_name защищён" grep -q 'already served' "$T/certs/status/taken.com"
check "конфиг не создан" test ! -e "$T/nginx/vladhost-domains/taken.com.conf"
# через символическую ссылку в sites-enabled (как у существующих проектов)
echo 203.0.113.10 >"$T/dns/linked.com"
mkdir -p "$T/available"
printf 'server {\n    server_name api.linked.com linked.com;\n}\n' >"$T/available/p.conf"
ln -sf "$T/available/p.conf" "$T/nginx/sites-enabled/p.conf"
reset
enqueue issue-linked.com
run
check "чужой server_name за симлинком защищён" grep -q 'already served' "$T/certs/status/linked.com"
rm -f "$T/nginx/sites-enabled/p.conf" "$T/nginx/conf.d/other.conf"

echo "== nginx отверг конфигурацию → конфиг убран, статус error"
reset
echo 203.0.113.10 >"$T/dns/broken.com"
touch "$T/nginx.fail"
enqueue issue-broken.com
run
check "статус error" grep -q 'nginx rejected' "$T/certs/status/broken.com"
check "конфиг убран" test ! -e "$T/nginx/vladhost-domains/broken.com.conf"

echo "== certbot не смог → статус error, настройка nginx остаётся для повтора"
reset
echo 203.0.113.10 >"$T/dns/certfail.com"
touch "$T/certbot.fail"
enqueue issue-certfail.com
run
check "статус error" grep -q '^error:' "$T/certs/status/certfail.com"
check "текст ошибки certbot" grep -q 'challenges have failed' "$T/certs/status/certfail.com"
check "без служебных строк certbot" test -z "$(grep -E 'Saving debug log' "$T/certs/status/certfail.com")"
check "конфиг остался" test -f "$T/nginx/vladhost-domains/certfail.com.conf"

echo "== удаление: конфиг, сертификат и ключ убраны"
reset
echo 203.0.113.10 >"$T/dns/gone.com"
enqueue issue-gone.com
run
check "домен создан" test -f "$T/nginx/vladhost-domains/gone.com.conf"
store=$(readlink "$T/vhcerts/gone.com")
enqueue delete-gone.com
run
check "конфиг удалён" test ! -e "$T/nginx/vladhost-domains/gone.com.conf"
check "ссылка на сертификат удалена" test ! -e "$T/vhcerts/gone.com"
check "каталог с закрытым ключом удалён" test ! -e "$store"
check "certbot delete вызван" grep -q 'certbot delete --cert-name gone.com' "$LOG"

echo "== недопустимые имена не доходят ни до certbot, ни до nginx"
reset
for evil in 'issue-vladinc.ru' 'issue-app.vladinc.ru' 'issue-evil.vladinc.ru.com.' 'issue-localhost' 'issue-a b.com' 'issue-x;y.com' 'issue-$(id).com' 'issue-a.com;rm' 'issue-..' 'issue-.com' 'issue-com' 'issue-UPPER.com' 'issue-a_b.com' 'delete-vladinc.ru' 'delete-app.vladinc.ru' 'issue-123.456' 'issue-xn--.com'; do
    enqueue "$evil"
done
run
check "certbot не вызывался" test -z "$(grep certbot "$LOG")"
check "nginx-конфиги не созданы" test -z "$(ls "$T/nginx/vladhost-domains" 2>/dev/null)"
check "очередь очищена" test -z "$(ls "$T/certs/queue")"
check "недопустимые имена пропущены" grep -q 'skip: недопустимое имя' "$T/out.log"
check "команды из имени не выполнены" test ! -e "$T/rm" -a ! -e "$T/x"

echo "== адрес сайта (старый путь) продолжает работать"
reset
enqueue issue-blog.john.vladinc.ru
run
check "статус ok" test "$(status blog.john.vladinc.ru)" = ok
check "nginx-конфиг для адреса сайта не создаётся" test -z "$(ls "$T/nginx/vladhost-domains" 2>/dev/null)"
enqueue delete-blog.john.vladinc.ru
run
check "удаление адреса сайта" test ! -e "$T/vhcerts/blog.john.vladinc.ru"

echo "== без настроенных IP сервера домены не выпускаются"
reset
echo 'SERVER_IPS=""' >"$T/certs.conf"
echo 203.0.113.10 >"$T/dns/noips.com"
enqueue issue-noips.com
run
check "отказ без SERVER_IPS" grep -q 'not configured' "$T/certs/status/noips.com"

echo
echo "успешно: $pass, провалов: $fail"
[ "$fail" -eq 0 ]

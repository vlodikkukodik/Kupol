#!/bin/bash
# Собственный DNS (ns.vladinc.ru и ns2.vladinc.ru на одном IP) на уже установленном BIND (Ubuntu 24.04), от root. Идемпотентна.
#
#   * зоны пользователей лежат отдельно: /var/lib/bind/vladhost/*.zone, перечень зон — /etc/bind/named.conf.vladhost, подключается одной строкой include
#     из named.conf.local; собирает их исполнитель (deploy/bin/dns-sync.py) по файлу панели, каждую зону проверяет named-checkzone до подмены;
#   * рекурсивные запросы разрешены только с самого сервера (иначе публичный BIND стал бы открытым резолвером), ответы ограничены по частоте (rate-limit),
#     версия и лишние сведения не раскрываются; всё остальное в настройках BIND (в том числе прежние зоны) не меняется;
#   * ns.vladinc.ru и ns2.vladinc.ru у вас уже указывают на этот сервер; домену клиента нужно лишь в NS-записях у регистратора указать оба имени.
# Перед изменениями настройки сохраняются в /root/vladhost-dns-backup (там же rollback-dns.sh); при сбое откат выполняется сам.
set -euo pipefail

SRC=$(cd "$(dirname "$0")/.." && pwd)
ENVF=/etc/vladhost/env
BACKUP=/root/vladhost-dns-backup
NS1=${VLADHOST_NS1:-ns.vladinc.ru}
NS2=${VLADHOST_NS2:-ns2.vladinc.ru}
BASE=${VLADHOST_BASE_DOMAIN:-vladinc.ru}
[ -f "$ENVF" ] || { echo "нет $ENVF: сначала deploy/prepare.sh" >&2; exit 1; }
grep -q '^VLADHOST_RUNTIME_DIR=' "$ENVF" || { echo "нет VLADHOST_RUNTIME_DIR: сначала deploy/prepare-runtime.sh" >&2; exit 1; }
RT=$(grep '^VLADHOST_RUNTIME_DIR=' "$ENVF" | cut -d= -f2-)
export DEBIAN_FRONTEND=noninteractive
command -v named >/dev/null || { apt-get update -qq; apt-get install -y -qq bind9 bind9-utils bind9-dnsutils >/dev/null; }
command -v named-checkzone >/dev/null && command -v named-checkconf >/dev/null && command -v rndc >/dev/null || { apt-get install -y -qq bind9-utils >/dev/null; }
command -v python3 >/dev/null || { echo "нужен python3" >&2; exit 1; }

# --- копия настроек и откат ---
install -d -m 0700 "$BACKUP"
[ -f "$BACKUP/etc-bind.tar.gz" ] || tar -czf "$BACKUP/etc-bind.tar.gz" -C / etc/bind
cat >"$BACKUP/rollback-dns.sh" <<'EOF'
#!/bin/bash
# Возвращает настройки BIND к состоянию до prepare-dns.sh: зоны Vladhost перестают обслуживаться, прежняя настройка возвращается из архива.
set -e
tar -xzf /root/vladhost-dns-backup/etc-bind.tar.gz -C / etc/bind
rm -f /etc/bind/named.conf.vladhost
named-checkconf
rndc reconfig
echo "откат выполнен: настройки BIND как до prepare-dns.sh"
EOF
chmod 0700 "$BACKUP/rollback-dns.sh"
fail() { echo "$1" >&2; "$BACKUP/rollback-dns.sh" || true; exit 1; }

# --- исполнитель и каталоги ---
install -d -m 0755 /usr/local/lib/vladhost
install -m 0755 "$SRC/deploy/bin/dns-sync.py" "$SRC/deploy/bin/runtime.sh" /usr/local/lib/vladhost/
install -d -o root -g bind -m 0755 /var/lib/bind/vladhost
install -d -o vladhost -g vladhost -m 0750 "$RT/dns"
[ -f /etc/bind/named.conf.vladhost ] || { echo "// Зоны Vladhost: файл создаёт dns-sync.py" >/etc/bind/named.conf.vladhost; }
chown root:bind /etc/bind/named.conf.vladhost
chmod 0644 /etc/bind/named.conf.vladhost

# --- подключение перечня зон ---
grep -q 'named.conf.vladhost' /etc/bind/named.conf.local || printf '\n// Зоны клиентов Vladhost (собственный DNS)\ninclude "/etc/bind/named.conf.vladhost";\n' >>/etc/bind/named.conf.local

# --- безопасные настройки: рекурсия только для самого сервера, ограничение частоты ответов ---
if ! grep -q 'vladhost-dns' /etc/bind/named.conf.options; then
    python3 - <<'PY'
import re
p = "/etc/bind/named.conf.options"
s = open(p).read()
block = """
\t// --- vladhost-dns: публичный сервер имён, а не открытый резолвер ---
\tallow-recursion { 127.0.0.0/8; ::1; };
\tallow-query-cache { 127.0.0.0/8; ::1; };
\tallow-transfer { none; };
\tversion "none";
\tminimal-responses yes;
\trate-limit { responses-per-second 20; window 5; };
\t// --- конец vladhost-dns ---
"""
m = re.search(r"options\s*\{", s)
if not m:
    raise SystemExit("в named.conf.options нет блока options")
open(p, "w").write(s[: m.end()] + block + s[m.end():])
PY
fi
named-checkconf || fail "named-checkconf не принял настройки: откат выполнен"

# --- пустое состояние и первая синхронизация ---
[ -f "$RT/dns/state.json" ] || {
    printf '{"version":1,"nameservers":["%s","%s"],"hostmaster":"hostmaster.%s","zones":[]}\n' "$NS1" "$NS2" "$BASE" >"$RT/dns/state.json"
    chown vladhost:vladhost "$RT/dns/state.json"
    chmod 0640 "$RT/dns/state.json"
}
systemctl reload named 2>/dev/null || systemctl restart named
sleep 1
systemctl is-active --quiet named || fail "BIND не поднялся: откат выполнен"
python3 /usr/local/lib/vladhost/dns-sync.py --state "$RT/dns/state.json" | grep -qx ok || fail "dns-sync.py не отработал: откат выполнен"

# --- самопроверка на временной зоне (только если у панели ещё нет своих зон) ---
if python3 - "$RT/dns/state.json" <<'PY'
import json, sys
sys.exit(0 if not json.load(open(sys.argv[1])).get("zones") else 1)
PY
then
    T=$(mktemp -d)
    trap 'rm -rf "$T"' EXIT
    cat >"$T/state.json" <<EOF
{"version":1,"nameservers":["$NS1","$NS2"],"hostmaster":"hostmaster.$BASE","zones":[{"name":"vh-selftest.example","serial":1,"records":[{"name":"@","type":"A","value":"203.0.113.77","ttl":60},{"name":"@","type":"MX","priority":10,"value":"mail.$BASE","ttl":60}]}]}
EOF
    python3 /usr/local/lib/vladhost/dns-sync.py --state "$T/state.json" | grep -qx ok || fail "самопроверка: зона не собралась: откат выполнен"
    sleep 1
    IP=$(hostname -I | awk '{print $1}')
    got=$(dig +short +time=3 +tries=1 @127.0.0.1 vh-selftest.example A)
    [ "$got" = 203.0.113.77 ] || { python3 /usr/local/lib/vladhost/dns-sync.py --state "$RT/dns/state.json" >/dev/null; fail "самопроверка: BIND не отвечает по зоне (получено «$got»)"; }
    pub=$(dig +short +time=3 +tries=1 @"$IP" vh-selftest.example MX)
    echo "$pub" | grep -q "mail.$BASE" || echo "ВНИМАНИЕ: по адресу $IP зона не отвечает: $pub" >&2
    # чужие имена без рекурсии: с публичного адреса резолвером сервер быть не должен
    rec=$(dig +time=3 +tries=1 @"$IP" example.org A 2>/dev/null | grep -c 'status: REFUSED' || true)
    [ "$rec" -ge 1 ] || echo "ВНИМАНИЕ: рекурсия через $IP не отклоняется (с самого сервера это допустимо: разрешён весь loopback и локальные адреса не считаются)" >&2
    python3 /usr/local/lib/vladhost/dns-sync.py --state "$RT/dns/state.json" | grep -qx ok
    [ -z "$(dig +short +time=3 +tries=1 @127.0.0.1 vh-selftest.example A)" ] || fail "временная зона не убрана"
fi

# --- настройки панели ---
grep -q '^VLADHOST_DNS_NS=' "$ENVF" || echo "VLADHOST_DNS_NS=$NS1,$NS2" >>"$ENVF"
chown root:vladhost "$ENVF"
chmod 0640 "$ENVF"

echo "готово: BIND $(systemctl is-active named), серверы имён $NS1 и $NS2 (один IP), откат: $BACKUP/rollback-dns.sh"

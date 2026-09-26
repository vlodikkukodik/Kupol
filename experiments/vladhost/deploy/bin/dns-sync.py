#!/usr/bin/env python3
"""Собственный DNS: зоны пользователей для BIND по файлу состояния панели (root, вызывается runtime.sh, действие dns-sync).

dns-sync.py --state /var/lib/vladhost/runtime/dns/state.json
Состояние: {"version":1,"nameservers":[…],"hostmaster":"…","zones":[{"name","serial","records":[{"name","type","value","priority","ttl"}]}]}.
Панели не верим: всё проверяется здесь строго (те же правила, что в backend/internal/dnszones/validate.go), и пока не проверено ВСЁ,
на диске не меняется ничего. Каждая зона перед подменой проходит named-checkzone. BIND трогается только если что-то изменилось:
rndc reconfig — когда изменился перечень зон, rndc reload ЗОНА — для зон с новым содержимым.
Печатает «ok» или «error=код» (код 2 — состояние отвергнуто).
"""
import ipaddress
import json
import os
import re
import shutil
import subprocess
import sys

ZONES = "/var/lib/bind/vladhost"
CONF = "/etc/bind/named.conf.vladhost"
TYPES = ("A", "AAAA", "CNAME", "MX", "TXT", "SRV", "CAA")
TTLS = (60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 43200, 86400)
MAX_RECORDS = 200
MAX_TXT = 2048
MAX_NAME = 253

LABEL = r"[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?"
DOMAIN_RE = re.compile(r"^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$")
NAME_RE = re.compile(r"^(\*\.)?" + LABEL + r"(\." + LABEL + r")*$")
HOST_RE = re.compile(r"^([a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$")
NS_RE = re.compile(r"^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$")
CTRL = re.compile(r"[\x00-\x1f\x7f]")


class Reject(Exception):
    """Состояние отвергнуто; code печатается как error=code."""

    def __init__(self, code):
        super().__init__(code)
        self.code = code


def is_int(v, lo, hi):
    return isinstance(v, int) and not isinstance(v, bool) and lo <= v <= hi


def txt_quote(s):
    """Значение TXT в кавычках: \\ и " экранируются, не-ASCII — десятичными кодами байтов, строка режется по 255 байт."""
    raw = s.encode("utf-8")
    chunks = [raw[i:i + 255] for i in range(0, len(raw), 255)] or [b""]
    out = []
    for ch in chunks:
        buf = []
        for b in ch:
            if b in (0x22, 0x5C):
                buf.append("\\" + chr(b))
            elif 32 <= b < 127:
                buf.append(chr(b))
            else:
                buf.append("\\%03d" % b)
        out.append('"' + "".join(buf) + '"')
    return " ".join(out)


def absolute(host):
    return host if host.endswith(".") else host + "."


def check_host(v, code="record_value"):
    if not isinstance(v, str):
        raise Reject(code)
    h = v[:-1] if v.endswith(".") else v
    if len(h) > MAX_NAME or not HOST_RE.match(h):
        raise Reject(code)
    return h


def render_value(rec):
    """Данные записи в зонном файле; неверное значение → Reject."""
    typ, val = rec["type"], rec["value"]
    if not isinstance(val, str):
        raise Reject("record_value")
    if typ == "A":
        try:
            ip = ipaddress.ip_address(val)
        except ValueError:
            raise Reject("record_value")
        if ip.version != 4 or ip.is_unspecified or ip.is_multicast or str(ip) != val:
            raise Reject("record_value")
        return val
    if typ == "AAAA":
        try:
            ip = ipaddress.ip_address(val)
        except ValueError:
            raise Reject("record_value")
        if ip.version != 6 or ip.ipv4_mapped or ip.is_unspecified or ip.is_multicast:
            raise Reject("record_value")
        return str(ip)
    if typ == "CNAME":
        return absolute(check_host(val))
    if typ == "MX":
        return "%d %s" % (rec["priority"], absolute(check_host(val)))
    if typ == "TXT":
        if not val or len(val) > MAX_TXT or CTRL.search(val):
            raise Reject("record_value")
        return txt_quote(val)
    if typ == "SRV":
        parts = val.split(" ")
        if len(parts) != 3 or not all(re.fullmatch(r"0|[1-9][0-9]{0,4}", p) for p in parts[:2]):
            raise Reject("record_value")
        weight, port = int(parts[0]), int(parts[1])
        if weight > 65535 or not 1 <= port <= 65535:
            raise Reject("record_value")
        target = "." if parts[2] == "." else absolute(check_host(parts[2]))
        return "%d %d %d %s" % (rec["priority"], weight, port, target)
    if typ == "CAA":
        parts = val.split(" ", 2)
        if len(parts) != 3 or parts[0] not in ("0", "128") or parts[1] not in ("issue", "issuewild", "iodef"):
            raise Reject("record_value")
        v = parts[2].strip()
        if not v or len(v) > 255 or " " in v or CTRL.search(v):
            raise Reject("record_value")
        return '%s %s "%s"' % (parts[0], parts[1], v.replace("\\", "\\\\").replace('"', '\\"'))
    raise Reject("record_type")


def validate(st):
    """Проверяет всё состояние; возвращает (nameservers, hostmaster, зоны) для отрисовки. Ничего не пишет."""
    if not isinstance(st, dict) or st.get("version") != 1:
        raise Reject("version")
    ns = st.get("nameservers")
    if not isinstance(ns, list) or not ns or not all(isinstance(n, str) and NS_RE.match(n) for n in ns):
        raise Reject("nameservers")
    hm = st.get("hostmaster")
    if not isinstance(hm, str) or not NS_RE.match(hm):
        raise Reject("hostmaster")
    zones = st.get("zones")
    if not isinstance(zones, list):
        raise Reject("zone_name")
    seen, out = set(), []
    for z in zones:
        if not isinstance(z, dict):
            raise Reject("zone_name")
        name = z.get("name")
        if not isinstance(name, str) or len(name) > MAX_NAME or not DOMAIN_RE.match(name) or name in seen:
            raise Reject("zone_name")
        seen.add(name)
        if not is_int(z.get("serial"), 1, 4294967295):
            raise Reject("serial")
        recs = z.get("records")
        if not isinstance(recs, list):
            raise Reject("record_type")
        if len(recs) > MAX_RECORDS:
            raise Reject("too_many")
        lines, keys, by_name = [], set(), {}
        for r in recs:
            if not isinstance(r, dict):
                raise Reject("record_type")
            typ = r.get("type")
            if typ not in TYPES:
                raise Reject("record_type")
            rn = r.get("name")
            if not isinstance(rn, str) or (rn != "@" and (not NAME_RE.match(rn) or ".." in rn or len(rn) + len(name) >= MAX_NAME)):
                raise Reject("record_name")
            if not is_int(r.get("ttl"), 1, 86400) or r["ttl"] not in TTLS:
                raise Reject("record_ttl")
            if typ in ("MX", "SRV") and not is_int(r.get("priority"), 0, 65535):
                raise Reject("record_priority")
            if typ == "CNAME" and rn == "@":
                raise Reject("record_cname")
            data = render_value(r)
            key = (rn, typ, data)
            if key in keys:
                raise Reject("record_dup")
            keys.add(key)
            by_name.setdefault(rn, []).append(typ)
            lines.append("%s %d IN %s %s" % (rn, r["ttl"], typ, data))
        for rn, types in by_name.items():
            if "CNAME" in types and len(types) > 1:
                raise Reject("record_cname")  # CNAME не уживается ни с чем на том же имени, в том числе с другим CNAME
        out.append((name, z["serial"], lines))
    return ns, hm, out


def render_zone(name, serial, ns, hm, lines):
    head = [
        "$ORIGIN %s." % name,
        "$TTL 300",
        "@ IN SOA %s. %s. %d 3600 900 1209600 300" % (ns[0], hm, serial),
    ]
    head += ["@ IN NS %s." % n for n in ns]
    return "\n".join(head + lines) + "\n"


def render_conf(zones_dir, names):
    out = ["// Зоны Vladhost (собственный DNS). Файл создаёт dns-sync.py — руками не править.\n"]
    for n in sorted(names):
        out.append('zone "%s" { type primary; file "%s/%s.zone"; allow-transfer { none; }; allow-update { none; }; notify no; };\n' % (n, zones_dir, n))
    return "".join(out)


def read(path):
    try:
        with open(path, encoding="utf-8") as f:
            return f.read()
    except OSError:
        return None


def write_atomic(path, text, mode, gid):
    tmp = os.path.join(os.path.dirname(path), ".tmp." + os.path.basename(path))
    with open(tmp, "w", encoding="utf-8") as f:
        f.write(text)
    os.chmod(tmp, mode)
    if gid is not None:
        os.chown(tmp, 0, gid)
    os.replace(tmp, path)


def run(cmd):
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=60, check=False)
    return r.returncode == 0


def arg_value(argv, name, default):
    return argv[argv.index(name) + 1] if name in argv and argv.index(name) + 1 < len(argv) else default


def main(argv):
    state_path = arg_value(argv, "--state", "")
    zones_dir = arg_value(argv, "--zones", ZONES)
    conf_path = arg_value(argv, "--conf", CONF)
    checkzone = arg_value(argv, "--checkzone", "named-checkzone")
    checkconf = arg_value(argv, "--checkconf", "named-checkconf")
    group = arg_value(argv, "--group", "bind")
    rndc = arg_value(argv, "--rndc", "rndc")
    try:
        try:
            with open(state_path, encoding="utf-8") as f:
                st = json.load(f)
        except (OSError, ValueError):
            raise Reject("json")
        ns, hm, zones = validate(st)
        gid = None
        if group:
            import grp

            try:
                gid = grp.getgrnam(group).gr_gid
            except KeyError:
                gid = None
        os.makedirs(zones_dir, exist_ok=True)

        # Новые файлы зон собираются во временной папке и проверяются named-checkzone до подмены.
        stage = os.path.join(zones_dir, ".check")
        shutil.rmtree(stage, ignore_errors=True)
        os.makedirs(stage)
        try:
            texts = {}
            for name, serial, lines in zones:
                text = render_zone(name, serial, ns, hm, lines)
                texts[name] = text
                if checkzone:
                    p = os.path.join(stage, name + ".zone")
                    with open(p, "w", encoding="utf-8") as f:
                        f.write(text)
                    if not run([checkzone, "-q", name, p]):
                        raise Reject("checkzone")
        finally:
            shutil.rmtree(stage, ignore_errors=True)

        changed = []
        for name, text in texts.items():
            path = os.path.join(zones_dir, name + ".zone")
            if read(path) != text:
                write_atomic(path, text, 0o644, gid)
                changed.append(name)
        # Зоны, которых больше нет в состоянии, убираются вместе с файлом.
        for fn in os.listdir(zones_dir):
            if fn.endswith(".zone") and fn[:-5] not in texts and DOMAIN_RE.match(fn[:-5]):
                os.remove(os.path.join(zones_dir, fn))
        conf_new = render_conf(zones_dir, list(texts))
        conf_old = read(conf_path)
        conf_changed = conf_old != conf_new
        if conf_changed:
            write_atomic(conf_path, conf_new, 0o644, gid)
            if checkconf and not run([checkconf]):
                if conf_old is not None:
                    write_atomic(conf_path, conf_old, 0o644, gid)
                raise Reject("checkconf")
        if rndc:
            if conf_changed:
                run([rndc, "reconfig"])
            for name in changed:
                run([rndc, "reload", name])
        print("ok")
        return 0
    except Reject as e:
        print("error=" + e.code)
        return 2
    except OSError:
        print("error=write")
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

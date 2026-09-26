#!/usr/bin/env python3
"""Журнал почты одного домена и его очередь отправки (root: журнал exim доступен только ему).

Запускается исполнителем сред выполнения (runtime.sh, действие mail-log): mail-log.py --domain example.com [--limit 100].
Печатает JSON {"events": [...], "queue": [...]}. События: принято (received), доставлено (delivered), не доставлено (failed),
отложено (deferred), отклонено (rejected). Клиент видит только письма своего домена: строка попадает в ответ, только если
в ней (или в другой строке того же письма) есть адрес этого домена; сравнение идёт по домену целиком.
При неверных аргументах: «error=bad_args», код 2.
"""
import json
import os
import re
import shlex
import subprocess
import sys

LOG = "/var/log/exim4/mainlog"
QUEUE_CMD = "exim4 -bp"
MAX_LIMIT = 120
DEFAULT_LIMIT = 100
TAIL_BYTES = 8 << 20  # с большого журнала берём только хвост

DOMAIN_RE = re.compile(r"^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$")
MSGID = r"[0-9A-Za-z]{6}-[0-9A-Za-z]{6,11}-[0-9A-Za-z]{2,4}"
TS = re.compile(r"^(\d{4}-\d\d-\d\d \d\d:\d\d:\d\d) (.*)$")
MSG = re.compile(r"^(" + MSGID + r") (.*)$")
CTRL = re.compile(r"[\x00-\x1f\x7f]")
HOST = re.compile(r"\bH=(\S+)(?: \([^)]*\))? (\[[^\]]+\])")
ADDR = re.compile(r"<([^<>\s]*)>")


def clean(s, n=300):
    """Строка без управляющих символов (журнал пишут посторонние: SMTP-ответы, имена узлов) и не длиннее n."""
    return CTRL.sub(" ", s).strip()[:n]


def domain_of(addr):
    return addr.rsplit("@", 1)[1].lower() if "@" in addr else ""


def host_of(text):
    m = HOST.search(text)
    return f"{m.group(1)} {m.group(2)}" if m else ""


def parse_line(line, ctx):
    """Одна строка журнала → событие или None. ctx: номер письма → откуда оно пришло (запоминается по строкам «<=»)."""
    m = TS.match(line.rstrip("\n"))
    if not m:
        return None
    t, rest = m.groups()
    mid = MSG.match(rest)
    if not mid:
        return parse_rejected(t, rest)
    msg, body = mid.groups()
    ev = {"t": t, "kind": "", "id": msg, "from": "", "to": "", "host": "", "via": "", "detail": ""}

    if body.startswith("<= "):
        m2 = re.match(r"<= (\S+)(.*)$", body)
        sender = m2.group(1)
        sender = "" if sender == "<>" else sender.strip("<>")
        tail = m2.group(2)
        host = host_of(tail)
        ip = re.search(r"\[([^\]]+)\]", host)
        if " A=" in tail:
            via = "auth"
        elif not host or (ip and ip.group(1) in ("127.0.0.1", "::1")):
            via = "local"
        else:
            via = "remote"
        rcpts = re.search(r" for (.+)$", tail)
        rl = rcpts.group(1).split() if rcpts else []
        ctx[msg] = {"from": sender, "host": host, "via": via}
        ev.update(kind="received", **{"from": sender}, to=", ".join(rl), host=host, via=via)
        ev["_rcpts"] = rl
        return ev

    src = ctx.get(msg, {})
    ev["from"] = src.get("from", "")
    ev["_known"] = msg in ctx

    m2 = re.match(r"(=>|->) (\S+)(?: <([^>]*)>)?(.*)$", body)
    if m2:
        target, orig, tail = m2.group(2), m2.group(3), m2.group(4)
        to = orig if orig else target
        t_ = re.search(r"\bT=(\S+)", tail)
        via = {"vladhost_lmtp": "mailbox", "remote_smtp": "remote"}.get(t_.group(1), t_.group(1)) if t_ else ""
        c = re.search(r'\bC="(.*)"', tail)
        ev.update(kind="delivered", to=to, host=host_of(tail), via=via, detail=clean(c.group(1)) if c else "")
        ev["_rcpts"] = [to]
        return ev

    m2 = re.match(r"== (\S+)(?: <[^>]*>)?(.*?) defer \((-?\d+)\): (.*)$", body)
    if m2:
        detail = m2.group(4)
        h = re.search(r"host (\S+) (\[[^\]]+\])", detail)
        ev.update(kind="deferred", to=m2.group(1), host=f"{h.group(1)} {h.group(2)}" if h else "", detail=clean(detail))
        ev["_rcpts"] = [m2.group(1)]
        return ev

    m2 = re.match(r"\*\* (\S+)\s+(?:<[^>]*>\s+)?(?:R=\S+\s+)?(?:T=([^\s:]+))?:?\s*(.*)$", body)
    if m2:
        ev.update(kind="failed", to=m2.group(1), detail=clean(m2.group(3)))
        ev["_rcpts"] = [m2.group(1)]
        return ev
    return None


def parse_rejected(t, rest):
    """Отказ в SMTP-сессии: у строки нет номера письма, адреса берутся из самой строки."""
    m = re.search(r"\brejected (RCPT|MAIL|after DATA|[A-Za-z ]+?)(?: <([^>]*)>)?: (.*)$", rest)
    if not m or not rest.startswith("H="):
        return None
    what, addr, detail = m.groups()
    frm = re.search(r"\bF=<([^>]*)>", rest)
    sender = frm.group(1) if frm else ""
    to = ""
    if what == "RCPT":
        to = addr or ""
    elif what == "MAIL":
        sender = addr or sender
    else:
        d = re.search(r"\bto (\S+)$", detail)
        to = d.group(1) if d else ""
    ev = {"t": t, "kind": "rejected", "id": "", "from": sender, "to": to, "host": host_of(rest), "via": "", "detail": clean(detail)}
    ev["_rcpts"] = [to] if to else []
    ev["_known"] = True
    return ev


def events_for(domain, lines, limit):
    """События домена, свежие сверху (не больше limit)."""
    ctx, evs = {}, []
    for line in lines:
        try:
            ev = parse_line(line, ctx)
        except (ValueError, IndexError):
            continue
        if ev:
            evs.append(ev)
    # Письмо «принадлежит» домену, если хоть в одной его строке есть адрес домена (отправитель или получатель).
    doms = {}
    for ev in evs:
        if ev["id"]:
            s = doms.setdefault(ev["id"], set())
            s.add(domain_of(ev["from"]))
            s.update(domain_of(a) for a in ev["_rcpts"])
    out = []
    for ev in evs:
        mine = [a for a in ev["_rcpts"] if domain_of(a) == domain]
        from_me = domain_of(ev["from"]) == domain
        if ev["kind"] == "rejected":
            ok = from_me or bool(mine)
        else:
            ok = domain in doms.get(ev["id"], ()) and (from_me or bool(mine) or not ev["_known"] and not ev["from"])
        if not ok:
            continue
        if ev["kind"] == "received" and not from_me:
            ev["to"] = ", ".join(mine)  # получатели других доменов в одном письме клиент не видит
        out.append(ev)
    out.reverse()  # при равном времени сверху оказывается более поздняя строка журнала (сортировка устойчивая)
    out.sort(key=lambda e: e["t"], reverse=True)
    out = out[:limit]
    return [{k: v for k, v in e.items() if not k.startswith("_")} for e in out]


def parse_queue(text, domain):
    """Вывод «exim -bp» → письма из очереди, где отправитель или получатель — домен клиента."""
    items = []
    for block in re.split(r"\n\s*\n", text.strip("\n")):
        lines = [ln for ln in block.split("\n") if ln.strip()]
        if not lines:
            continue
        m = re.match(r"^\s*(\S+)\s+(\S+)\s+(" + MSGID + r")\s+<([^>]*)>(.*)$", lines[0])
        if not m:
            continue
        age, size, msg, sender, tail = m.groups()
        rcpts = []
        for ln in lines[1:]:
            s = ln.strip()
            if s.startswith("D "):  # уже доставлено
                continue
            rcpts.append(clean(s, 200))
        if domain_of(sender) != domain and not any(domain_of(a) == domain for a in rcpts):
            continue
        items.append({"id": msg, "age": age, "size": size, "from": clean(sender, 200), "to": rcpts, "frozen": "frozen" in tail})
    return items


def read_tail(path):
    try:
        with open(path, "rb") as f:
            size = os.fstat(f.fileno()).st_size
            if size > TAIL_BYTES:
                f.seek(size - TAIL_BYTES)
                f.readline()  # обрезанную первую строку не разбираем
            return f.read().decode("utf-8", "replace").splitlines()
    except OSError:
        return []


def run_queue(cmd):
    try:
        r = subprocess.run(shlex.split(cmd), capture_output=True, text=True, timeout=15, check=False)
        return r.stdout if r.returncode == 0 else ""
    except (OSError, subprocess.SubprocessError, ValueError):
        return ""


def bad_args():
    print("error=bad_args")
    return 2


def main(argv):
    opts = {"--domain": "", "--limit": str(DEFAULT_LIMIT), "--log": LOG, "--queue-cmd": QUEUE_CMD}
    i = 0
    while i < len(argv):
        if argv[i] not in opts or i + 1 >= len(argv):
            return bad_args()
        opts[argv[i]] = argv[i + 1]
        i += 2
    domain = opts["--domain"]
    if len(domain) > 253 or not DOMAIN_RE.match(domain):
        return bad_args()
    if not re.fullmatch(r"[1-9][0-9]{0,2}", opts["--limit"]) or int(opts["--limit"]) > MAX_LIMIT:
        return bad_args()
    limit = int(opts["--limit"])

    lines = read_tail(opts["--log"])
    events = events_for(domain, lines, limit)
    if len(events) < limit:  # не хватило — берём и прошлый журнал (после ротации)
        older = read_tail(opts["--log"] + ".1")
        if older:
            events = events_for(domain, older + lines, limit)
    queue = parse_queue(run_queue(opts["--queue-cmd"]), domain)
    print(json.dumps({"events": events, "queue": queue}, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

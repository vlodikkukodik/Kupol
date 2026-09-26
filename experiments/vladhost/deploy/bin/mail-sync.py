#!/usr/bin/env python3
"""Почта на своих доменах: файлы для exim и dovecot по состоянию панели (root; вызывается runtime.sh, действие mail-sync).

mail-sync.py --state /var/lib/vladhost/runtime/mail/state.json --usage /var/lib/vladhost/runtime/mail/usage.json
Состояние: {"version":1,"domains":[{name, enabled, dkim:{selector, private_key}, mailboxes:[{local, hash, quota_mb, enabled,
autoreply, forward}], aliases:[{local, to:[…]}]}], "purge":["домен" | "домен/ящик"]}.

Пишет в /etc/vladhost/mail: domains, mailboxes, aliases (списки для exim), dovecot-users (ящики для dovecot), dkim/{домен}.key,
а в /var/vmail — папки ящиков, sieve-panel/panel.sieve (автоответчик и пересылка) и удаляет то, что велено в purge.
Панели не верим: пока строго не проверено ВСЁ состояние, на диске не меняется ничего. Файлы переписываются только при изменении.
Печатает «ok» или «error=код»; код 2 — состояние отвергнуто. Размеры ящиков пишутся в usage.json (симлинки не считаются).
"""
import datetime
import json
import os
import re
import shutil
import sys

MAIL_DIR = "/etc/vladhost/mail"
VMAIL_DIR = "/var/vmail"
VMAIL_UID = VMAIL_GID = 5000

DOMAIN_RE = re.compile(r"^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$")
LOCAL_RE = re.compile(r"^[a-z0-9]([a-z0-9._+-]{0,62}[a-z0-9])?$")
HASH_RE = re.compile(r"^\{SHA512-CRYPT\}\$6\$[./0-9A-Za-z]{1,16}\$[./0-9A-Za-z]{86}$")
SELECTOR_RE = re.compile(r"^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$")
PEM_RE = re.compile(r"^-----BEGIN (RSA )?PRIVATE KEY-----\n([A-Za-z0-9+/=]{1,200}\n){1,100}-----END (RSA )?PRIVATE KEY-----\n?$")
EMAIL_RE = re.compile(r"^[A-Za-z0-9._%+-]{1,64}@([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$")
DATE_RE = re.compile(r"^\d{4}-\d\d-\d\d$")
CTRL = re.compile(r"[\x00-\x1f\x7f]")
MIN_QUOTA, MAX_QUOTA = 10, 102400
MAX_REDIRECTS = 5  # столько же разрешает sieve_max_redirects в dovecot


class Reject(Exception):
    def __init__(self, code):
        super().__init__(code)
        self.code = code


def is_int(v, lo, hi):
    return isinstance(v, int) and not isinstance(v, bool) and lo <= v <= hi


# ---------- проверка состояния ----------

def valid_email(a):
    return isinstance(a, str) and len(a) <= 254 and bool(EMAIL_RE.match(a))


def check_autoreply(ar):
    """Настройки автоответчика → словарь для sieve или None (выключен)."""
    if ar is None:
        return None
    if not isinstance(ar, dict):
        raise Reject("autoreply")
    if not isinstance(ar.get("enabled"), bool):
        raise Reject("autoreply")
    if not ar["enabled"]:
        return None
    subj, body = ar.get("subject"), ar.get("body")
    if not isinstance(subj, str) or not subj.strip() or len(subj) > 200 or CTRL.search(subj):
        raise Reject("autoreply")
    if not isinstance(body, str) or not body.strip() or len(body) > 2000 or re.search(r"[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]", body):
        raise Reject("autoreply")
    dates = {}
    for k in ("from", "to"):
        v = ar.get(k) or ""
        if v:
            if not isinstance(v, str) or not DATE_RE.match(v):
                raise Reject("autoreply")
            try:
                datetime.date.fromisoformat(v)
            except ValueError:
                raise Reject("autoreply")
            dates[k] = v
    if "from" in dates and "to" in dates and dates["from"] > dates["to"]:
        raise Reject("autoreply")
    days = ar.get("days", 1)
    if not is_int(days, 1, 30):
        raise Reject("autoreply")
    return {"subject": subj.strip(), "body": body.replace("\r\n", "\n").replace("\r", "\n"), "days": days, **dates}


def check_forward(fw, own):
    """Пересылка → {"to": [...], "keep_copy": bool} или None."""
    if fw is None:
        return None
    if not isinstance(fw, dict):
        raise Reject("forward")
    to = fw.get("to") or []
    if not isinstance(to, list):
        raise Reject("forward")
    keep = fw.get("keep_copy", True)
    if not isinstance(keep, bool):
        raise Reject("forward")
    uniq = []
    for a in to:
        if not valid_email(a):
            raise Reject("forward")
        a = a.lower()
        if a == own:
            raise Reject("forward")  # пересылка самому себе — петля
        if a not in uniq:
            uniq.append(a)
    if len(uniq) > MAX_REDIRECTS:
        raise Reject("forward")
    return {"to": uniq, "keep_copy": keep} if uniq else None


def validate(st):
    if not isinstance(st, dict) or st.get("version") != 1:
        raise Reject("version")
    doms = st.get("domains")
    if not isinstance(doms, list):
        raise Reject("domain_name")
    seen, out = set(), []
    for d in doms:
        if not isinstance(d, dict):
            raise Reject("domain_name")
        name = d.get("name")
        if not isinstance(name, str) or len(name) > 253 or not DOMAIN_RE.match(name) or name in seen:
            raise Reject("domain_name")
        seen.add(name)
        if not isinstance(d.get("enabled"), bool):
            raise Reject("domain_name")
        dk = d.get("dkim")
        dkim = None
        if dk is not None:
            if not isinstance(dk, dict):
                raise Reject("dkim")
            pem = dk.get("private_key") or ""
            if pem:  # без ключа (ещё не создан) DKIM просто нет
                sel = dk.get("selector")
                if not isinstance(sel, str) or not SELECTOR_RE.match(sel) or not isinstance(pem, str) or not PEM_RE.match(pem):
                    raise Reject("dkim")
                dkim = pem
        boxes, locals_ = [], set()
        for b in d.get("mailboxes") or []:
            if not isinstance(b, dict):
                raise Reject("mailbox_local")
            local = b.get("local")
            if not isinstance(local, str) or not LOCAL_RE.match(local) or ".." in local or local in locals_:
                raise Reject("mailbox_local")
            locals_.add(local)
            if not isinstance(b.get("hash"), str) or not HASH_RE.match(b["hash"]):
                raise Reject("mailbox_hash")
            if not is_int(b.get("quota_mb"), MIN_QUOTA, MAX_QUOTA):
                raise Reject("mailbox_quota")
            if not isinstance(b.get("enabled"), bool):
                raise Reject("mailbox_local")
            own = f"{local}@{name}"
            boxes.append({"local": local, "hash": b["hash"], "quota": b["quota_mb"], "enabled": b["enabled"],
                          "autoreply": check_autoreply(b.get("autoreply")), "forward": check_forward(b.get("forward"), own)})
        aliases, alocals = [], set()
        for a in d.get("aliases") or []:
            if not isinstance(a, dict):
                raise Reject("alias_local")
            local = a.get("local")
            if local != "*" and (not isinstance(local, str) or not LOCAL_RE.match(local) or ".." in local):
                raise Reject("alias_local")
            if local in locals_ or local in alocals:
                raise Reject("alias_conflict")  # псевдоним не должен перекрывать ящик или другой псевдоним
            alocals.add(local)
            to = a.get("to")
            if not isinstance(to, list) or not to or not all(valid_email(x) for x in to):
                raise Reject("alias_dest")
            aliases.append({"local": local, "to": [x.lower() for x in to]})
        out.append({"name": name, "enabled": d["enabled"], "dkim": dkim, "boxes": boxes, "aliases": aliases})
    purge = st.get("purge") or []
    if not isinstance(purge, list):
        raise Reject("purge_path")
    paths = []
    for p in purge:
        if not isinstance(p, str):
            raise Reject("purge_path")
        parts = p.split("/")
        if len(parts) > 2 or not DOMAIN_RE.match(parts[0]) or (len(parts) == 2 and (not LOCAL_RE.match(parts[1]) or ".." in parts[1])):
            raise Reject("purge_path")
        paths.append(tuple(parts))
    return out, paths


# ---------- sieve ----------

def sieve_quote(s):
    return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'


def sieve_script(address, ar, fw):
    """Скрипт панели для ящика: пересылка и автоответчик; None, если нечего делать."""
    if not ar and not fw:
        return None
    need = []
    if fw and fw["keep_copy"]:
        need.append("copy")
    if ar:
        need.append("vacation")
        if "from" in ar or "to" in ar:
            need += ["date", "relational"]
    lines = ["# Создано панелью Vladhost: правила ящика (пересылка и автоответчик). Руками не править.",
             "require [" + ", ".join(sieve_quote(x) for x in need) + "];"]
    if fw:
        for a in fw["to"]:
            lines.append("redirect %s%s;" % (":copy " if fw["keep_copy"] else "", sieve_quote(a)))
    if ar:
        conds = []
        if "from" in ar:
            conds.append('currentdate :value "ge" "date" %s' % sieve_quote(ar["from"]))
        if "to" in ar:
            conds.append('currentdate :value "le" "date" %s' % sieve_quote(ar["to"]))
        body = "\n".join(("." + ln if ln.startswith(".") else ln) for ln in ar["body"].split("\n"))
        vac = "vacation :days %d :subject %s :addresses [%s] text:\n%s\n.\n;" % (ar["days"], sieve_quote(ar["subject"]), sieve_quote(address), body)
        if len(conds) == 1:
            lines.append("if %s {\n%s\n}" % (conds[0], vac))
        elif conds:
            lines.append("if allof (%s) {\n%s\n}" % (", ".join(conds), vac))
        else:
            lines.append(vac)
    return "\n".join(lines) + "\n"


# ---------- запись на диск ----------

class Disk:
    def __init__(self, chown):
        self.chown = chown

    def _group(self, name):
        if not self.chown or not name:
            return None
        try:
            import grp

            return grp.getgrnam(name).gr_gid
        except KeyError:
            return None

    def write(self, path, text, mode, group=None, owner=None):
        """Файл записывается только если содержимое изменилось (или права не те); подмена атомарная."""
        try:
            with open(path, encoding="utf-8") as f:
                same = f.read() == text
        except OSError:
            same = False
        if not same:
            tmp = os.path.join(os.path.dirname(path), ".tmp." + os.path.basename(path))
            fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, mode)
            with os.fdopen(fd, "w", encoding="utf-8") as f:
                f.write(text)
            os.chmod(tmp, mode)
            self._own(tmp, owner, group)
            os.replace(tmp, path)
        else:
            if os.stat(path).st_mode & 0o7777 != mode:
                os.chmod(path, mode)
            self._own(path, owner, group)

    def _own(self, path, uid, group):
        if not self.chown:
            return
        gid = self._group(group) if isinstance(group, str) else group
        try:
            os.lchown(path, uid if uid is not None else 0, gid if gid is not None else -1)
        except OSError:
            pass

    def mkdir(self, path, mode, owner_vmail):
        """Папка ящика: создаётся с нужными правами; принадлежит vmail, чтобы dovecot мог в ней работать."""
        os.makedirs(path, exist_ok=True)
        os.chmod(path, mode)
        if owner_vmail and self.chown:
            os.lchown(path, VMAIL_UID, VMAIL_GID)


def rmtree_safe(path):
    """Удаляет папку, не идя по ссылкам: ссылка убирается сама, её цель остаётся."""
    if os.path.islink(path):
        os.unlink(path)
    elif os.path.isdir(path):
        shutil.rmtree(path)


def mailbox_usage(box_dir):
    total = 0
    for root, dirs, files in os.walk(box_dir, followlinks=False):
        dirs[:] = [d for d in dirs if not os.path.islink(os.path.join(root, d))]
        for fn in files:
            p = os.path.join(root, fn)
            try:
                st = os.lstat(p)
            except OSError:
                continue
            if os.path.isfile(p) and not os.path.islink(p):
                total += st.st_size
    return total


def main(argv):
    def opt(name, default):
        return argv[argv.index(name) + 1] if name in argv and argv.index(name) + 1 < len(argv) else default

    state_path = opt("--state", "")
    mail_dir = opt("--mail-dir", MAIL_DIR)
    vmail = opt("--vmail-dir", VMAIL_DIR)
    usage_path = opt("--usage", "")
    disk = Disk("--no-chown" not in argv)
    try:
        try:
            with open(state_path, encoding="utf-8") as f:
                st = json.load(f)
        except (OSError, ValueError):
            raise Reject("json")
        doms, purge = validate(st)

        live = [d for d in doms if d["enabled"]]
        os.makedirs(os.path.join(mail_dir, "dkim"), exist_ok=True)
        exim_group = "Debian-exim"

        # Списки для exim (домены, ящики, псевдонимы) и файл ящиков dovecot.
        domains = "".join("%s: 1\n" % d["name"] for d in sorted(live, key=lambda d: d["name"]))
        mailboxes = "".join("%s@%s: 1\n" % (b["local"], d["name"]) for d in sorted(live, key=lambda d: d["name"]) for b in sorted(d["boxes"], key=lambda b: b["local"]))
        alias_rows = sorted((f"{a['local']}@{d['name']}", ", ".join(a["to"])) for d in live for a in d["aliases"])
        aliases = "".join("%s: %s\n" % row for row in alias_rows)
        users = []
        for d in sorted(live, key=lambda d: d["name"]):
            for b in sorted(d["boxes"], key=lambda b: b["local"]):
                home = os.path.join(vmail, d["name"], b["local"])
                extra = "userdb_mail=maildir:%s/Maildir userdb_quota_rule=*:storage=%dM" % (home, b["quota"])
                if not b["enabled"]:
                    extra += " nologin"  # доставка идёт, вход нет
                users.append("%s@%s:%s:%d:%d::%s::%s\n" % (b["local"], d["name"], b["hash"], VMAIL_UID, VMAIL_GID, home, extra))

        # Ключи DKIM: только у включённых доменов, у которых ключ есть.
        keys = {d["name"]: d["dkim"] for d in live if d["dkim"]}

        # Каталоги и sieve-скрипты ящиков.
        sieve_jobs = []
        for d in live:
            for b in d["boxes"]:
                sieve_jobs.append((d["name"], b["local"], sieve_script(f"{b['local']}@{d['name']}", b["autoreply"], b["forward"])))

        # --- всё проверено, дальше запись ---
        disk.write(os.path.join(mail_dir, "domains"), domains, 0o640, exim_group)
        disk.write(os.path.join(mail_dir, "mailboxes"), mailboxes, 0o640, exim_group)
        disk.write(os.path.join(mail_dir, "aliases"), aliases, 0o640, exim_group)
        disk.write(os.path.join(mail_dir, "dovecot-users"), "".join(users), 0o640, "dovecot")
        for name, pem in keys.items():
            disk.write(os.path.join(mail_dir, "dkim", name + ".key"), pem, 0o640, exim_group)
        for fn in os.listdir(os.path.join(mail_dir, "dkim")):
            if fn.endswith(".key") and fn[:-4] not in keys and DOMAIN_RE.match(fn[:-4]):
                os.remove(os.path.join(mail_dir, "dkim", fn))

        usage = {}
        for name, local, script in sieve_jobs:
            box = os.path.join(vmail, name, local)
            if not os.path.isdir(os.path.dirname(box)):
                disk.mkdir(os.path.dirname(box), 0o700, True)
            disk.mkdir(box, 0o700, True)
            sdir = os.path.join(box, "sieve-panel")
            spath = os.path.join(sdir, "panel.sieve")
            if script is None:
                for p in (spath, os.path.join(sdir, "panel.svbin")):
                    if os.path.lexists(p):
                        os.remove(p)
            else:
                disk.mkdir(sdir, 0o700, True)
                disk.write(spath, script, 0o600, owner=VMAIL_UID, group=VMAIL_GID)
            usage.setdefault(name, {})[local] = mailbox_usage(box)

        # Удаление с диска по просьбе панели: только перечисленное, живое не трогаем, ссылки не разворачиваем.
        live_domains = {d["name"] for d in doms}
        live_boxes = {(d["name"], b["local"]) for d in doms for b in d["boxes"]}
        for p in purge:
            if len(p) == 1 and p[0] in live_domains:
                continue
            if len(p) == 2 and (p[0], p[1]) in live_boxes:
                continue
            target = os.path.join(vmail, *p)
            if os.path.dirname(os.path.abspath(target)) not in (os.path.abspath(vmail), os.path.abspath(os.path.join(vmail, p[0]))):
                continue
            if len(p) == 2 and os.path.islink(os.path.join(vmail, p[0])):
                continue  # домен-ссылка: внутрь не заходим
            rmtree_safe(target)

        if usage_path:
            os.makedirs(os.path.dirname(usage_path), exist_ok=True)
            disk.write(usage_path, json.dumps(usage, sort_keys=True), 0o644)
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

#!/usr/bin/env python3
"""Тест deploy/bin/mail-sync.py на временных каталогах (без root: --no-chown). Запуск: python3 deploy/test/mail_sync_test.py"""
import importlib.util
import json
import os
import stat
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("mail_sync", os.path.join(HERE, "..", "bin", "mail-sync.py"))
ms = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ms)

HASH = "{SHA512-CRYPT}$6$saltsalt$" + "a" * 86
PEM = "-----BEGIN PRIVATE KEY-----\n" + ("QUJD" * 30 + "\n") * 3 + "-----END PRIVATE KEY-----\n"


def dom(name="example.com", **kw):
    d = {"name": name, "enabled": True, "dkim": {"selector": "vh1", "private_key": PEM}, "mailboxes": [{"local": "info", "hash": HASH, "quota_mb": 500, "enabled": True}],
         "aliases": [{"local": "postmaster", "to": ["boss@gmail.com"]}, {"local": "*", "to": ["info@example.com"]}]}
    d.update(kw)
    return d


class Env:
    def __init__(self, t):
        self.t = t
        self.dir = tempfile.mkdtemp()
        t.addCleanup(lambda: __import__("shutil").rmtree(self.dir, ignore_errors=True))
        self.mail = os.path.join(self.dir, "mail")
        self.vmail = os.path.join(self.dir, "vmail")
        self.usage = os.path.join(self.dir, "run", "usage.json")
        self.state = os.path.join(self.dir, "state.json")

    def run(self, state):
        with open(self.state, "w") as f:
            f.write(state if isinstance(state, str) else json.dumps(state))
        return ms.main(["--state", self.state, "--mail-dir", self.mail, "--vmail-dir", self.vmail, "--usage", self.usage, "--no-chown"])

    def read(self, name):
        with open(os.path.join(self.mail, name)) as f:
            return f.read()

    def snapshot(self):
        out = {}
        for root, _, files in os.walk(self.dir):
            for fn in files:
                p = os.path.join(root, fn)
                if p != self.state:
                    with open(p) as fh:
                        out[p] = fh.read()
        return out


class MailSyncTest(unittest.TestCase):
    def setUp(self):
        self.e = Env(self)

    def test_files_are_rendered(self):
        self.assertEqual(self.e.run({"version": 1, "domains": [dom(), dom("second.org", dkim=None, mailboxes=[], aliases=[])]}), 0)
        self.assertEqual(self.e.read("domains"), "example.com: 1\nsecond.org: 1\n")
        self.assertEqual(self.e.read("mailboxes"), "info@example.com: 1\n")
        self.assertEqual(self.e.read("aliases"), "*@example.com: info@example.com\npostmaster@example.com: boss@gmail.com\n")
        users = self.e.read("dovecot-users").strip().split("\n")
        self.assertEqual(len(users), 1)
        f = users[0].split(":", 7)
        self.assertEqual(f[0], "info@example.com")
        self.assertEqual(f[1], HASH)
        self.assertEqual(f[2:5], ["5000", "5000", ""])
        self.assertTrue(f[5].endswith("/example.com/info"))
        self.assertIn("userdb_mail=maildir:", f[7])
        self.assertIn("userdb_quota_rule=*:storage=500M", f[7])
        self.assertNotIn("nologin", f[7])
        self.assertTrue(os.path.isdir(os.path.join(self.e.vmail, "example.com", "info")))
        self.assertEqual(stat.S_IMODE(os.stat(os.path.join(self.e.vmail, "example.com", "info")).st_mode), 0o700)
        self.assertEqual(stat.S_IMODE(os.stat(os.path.join(self.e.mail, "dovecot-users")).st_mode), 0o640)

    def test_disabled_mailbox_keeps_delivery_but_blocks_login(self):
        d = dom()
        d["mailboxes"][0]["enabled"] = False
        self.assertEqual(self.e.run({"version": 1, "domains": [d]}), 0)
        self.assertIn("info@example.com: 1", self.e.read("mailboxes"))
        self.assertTrue(self.e.read("dovecot-users").strip().endswith(" nologin"))

    def test_disabled_domain_disappears_everywhere(self):
        self.assertEqual(self.e.run({"version": 1, "domains": [dom()]}), 0)
        self.assertTrue(os.path.exists(os.path.join(self.e.mail, "dkim", "example.com.key")))
        self.assertEqual(self.e.run({"version": 1, "domains": [dom(enabled=False)]}), 0)
        for name in ("domains", "mailboxes", "aliases", "dovecot-users"):
            self.assertEqual(self.e.read(name), "", name)
        self.assertFalse(os.path.exists(os.path.join(self.e.mail, "dkim", "example.com.key")))
        # почта не удаляется, пока панель не велела
        self.assertTrue(os.path.isdir(os.path.join(self.e.vmail, "example.com", "info")))

    def test_dkim_key_file(self):
        self.e.run({"version": 1, "domains": [dom()]})
        p = os.path.join(self.e.mail, "dkim", "example.com.key")
        with open(p) as fh:
            self.assertEqual(fh.read(), PEM)
        self.assertEqual(stat.S_IMODE(os.stat(p).st_mode), 0o640)
        # ключ у домена пропал — файл убирается
        self.e.run({"version": 1, "domains": [dom(dkim=None)]})
        self.assertFalse(os.path.exists(p))

    def test_second_run_changes_nothing(self):
        state = {"version": 1, "domains": [dom()]}
        self.e.run(state)
        marks = {p: os.stat(p).st_mtime_ns for p in self.e.snapshot() if "usage" not in p}
        self.e.run(state)
        for p, m in marks.items():
            self.assertEqual(os.stat(p).st_mtime_ns, m, p)

    def test_purge_removes_only_listed_and_not_live(self):
        self.e.run({"version": 1, "domains": [dom()]})
        for p in ("example.com/info", "example.com/old", "gone.org/x"):
            os.makedirs(os.path.join(self.e.vmail, p), exist_ok=True)
        os.makedirs(os.path.join(self.e.vmail, "keep.org", "y"), exist_ok=True)
        self.assertEqual(self.e.run({"version": 1, "domains": [dom()], "purge": ["example.com/old", "gone.org", "example.com/info", "example.com"]}), 0)
        self.assertFalse(os.path.exists(os.path.join(self.e.vmail, "example.com", "old")))
        self.assertFalse(os.path.exists(os.path.join(self.e.vmail, "gone.org")))
        self.assertTrue(os.path.isdir(os.path.join(self.e.vmail, "example.com", "info")), "живой ящик не удаляется, даже если он в списке")
        self.assertTrue(os.path.isdir(os.path.join(self.e.vmail, "keep.org", "y")), "чужое не трогаем")

    def test_purge_never_follows_symlinks_or_leaves_vmail(self):
        outside = os.path.join(self.e.dir, "outside")
        os.makedirs(outside)
        with open(os.path.join(outside, "precious"), "w") as fh:
            fh.write("x")
        os.makedirs(self.e.vmail)
        os.symlink(outside, os.path.join(self.e.vmail, "evil.org"))
        os.makedirs(os.path.join(self.e.vmail, "real.org"))
        os.symlink(outside, os.path.join(self.e.vmail, "real.org", "box"))
        self.assertEqual(self.e.run({"version": 1, "domains": [], "purge": ["evil.org", "real.org/box"]}), 0)
        self.assertTrue(os.path.exists(os.path.join(outside, "precious")))
        for bad in ("../outside", "/etc", "a/../../b", "x/y/z", "Evil.org", "a b.org", ""):
            self.assertEqual(self.e.run({"version": 1, "domains": [], "purge": [bad]}), 2, bad)

    def test_usage_is_reported(self):
        self.e.run({"version": 1, "domains": [dom()]})
        box = os.path.join(self.e.vmail, "example.com", "info", "Maildir", "new")
        os.makedirs(box)
        with open(os.path.join(box, "1"), "w") as fh:
            fh.write("x" * 1000)
        os.symlink("/etc/passwd", os.path.join(box, "link"))
        self.e.run({"version": 1, "domains": [dom()]})
        with open(self.e.usage) as fh:
            usage = json.load(fh)
        self.assertEqual(usage, {"example.com": {"info": 1000}})

    def test_invalid_state_is_rejected_and_nothing_changes(self):
        good = {"version": 1, "domains": [dom()]}
        self.assertEqual(self.e.run(good), 0)
        before = self.e.snapshot()
        bad_states = {
            "json": "{not json",
            "version": {"version": 2, "domains": []},
            "domain_name": {"version": 1, "domains": [dom(), dom("Bad Domain")]},
            "domain_name2": {"version": 1, "domains": [dom("a.com"), dom("a.com")]},
            "domain_name3": {"version": 1, "domains": [dom("a/b.com")]},
            "domain_name4": {"version": 1, "domains": [dom("localhost")]},
            "mailbox_local": {"version": 1, "domains": [dom(mailboxes=[{"local": "a:b", "hash": HASH, "quota_mb": 10}])]},
            "mailbox_local2": {"version": 1, "domains": [dom(mailboxes=[{"local": "a\nroot", "hash": HASH, "quota_mb": 10}])]},
            "mailbox_local3": {"version": 1, "domains": [dom(mailboxes=[{"local": "../x", "hash": HASH, "quota_mb": 10}])]},
            "mailbox_local4": {"version": 1, "domains": [dom(mailboxes=[{"local": "a..b", "hash": HASH, "quota_mb": 10}])]},
            "mailbox_hash": {"version": 1, "domains": [dom(mailboxes=[{"local": "a", "hash": HASH + "\nroot:x", "quota_mb": 10}])]},
            "mailbox_hash2": {"version": 1, "domains": [dom(mailboxes=[{"local": "a", "hash": "plain", "quota_mb": 10}])]},
            "mailbox_quota": {"version": 1, "domains": [dom(mailboxes=[{"local": "a", "hash": HASH, "quota_mb": 5}])]},
            "mailbox_quota2": {"version": 1, "domains": [dom(mailboxes=[{"local": "a", "hash": HASH, "quota_mb": True}])]},
            "alias_dest": {"version": 1, "domains": [dom(aliases=[{"local": "a", "to": ["x@y.com, evil@z.com"]}])]},
            "alias_dest2": {"version": 1, "domains": [dom(aliases=[{"local": "a", "to": ["|/bin/sh"]}])]},
            "alias_dest3": {"version": 1, "domains": [dom(aliases=[{"local": "a", "to": [":fail:"]}])]},
            "alias_dest4": {"version": 1, "domains": [dom(aliases=[{"local": "a", "to": []}])]},
            "alias_conflict": {"version": 1, "domains": [dom(aliases=[{"local": "info", "to": ["a@b.com"]}])]},
            "dkim": {"version": 1, "domains": [dom(dkim={"selector": "Bad Sel", "private_key": PEM})]},
            "dkim2": {"version": 1, "domains": [dom(dkim={"selector": "vh1", "private_key": "not a key"})]},
            "dkim3": {"version": 1, "domains": [dom(dkim={"selector": "vh1", "private_key": PEM + "\nextra"})]},
        }
        for code, state in bad_states.items():
            rc = self.e.run(state)
            self.assertEqual(rc, 2, code)
            self.assertEqual(self.e.snapshot(), before, f"{code}: файлы не должны меняться")

    def test_error_codes_are_reported(self):
        import io
        from contextlib import redirect_stdout
        buf = io.StringIO()
        with redirect_stdout(buf):
            self.e.run({"version": 1, "domains": [dom("Bad")]})
        self.assertEqual(buf.getvalue().strip(), "error=domain_name")

    def test_stray_dkim_keys_are_cleaned(self):
        self.e.run({"version": 1, "domains": [dom()]})
        stray = os.path.join(self.e.mail, "dkim", "old.org.key")
        with open(stray, "w") as fh:
            fh.write("x")
        self.e.run({"version": 1, "domains": [dom()]})
        self.assertFalse(os.path.exists(stray))

    def box_with(self, **extra):
        d = dom()
        d["mailboxes"][0].update(extra)
        return {"version": 1, "domains": [d]}

    def sieve_path(self):
        return os.path.join(self.e.vmail, "example.com", "info", "sieve-panel", "panel.sieve")

    def test_autoreply_and_forward_become_a_sieve_script(self):
        state = self.box_with(
            autoreply={"enabled": True, "subject": 'В "отпуске" \\', "body": "Здравствуйте\n.строка с точкой\nДо встречи", "from": "2026-10-01", "to": "2026-10-15", "days": 3},
            forward={"to": ["boss@gmail.com", "boss@gmail.com", "b@ex.org"], "keep_copy": True},
        )
        self.assertEqual(self.e.run(state), 0)
        with open(self.sieve_path(), encoding="utf-8") as f:
            text = f.read()
        self.assertIn('require ["copy", "vacation", "date", "relational"];', text)
        self.assertIn('redirect :copy "boss@gmail.com";', text)
        self.assertEqual(text.count("redirect"), 2, "повторы получателей убираются")
        self.assertIn('vacation :days 3 :subject "В \\"отпуске\\" \\\\" :addresses ["info@example.com"] text:', text)
        self.assertIn('currentdate :value "ge" "date" "2026-10-01"', text)
        self.assertIn("\n..строка с точкой\n", text)
        self.assertEqual(stat.S_IMODE(os.stat(self.sieve_path()).st_mode), 0o600)

    def test_forward_without_copy_and_autoreply_without_dates(self):
        self.assertEqual(self.e.run(self.box_with(forward={"to": ["x@y.com"], "keep_copy": False})), 0)
        with open(self.sieve_path(), encoding="utf-8") as f:
            text = f.read()
        self.assertIn('redirect "x@y.com";', text)
        self.assertNotIn("vacation", text)
        self.assertEqual(self.e.run(self.box_with(autoreply={"enabled": True, "subject": "Занят", "body": "Отвечу позже"})), 0)
        with open(self.sieve_path(), encoding="utf-8") as f:
            text = f.read()
        self.assertNotIn("redirect", text)
        self.assertNotIn("allof", text)
        self.assertIn(":days 1", text)

    def test_sieve_script_is_removed_when_settings_are_turned_off(self):
        self.assertEqual(self.e.run(self.box_with(forward={"to": ["x@y.com"], "keep_copy": True})), 0)
        self.assertTrue(os.path.exists(self.sieve_path()))
        open(os.path.join(os.path.dirname(self.sieve_path()), "panel.svbin"), "w").close()
        self.assertEqual(self.e.run(self.box_with(autoreply={"enabled": False, "subject": "x", "body": "y"}, forward={"to": []})), 0)
        self.assertFalse(os.path.exists(self.sieve_path()))
        self.assertFalse(os.path.exists(os.path.join(os.path.dirname(self.sieve_path()), "panel.svbin")))

    def test_invalid_sieve_settings_are_rejected_and_nothing_changes(self):
        good = self.box_with(forward={"to": ["x@y.com"], "keep_copy": True})
        self.assertEqual(self.e.run(good), 0)
        before = self.e.snapshot()
        ar = lambda **kw: {"enabled": True, "subject": "s", "body": "b", **kw}
        bad = {
            "subject_nl": self.box_with(autoreply=ar(subject="a\nb")),
            "subject_long": self.box_with(autoreply=ar(subject="x" * 201)),
            "subject_empty": self.box_with(autoreply=ar(subject="  ")),
            "body_nul": self.box_with(autoreply=ar(body="a\x00b")),
            "body_long": self.box_with(autoreply=ar(body="x" * 2001)),
            "date_format": self.box_with(autoreply=ar(**{"from": "01.10.2026"})),
            "date_calendar": self.box_with(autoreply=ar(**{"from": "2026-02-30"})),
            "date_order": self.box_with(autoreply=ar(**{"from": "2026-10-15", "to": "2026-10-01"})),
            "days": self.box_with(autoreply=ar(days=0)),
            "days_bool": self.box_with(autoreply=ar(days=True)),
            "autoreply_type": self.box_with(autoreply="yes"),
            "fw_dest": self.box_with(forward={"to": ["x@y.com, z@y.com"], "keep_copy": True}),
            "fw_pipe": self.box_with(forward={"to": ["|/bin/sh"], "keep_copy": True}),
            "fw_many": self.box_with(forward={"to": [f"u{i}@y.com" for i in range(6)], "keep_copy": True}),
            "fw_loop": self.box_with(forward={"to": ["info@example.com"], "keep_copy": True}),
            "fw_keep": self.box_with(forward={"to": ["x@y.com"], "keep_copy": "yes"}),
            "fw_type": self.box_with(forward=["x@y.com"]),
        }
        for code, state in bad.items():
            self.assertEqual(self.e.run(state), 2, code)
            self.assertEqual(self.e.snapshot(), before, f"{code}: файлы не должны меняться")


if __name__ == "__main__":
    unittest.main(verbosity=1)

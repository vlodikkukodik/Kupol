#!/usr/bin/env python3
"""Тест deploy/bin/mail-log.py на настоящих строках журнала exim (снятых с сервера). Запуск: python3 deploy/test/mail_log_test.py"""
import importlib.util
import io
import json
import os
import tempfile
import unittest
from contextlib import redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("mail_log", os.path.join(HERE, "..", "bin", "mail-log.py"))
ml = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ml)

LOG = """\
2026-09-25 14:22:22 1xA6og-000000037kI-09Ie <= app@localhost H=localhost (www.abstract-laced-fjord.majordomo-client.ru) [127.0.0.1] P=esmtp S=599 from <app@localhost> for sales@zzmail.test
2026-09-25 14:22:22 1xA6og-000000037kI-0BvG <= app@localhost H=localhost (www.abstract-laced-fjord.majordomo-client.ru) [127.0.0.1] P=esmtp S=603 from <app@localhost> for random@zzmail.test
2026-09-25 14:22:22 1xA6og-000000037kI-0BvG => info <random@zzmail.test> R=vladhost_mailbox T=vladhost_lmtp C="250 2.0.0 <info@zzmail.test> 1EU/Bp6DtmpFXQsA4KmKtQ Saved"
2026-09-25 14:22:22 1xA6og-000000037kI-0BvG Completed
2026-09-25 14:22:25 H=www.abstract-laced-fjord.majordomo-client.ru [37.153.70.33] F=<x@gmail.com> rejected RCPT <hacker@evil.example>: relay not permitted
2026-09-25 14:22:25 H=www.abstract-laced-fjord.majordomo-client.ru [37.153.70.33] F=<boss@zzmail.test> rejected RCPT <second@zzmail.test>: Sender domain is served by this server: authenticate to send from it
2026-09-25 14:22:27 1xA6ol-000000037ku-33mU <= info@zzmail.test H=www.abstract-laced-fjord.majordomo-client.ru [37.153.70.33] P=esmtpsa X=TLS1.3:ECDHE_X25519__ECDSA_SECP256R1_SHA256__AES_256_GCM:256 CV=no A=plain_dovecot:info@zzmail.test S=632 from <info@zzmail.test> for second@zzmail.test
2026-09-25 14:22:27 H=www.abstract-laced-fjord.majordomo-client.ru [37.153.70.33] X=TLS1.3:ECDHE_X25519__ECDSA_SECP256R1_SHA256__AES_256_GCM:256 CV=no rejected MAIL <boss@other.example>: Sender address does not belong to the authenticated account
2026-09-25 14:22:27 1xA6ol-000000037ku-33mU => second <second@zzmail.test> R=vladhost_mailbox T=vladhost_lmtp C="250 2.0.0 <second@zzmail.test> yP3fLKODtmpFXQsA4KmKtQ Saved"
2026-09-25 14:22:27 1xA6ol-000000037ku-33mU Completed
2026-09-25 14:23:52 no host name found for IP address 102.220.161.126
2026-09-25 14:51:37 1x9dkv-00000002abD-25c9 == deploy@www.abstract-laced-fjord.majordomo-client.ru routing defer (-52): retry time not reached
2026-09-25 15:01:00 1xB000-000000037ku-33mU <= boss@other.example H=mx.other.example (mx.other.example) [203.0.113.5] P=esmtps S=900 for zzmail.test-not@example.org
2026-09-25 15:02:01 1xB111-000000037ku-33mU <= info@zzmail.test H=x [1.2.3.4] P=esmtpsa A=plain_dovecot:info@zzmail.test S=500 from <info@zzmail.test> for friend@gmail.com
2026-09-25 15:02:02 1xB111-000000037ku-33mU => friend@gmail.com <friend@gmail.com> R=dnslookup T=remote_smtp H=gmail-smtp-in.l.google.com [142.250.1.26] X=TLS1.3 C="250 2.0.0 OK"
2026-09-25 15:03:00 1xB222-000000037ku-33mU == friend@gmail.com R=dnslookup T=remote_smtp defer (-44): SMTP error from remote mail server after RCPT TO:<friend@gmail.com>: host gmail-smtp-in.l.google.com [142.250.1.26]: 451 4.3.0 try later
2026-09-25 15:04:00 1xB222-000000037ku-33mU ** ghost@zzmail.test R=vladhost_mailbox T=vladhost_lmtp: LMTP error after RCPT: 550 5.1.1 User doesn't exist
2026-09-25 15:05:00 H=bad.example [198.51.100.9] F=<spammer@bad.example> rejected after DATA: virus Eicar-Test-Signature from spammer@bad.example to info@zzmail.test
2026-09-25 15:06:00 H=host [198.51.100.9] F=<a@b.example> rejected RCPT <info@zzmail.testx>: not ours
"""

QUEUE = """\
 2h  1.5K 1xB222-000000037ku-33mU <info@zzmail.test>
          friend@gmail.com
        D done@gmail.com

 5m   800 1xB333-000000037ku-33mU <other@else.org> *** frozen ***
          somebody@else.org
"""


class MailLogTest(unittest.TestCase):
    def events(self, domain="zzmail.test", limit=100):
        return ml.events_for(domain, LOG.splitlines(), limit)

    def test_only_the_domains_lines_are_returned_newest_first(self):
        ev = self.events()
        self.assertEqual([e["t"] for e in ev], sorted([e["t"] for e in ev], reverse=True))
        self.assertFalse(any(e["id"] == "1x9dkv-00000002abD-25c9" for e in ev), "чужое письмо без адресов домена не попадает")
        self.assertFalse(any("evil.example" in e["to"] for e in ev), "чужие адреса не попадают")
        self.assertFalse(any(e["to"].endswith("zzmail.testx") for e in ev), "домен сравнивается целиком")
        self.assertEqual(self.events("nothing.example"), [])

    def test_kinds_and_fields(self):
        by = {}
        for e in self.events():
            by.setdefault(e["kind"], []).append(e)
        self.assertEqual({k: len(v) for k, v in by.items()}, {"received": 4, "delivered": 3, "deferred": 1, "failed": 1, "rejected": 2})
        d = next(e for e in by["delivered"] if e["to"] == "random@zzmail.test")
        self.assertEqual((d["via"], d["from"], d["id"]), ("mailbox", "app@localhost", "1xA6og-000000037kI-0BvG"))
        self.assertIn("Saved", d["detail"])
        rec = next(e for e in by["received"] if e["from"] == "info@zzmail.test" and e["to"] == "second@zzmail.test")
        self.assertEqual(rec["via"], "auth")
        self.assertEqual(rec["host"], "www.abstract-laced-fjord.majordomo-client.ru [37.153.70.33]")
        local = next(e for e in by["received"] if e["to"] == "sales@zzmail.test")
        self.assertEqual((local["via"], local["host"]), ("local", "localhost [127.0.0.1]"))
        out = next(e for e in by["delivered"] if e["to"] == "friend@gmail.com")
        self.assertEqual((out["via"], out["host"]), ("remote", "gmail-smtp-in.l.google.com [142.250.1.26]"))
        self.assertIn("451 4.3.0 try later", by["deferred"][0]["detail"])
        self.assertIn("User doesn't exist", by["failed"][0]["detail"])
        rej = {e["detail"]: e for e in by["rejected"]}
        self.assertIn("Sender domain is served by this server: authenticate to send from it", rej)
        self.assertNotIn("Sender address does not belong to the authenticated account", rej, "в строке нет адресов домена")
        virus = next(e for e in by["rejected"] if "virus" in e["detail"])
        self.assertEqual((virus["from"], virus["host"]), ("spammer@bad.example", "bad.example [198.51.100.9]"))

    def test_limit_and_control_characters(self):
        self.assertEqual(len(self.events(limit=3)), 3)
        ev = ml.parse_line("2026-09-25 15:00:00 1xB000-000000037ku-33mU ** a@z.test R=x T=y: bad\x1b[31m text\x00", {})
        self.assertNotRegex(ev["detail"], r"[\x00-\x1f]")

    def test_queue_is_filtered_by_domain(self):
        q = ml.parse_queue(QUEUE, "zzmail.test")
        self.assertEqual(len(q), 1)
        self.assertEqual((q[0]["id"], q[0]["from"], q[0]["to"], q[0]["frozen"], q[0]["age"]), ("1xB222-000000037ku-33mU", "info@zzmail.test", ["friend@gmail.com"], False, "2h"))
        q = ml.parse_queue(QUEUE, "else.org")
        self.assertEqual((len(q), q[0]["frozen"]), (1, True))

    def test_main_reads_files_and_validates_arguments(self):
        with tempfile.TemporaryDirectory() as d:
            log = os.path.join(d, "mainlog")
            with open(log, "w", encoding="utf-8") as f:
                f.write(LOG)
            with open(log + ".1", "w", encoding="utf-8") as f:
                f.write("2026-09-24 10:00:00 1xC000-000000037ku-33mU => old@zzmail.test <old@zzmail.test> R=vladhost_mailbox T=vladhost_lmtp C=\"250 Saved\"\n")
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = ml.main(["--domain", "zzmail.test", "--log", log, "--queue-cmd", "true"])
            self.assertEqual(rc, 0)
            out = json.loads(buf.getvalue())
            self.assertEqual(len(out["events"]), 12)
            self.assertEqual(out["events"][-1]["to"], "old@zzmail.test", "недостающее берётся из прошлого журнала")
            self.assertEqual(out["queue"], [])
            for bad in (["--domain", "Bad Domain"], ["--domain", "../etc"], ["--domain", "zzmail.test", "--limit", "0"], ["--domain", "zzmail.test", "--limit", "500"]):
                buf = io.StringIO()
                with redirect_stdout(buf):
                    self.assertEqual(ml.main(bad + ["--log", log]), 2, bad)
                self.assertEqual(buf.getvalue().strip(), "error=bad_args")

    def test_missing_log_gives_empty_result(self):
        buf = io.StringIO()
        with redirect_stdout(buf):
            self.assertEqual(ml.main(["--domain", "zzmail.test", "--log", "/nonexistent/mainlog", "--queue-cmd", "true"]), 0)
        self.assertEqual(json.loads(buf.getvalue()), {"events": [], "queue": []})


if __name__ == "__main__":
    unittest.main(verbosity=1)

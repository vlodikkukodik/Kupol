#!/usr/bin/env python3
"""Тест deploy/bin/dns-sync.py на временных каталогах (без BIND и root). Запуск: python3 deploy/test/dns_sync_test.py"""
import importlib.util
import io
import json
import os
import shutil
import stat
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("dns_sync", os.path.join(HERE, "..", "bin", "dns-sync.py"))
ds = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ds)

NS = ["ns.vladinc.ru", "ns2.vladinc.ru"]


def rec(name="@", typ="A", value="203.0.113.10", ttl=300, **kw):
    r = {"name": name, "type": typ, "value": value, "ttl": ttl}
    r.update(kw)
    return r


def zone(name="example.com", records=None, serial=1700000000):
    return {"name": name, "serial": serial, "records": records if records is not None else [rec(), rec("www", "CNAME", "example.com")]}


def state(zones):
    return {"version": 1, "nameservers": NS, "hostmaster": "hostmaster.vladinc.ru", "zones": zones}


class Env:
    def __init__(self, t):
        self.dir = tempfile.mkdtemp()
        t.addCleanup(lambda: shutil.rmtree(self.dir, ignore_errors=True))
        self.zones = os.path.join(self.dir, "zones")
        self.conf = os.path.join(self.dir, "named.conf.vladhost")
        self.state = os.path.join(self.dir, "state.json")
        self.calls = os.path.join(self.dir, "calls.log")
        self.rndc = os.path.join(self.dir, "rndc")
        with open(self.rndc, "w") as f:
            f.write(f'#!/bin/sh\necho "rndc $*" >>{self.calls}\n')
        os.chmod(self.rndc, 0o755)

    def run(self, st, checkzone="", rndc=True):
        with open(self.state, "w") as f:
            f.write(st if isinstance(st, str) else json.dumps(st))
        buf = io.StringIO()
        argv = ["--state", self.state, "--zones", self.zones, "--conf", self.conf, "--checkzone", checkzone, "--checkconf", "", "--group", ""]
        argv += ["--rndc", self.rndc if rndc else ""]
        with redirect_stdout(buf):
            rc = ds.main(argv)
        self.out = buf.getvalue().strip()
        return rc

    def read(self, name):
        with open(os.path.join(self.zones, name + ".zone"), encoding="utf-8") as f:
            return f.read()

    def snapshot(self):
        out = {}
        for root, _, files in os.walk(self.dir):
            for fn in files:
                p = os.path.join(root, fn)
                if p not in (self.state, self.calls):
                    with open(p, encoding="utf-8") as fh:
                        out[p] = fh.read()
        return out

    def calls_text(self):
        try:
            with open(self.calls) as f:
                return f.read()
        except FileNotFoundError:
            return ""


class DnsSyncTest(unittest.TestCase):
    def setUp(self):
        self.e = Env(self)

    def test_zone_file_and_config(self):
        recs = [
            rec(), rec("www", "CNAME", "example.com"), rec("@", "AAAA", "2001:db8::1"),
            rec("@", "MX", "mail.vladinc.ru", priority=10), rec("@", "TXT", "v=spf1 mx ip4:203.0.113.10 ~all"),
            rec("vh1._domainkey", "TXT", "v=DKIM1; k=rsa; p=" + "A" * 400), rec("_sip._tcp", "SRV", "5 5060 sip.example.com", priority=10),
            rec("@", "CAA", "0 issue letsencrypt.org"), rec("*.dev", "A", "203.0.113.11", 3600),
        ]
        self.assertEqual(self.e.run(state([zone(records=recs)])), 0, self.e.out)
        self.assertEqual(self.e.out, "ok")
        text = self.e.read("example.com")
        lines = text.splitlines()
        self.assertEqual(lines[0], "$ORIGIN example.com.")
        self.assertEqual(lines[2], "@ IN SOA ns.vladinc.ru. hostmaster.vladinc.ru. 1700000000 3600 900 1209600 300")
        self.assertIn("@ IN NS ns.vladinc.ru.", lines)
        self.assertIn("@ IN NS ns2.vladinc.ru.", lines)
        self.assertIn("@ 300 IN A 203.0.113.10", lines)
        self.assertIn("@ 300 IN MX 10 mail.vladinc.ru.", lines)
        self.assertIn("www 300 IN CNAME example.com.", lines)
        self.assertIn('@ 300 IN CAA 0 issue "letsencrypt.org"', lines)
        self.assertIn("_sip._tcp 300 IN SRV 10 5 5060 sip.example.com.", lines)
        self.assertIn("*.dev 3600 IN A 203.0.113.11", lines)
        dkim = next(ln for ln in lines if ln.startswith("vh1._domainkey"))
        self.assertEqual(dkim.count('" "'), 1, "TXT длиннее 255 знаков режется на строки")
        with open(self.e.conf) as f:
            conf = f.read()
        self.assertIn(f'zone "example.com" {{ type primary; file "{self.e.zones}/example.com.zone"; allow-transfer {{ none; }}; allow-update {{ none; }}; notify no; }};', conf)
        self.assertEqual(stat.S_IMODE(os.stat(os.path.join(self.e.zones, "example.com.zone")).st_mode), 0o644)

    def test_txt_escaping(self):
        self.assertEqual(ds.txt_quote('a "b" \\c'), '"a \\"b\\" \\\\c"')
        self.assertEqual(ds.txt_quote("é"), '"\\195\\169"')
        self.assertEqual(ds.txt_quote("x" * 300), '"' + "x" * 255 + '" "' + "x" * 45 + '"')

    def test_reload_only_what_changed(self):
        self.assertEqual(self.e.run(state([zone(), zone("other.org")])), 0)
        first = self.e.calls_text()
        self.assertIn("rndc reconfig", first)
        self.assertIn("rndc reload example.com", first)
        self.assertIn("rndc reload other.org", first)
        open(self.e.calls, "w").close()
        self.assertEqual(self.e.run(state([zone(), zone("other.org")])), 0)
        self.assertEqual(self.e.calls_text(), "", "ничего не менялось — BIND не трогаем")
        open(self.e.calls, "w").close()
        self.assertEqual(self.e.run(state([zone(serial=1700000001), zone("other.org")])), 0)
        self.assertEqual(self.e.calls_text().strip(), "rndc reload example.com")

    def test_removed_zone_disappears(self):
        self.assertEqual(self.e.run(state([zone(), zone("other.org")])), 0)
        self.assertEqual(self.e.run(state([zone()])), 0)
        self.assertFalse(os.path.exists(os.path.join(self.e.zones, "other.org.zone")))
        with open(self.e.conf) as f:
            self.assertNotIn("other.org", f.read())
        self.assertEqual(self.e.run(state([])), 0)
        self.assertEqual(os.listdir(self.e.zones), [])

    def test_checkzone_failure_changes_nothing(self):
        self.assertEqual(self.e.run(state([zone()])), 0)
        before = self.e.snapshot()
        bad = os.path.join(self.e.dir, "checkzone")
        with open(bad, "w") as f:
            f.write("#!/bin/sh\necho 'zone bad' >&2\nexit 1\n")
        os.chmod(bad, 0o755)
        self.assertEqual(self.e.run(state([zone(serial=1700000009), zone("new.org")]), checkzone=bad), 2)
        self.assertEqual(self.e.out, "error=checkzone")
        after = self.e.snapshot()
        self.assertEqual({k: v for k, v in after.items() if "checkzone" not in k and "/.check" not in k}, {k: v for k, v in before.items() if "checkzone" not in k})
        self.assertFalse(os.path.exists(os.path.join(self.e.zones, ".check")))

    def test_invalid_state_is_rejected_and_nothing_changes(self):
        self.assertEqual(self.e.run(state([zone()])), 0)
        before = self.e.snapshot()
        r = lambda **kw: [zone(records=[rec(**kw)])]
        bad = {
            "json": "{nope",
            "version": {"version": 2},
            "nameservers": {**state([]), "nameservers": []},
            "nameservers_bad": {**state([]), "nameservers": ["ns one"]},
            "hostmaster": {**state([]), "hostmaster": "x y"},
            "zone_name": state([zone("Bad Zone")]),
            "zone_name2": state([zone("a/b.com")]),
            "zone_name3": state([zone("localhost")]),
            "zone_dup": state([zone(), zone()]),
            "serial": state([zone(serial=0)]),
            "serial_bool": state([zone(serial=True)]),
            "type": state(r(typ="NS")),
            "type_soa": state(r(typ="SOA")),
            "name": state(r(name="bad name")),
            "name_dots": state(r(name="a..b")),
            "name_newline": state(r(name="a\nb")),
            "name_star_mid": state(r(name="a.*.b")),
            "ttl": state(r(ttl=1)),
            "ttl_odd": state(r(ttl=301)),
            "ttl_bool": state(r(ttl=True)),
            "a_value": state(r(value="999.1.1.1")),
            "a_zero": state(r(value="0.0.0.0")),
            "a_leading_zero": state(r(value="010.0.0.1")),
            "a_multi": state(r(value="1.1.1.1 2.2.2.2")),
            "aaaa": state(r(typ="AAAA", value="not-ip")),
            "cname_ip": state(r(name="w", typ="CNAME", value="1.2.3.4")),
            "cname_apex": state(r(name="@", typ="CNAME", value="x.example.org")),
            "cname_space": state(r(name="w", typ="CNAME", value="x.example.org ; evil")),
            "mx_prio": state(r(typ="MX", value="mail.example.org", priority=70000)),
            "mx_noprio": state(r(typ="MX", value="mail.example.org")),
            "mx_ip": state(r(typ="MX", value="1.2.3.4", priority=10)),
            "txt_empty": state(r(typ="TXT", value="")),
            "txt_long": state(r(typ="TXT", value="x" * 2049)),
            "txt_ctrl": state(r(typ="TXT", value="a\nb")),
            "srv": state(r(name="_s._tcp", typ="SRV", value="0 80", priority=1)),
            "srv_port": state(r(name="_s._tcp", typ="SRV", value="5 0 host.example.org", priority=1)),
            "caa_tag": state(r(typ="CAA", value="0 evil letsencrypt.org")),
            "caa_flags": state(r(typ="CAA", value="1 issue letsencrypt.org")),
            "dup": state([zone(records=[rec(), rec()])]),
            "cname_and_a": state([zone(records=[rec("w", "A", "1.2.3.4"), rec("w", "CNAME", "x.example.org")])]),
            "two_cnames": state([zone(records=[rec("w", "CNAME", "x.example.org"), rec("w", "CNAME", "y.example.org")])]),
            "too_many": state([zone(records=[rec(f"h{i}", "A", "1.2.3.4") for i in range(201)])]),
        }
        for code, st in bad.items():
            rc = self.e.run(st)
            self.assertEqual(rc, 2, code)
            self.assertEqual(self.e.snapshot(), before, f"{code}: файлы не должны меняться")

    def test_error_code_is_reported(self):
        self.e.run(state([zone(records=[rec(ttl=1)])]))
        self.assertEqual(self.e.out, "error=record_ttl")
        self.e.run(state([zone(records=[rec("w", "CNAME", "x.example.org"), rec("w", "A", "1.2.3.4")])]))
        self.assertEqual(self.e.out, "error=record_cname")

    def test_same_name_different_types_are_fine(self):
        recs = [rec("@", "A", "1.1.1.1"), rec("@", "A", "2.2.2.2"), rec("@", "TXT", "one"), rec("@", "TXT", "two"), rec("@", "MX", "m1.example.org", priority=10), rec("@", "MX", "m2.example.org", priority=20)]
        self.assertEqual(self.e.run(state([zone(records=recs)])), 0, self.e.out)


if __name__ == "__main__":
    unittest.main(verbosity=1)

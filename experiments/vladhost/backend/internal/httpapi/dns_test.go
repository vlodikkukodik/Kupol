package httpapi_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vladhost/internal/config"
	"vladhost/internal/dnszones"
	"vladhost/internal/httpapi"
	"vladhost/internal/mailhost"
	"vladhost/internal/runtimes"
)

// dnsHelper играет исполнителя DNS: запускает настоящий dns-sync.py на временных каталогах (без BIND) и подтверждает почтовые заявки.
type dnsHelper struct {
	dir, zones, conf string
	mu               sync.Mutex
	syncs            int
}

func (h *dnsHelper) Do(_ context.Context, r runtimes.Request, _ time.Duration) (runtimes.Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Action != dnszones.ActionDNSSync {
		return runtimes.Result{OK: true}, nil // заявки почты
	}
	h.syncs++
	script, _ := filepath.Abs("../../../deploy/bin/dns-sync.py")
	out, err := exec.Command("python3", script, "--state", filepath.Join(h.dir, "dns", "state.json"), "--zones", h.zones, "--conf", h.conf,
		"--checkzone", "", "--checkconf", "", "--rndc", "", "--group", "").CombinedOutput()
	if got := strings.TrimSpace(string(out)); err != nil || got != "ok" {
		return runtimes.Result{Error: strings.TrimPrefix(got, "error="), Output: got}, nil
	}
	return runtimes.Result{OK: true}, nil
}

type nsResolver struct {
	mu sync.Mutex
	ns map[string][]string
}

func (n *nsResolver) LookupNS(_ context.Context, name string) ([]string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ns[name], nil
}

func (n *nsResolver) set(domain string, ns ...string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ns[domain] = ns
}

func (e *env) withDNS() (*dnsHelper, *nsResolver) {
	e.t.Helper()
	dir := e.t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rt", "dns"), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "rt", "mail"), 0o755); err != nil {
		e.t.Fatal(err)
	}
	h := &dnsHelper{dir: filepath.Join(dir, "rt"), zones: filepath.Join(dir, "zones"), conf: filepath.Join(dir, "named.conf.vladhost")}
	res := &nsResolver{ns: map[string][]string{}}
	dns := dnszones.New(e.db, dnszones.Config{NS: []string{"ns.vladinc.ru", "ns2.vladinc.ru"}, Hostmaster: "hostmaster.vladinc.ru", ServerIPs: []string{serverIP},
		BaseDomain: "vladinc.ru", Dir: h.dir, Applier: h, Sites: e.sites, Resolver: res})
	mail := mailhost.New(e.db, mailhost.Config{Host: "mail.vladinc.ru", ServerIP: serverIP, BaseDomain: "vladinc.ru", Dir: h.dir, Applier: h, Sites: e.sites,
		Resolver: noDNS{}, DNS: dns.ForMail()})
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000}, httpapi.WithDNS(dns), httpapi.WithMailHost(mail))
	return h, res
}

func TestDNSHiddenWhenNotConfigured(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	for _, tc := range [][2]string{{"GET", "/api/dns"}, {"POST", "/api/dns/zones"}, {"DELETE", "/api/dns/zones/1"}, {"GET", "/api/dns/zones/1/delegation"},
		{"POST", "/api/dns/zones/1/records"}, {"PUT", "/api/dns/records/1"}, {"DELETE", "/api/dns/records/1"}} {
		if w := e.do(tc[0], tc[1], map[string]any{}, tok); w.Code != 404 {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	if !strings.Contains(e.do("GET", "/api/me", nil, tok).Body.String(), `"dns_enabled":false`) {
		t.Fatal("флаг выключенного DNS должен быть в /me")
	}
}

func TestDNSThroughAPI(t *testing.T) {
	e := newEnv(t)
	dns, res := e.withDNS()
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	if w := e.do("GET", "/api/dns", nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
	if !strings.Contains(e.do("GET", "/api/me", nil, john).Body.String(), `"dns_enabled":true`) {
		t.Fatal("флаг DNS должен быть в /me")
	}
	site, _ := e.createSite(john, "blog")
	dnsFake, _ := e.withDomains(10, 10)
	dnsFake.set("mine.example.com", serverIP)
	if code, _, body := e.addDomain(john, site, "mine.example.com"); code != 201 {
		t.Fatalf("домен сайта: %d %s", code, body)
	}

	type overview struct {
		Info struct {
			NS       []string `json:"ns"`
			Eligible []string `json:"eligible_domains"`
			Types    []string `json:"types"`
		} `json:"info"`
		Zones []struct {
			ID      int64  `json:"id"`
			Domain  string `json:"domain"`
			Records []struct {
				ID      int64  `json:"id"`
				Name    string `json:"name"`
				Type    string `json:"type"`
				Value   string `json:"value"`
				Managed string `json:"managed"`
			} `json:"records"`
		} `json:"zones"`
	}
	ov := decode[overview](t, e.do("GET", "/api/dns", nil, john))
	if len(ov.Info.NS) != 2 || ov.Info.NS[0] != "ns.vladinc.ru" || len(ov.Info.Eligible) != 1 || ov.Info.Eligible[0] != "mine.example.com" || len(ov.Zones) != 0 || len(ov.Info.Types) != 7 {
		t.Fatalf("%+v", ov)
	}
	if w := e.do("POST", "/api/dns/zones", map[string]string{"domain": "theirs.org"}, john); w.Code != 422 || decode[errBody](t, w).Error.Code != "validation.dns_domain_not_attached" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w := e.do("POST", "/api/dns/zones", map[string]string{"domain": "Mine.Example.com"}, john)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/dns/zones", map[string]string{"domain": "mine.example.com"}, john); w.Code != 409 {
		t.Fatalf("повтор: %d", w.Code)
	}
	ov = decode[overview](t, e.do("GET", "/api/dns", nil, john))
	if len(ov.Zones) != 1 || len(ov.Zones[0].Records) != 2 || len(ov.Info.Eligible) != 0 {
		t.Fatalf("%+v", ov)
	}
	zone := ov.Zones[0].ID
	base := fmt.Sprintf("/api/dns/zones/%d", zone)
	if data, err := os.ReadFile(filepath.Join(dns.zones, "mine.example.com.zone")); err != nil || !strings.Contains(string(data), "@ IN NS ns2.vladinc.ru.") {
		t.Fatalf("файл зоны: %s %v", data, err)
	}

	// чужой пользователь ничего не видит и не может
	for _, tc := range [][2]string{{"GET", base + "/delegation"}, {"DELETE", base}, {"POST", base + "/records"}} {
		if w := e.do(tc[0], tc[1], map[string]any{"type": "A", "value": "1.2.3.4"}, mary); w.Code != 404 {
			t.Errorf("чужой %s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	if len(decode[overview](t, e.do("GET", "/api/dns", nil, mary)).Zones) != 0 {
		t.Fatal("чужие зоны не показываются")
	}

	// записи
	w = e.do("POST", base+"/records", map[string]any{"name": "Blog", "type": "a", "value": "203.0.113.50", "ttl": 600}, john)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	rec := decode[struct {
		Record struct{ ID int64 } `json:"record"`
	}](t, w).Record.ID
	for _, tc := range []struct {
		body map[string]any
		want string
	}{
		{map[string]any{"type": "NS", "value": "x.example.org"}, "validation.dns_type"},
		{map[string]any{"name": "a b", "type": "A", "value": "1.2.3.4"}, "validation.dns_name"},
		{map[string]any{"type": "A", "value": "999.1.1.1"}, "validation.dns_value"},
		{map[string]any{"type": "A", "value": "1.2.3.4", "ttl": 5}, "validation.dns_ttl"},
		{map[string]any{"name": "www", "type": "A", "value": "1.2.3.4"}, "dns_cname_conflict"},
		{map[string]any{"name": "blog", "type": "A", "value": "203.0.113.50", "ttl": 600}, "dns_record_duplicate"},
		{map[string]any{"type": "CNAME", "value": "x.example.org"}, "validation.dns_cname_apex"},
	} {
		if w := e.do("POST", base+"/records", tc.body, john); decode[errBody](t, w).Error.Code != tc.want {
			t.Errorf("%v: %d %s (ждали %s)", tc.body, w.Code, w.Body, tc.want)
		}
	}
	if w := e.do("PUT", fmt.Sprintf("/api/dns/records/%d", rec), map[string]any{"name": "blog", "type": "A", "value": "203.0.113.51", "ttl": 300}, john); w.Code != 200 || !strings.Contains(w.Body.String(), "203.0.113.51") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", fmt.Sprintf("/api/dns/records/%d", rec), map[string]any{"type": "A", "value": "1.2.3.4"}, mary); w.Code != 404 {
		t.Fatalf("чужая запись: %d", w.Code)
	}
	if data, _ := os.ReadFile(filepath.Join(dns.zones, "mine.example.com.zone")); !strings.Contains(string(data), "blog 300 IN A 203.0.113.51") {
		t.Fatalf("%s", data)
	}

	// делегирование
	type deleg struct {
		Delegation struct {
			State    string   `json:"state"`
			Found    []string `json:"found"`
			Expected []string `json:"expected"`
		} `json:"delegation"`
	}
	if d := decode[deleg](t, e.do("GET", base+"/delegation", nil, john)); d.Delegation.State != "none" || len(d.Delegation.Expected) != 2 {
		t.Fatalf("%+v", d)
	}
	res.set("mine.example.com", "ns.vladinc.ru", "ns2.vladinc.ru")
	if d := decode[deleg](t, e.do("GET", base+"/delegation", nil, john)); d.Delegation.State != "ok" {
		t.Fatalf("%+v", d)
	}

	// почта: у домена на наших серверах имён кнопка ставит записи в зону
	mailBox := decode[struct {
		Domain struct{ ID int64 } `json:"domain"`
	}](t, e.do("POST", "/api/mail/domains", map[string]string{"domain": "mine.example.com"}, john))
	mbase := fmt.Sprintf("/api/mail/domains/%d", mailBox.Domain.ID)
	type mailDNS struct {
		Auto struct {
			Available bool   `json:"available"`
			Zone      bool   `json:"zone"`
			Delegated bool   `json:"delegated"`
			State     string `json:"state"`
		} `json:"auto"`
		Records []struct{ Kind, State string } `json:"records"`
	}
	res.set("mine.example.com", "ns.majordomo.ru")
	md := decode[mailDNS](t, e.do("GET", mbase+"/dns", nil, john))
	if !md.Auto.Available || !md.Auto.Zone || md.Auto.Delegated || md.Auto.State != "none" || len(md.Records) != 4 {
		t.Fatalf("на чужих серверах: %+v", md)
	}
	if w := e.do("POST", mbase+"/dns/auto", nil, john); w.Code != 409 || decode[errBody](t, w).Error.Code != "mail_dns_not_delegated" {
		t.Fatalf("на чужих серверах кнопка не работает: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", mbase+"/dns/auto", nil, mary); w.Code != 404 {
		t.Fatalf("чужой домен: %d", w.Code)
	}
	res.set("mine.example.com", "ns.vladinc.ru", "ns2.vladinc.ru")
	if md = decode[mailDNS](t, e.do("GET", mbase+"/dns", nil, john)); !md.Auto.Delegated || md.Auto.State != "ok" {
		t.Fatalf("на наших серверах: %+v", md)
	}
	w = e.do("POST", mbase+"/dns/auto", nil, john)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	zoneText, _ := os.ReadFile(filepath.Join(dns.zones, "mine.example.com.zone"))
	for _, want := range []string{"@ 300 IN MX 10 mail.vladinc.ru.", `@ 300 IN TXT "v=spf1 mx ip4:` + serverIP + ` ~all"`, "vh1._domainkey 300 IN TXT", `_dmarc 300 IN TXT "v=DMARC1;`} {
		if !strings.Contains(string(zoneText), want) {
			t.Errorf("в зоне нет %q:\n%s", want, zoneText)
		}
	}
	ov = decode[overview](t, e.do("GET", "/api/dns", nil, john))
	managed := 0
	for _, r := range ov.Zones[0].Records {
		if r.Managed == "mail" {
			managed++
		}
	}
	if managed != 4 {
		t.Fatalf("почтовых записей %d", managed)
	}

	// удаление
	if w := e.do("DELETE", fmt.Sprintf("/api/dns/records/%d", rec), nil, john); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if w := e.do("DELETE", base, nil, john); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dns.zones, "mine.example.com.zone")); err == nil {
		t.Fatal("файл зоны должен исчезнуть")
	}
}

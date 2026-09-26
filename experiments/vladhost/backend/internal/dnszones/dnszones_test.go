package dnszones

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vladhost/internal/apperr"
	"vladhost/internal/auth"
	"vladhost/internal/runtimes"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

var bg = context.Background()

// realHelper — исполнитель, который запускает настоящий deploy/bin/dns-sync.py на временных каталогах (без BIND): так всё, что панель пишет
// в state.json, проверяется тем же кодом, который на сервере собирает зоны.
type realHelper struct {
	t        *testing.T
	dir      string
	zones    string
	conf     string
	mu       sync.Mutex
	calls    int
	fail     string
	down     bool
	failures int
	lastOut  string
}

func (h *realHelper) Do(_ context.Context, r runtimes.Request, _ time.Duration) (runtimes.Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	if h.down {
		return runtimes.Result{}, errors.New("no answer")
	}
	if h.failures > 0 {
		h.failures--
		return runtimes.Result{Error: "io_13"}, nil
	}
	if h.fail != "" {
		return runtimes.Result{Error: h.fail}, nil
	}
	if r.Action != ActionDNSSync {
		h.t.Fatalf("неожиданное действие %q", r.Action)
	}
	script, _ := filepath.Abs("../../../deploy/bin/dns-sync.py")
	out, err := exec.Command("python3", script, "--state", filepath.Join(h.dir, "dns", "state.json"), "--zones", h.zones, "--conf", h.conf,
		"--checkzone", "", "--checkconf", "", "--rndc", "", "--group", "").CombinedOutput()
	h.lastOut = strings.TrimSpace(string(out))
	if err != nil || h.lastOut != "ok" {
		return runtimes.Result{Error: strings.TrimPrefix(h.lastOut, "error="), Output: h.lastOut}, nil
	}
	return runtimes.Result{OK: true}, nil
}

type fakeSites struct {
	mu      sync.Mutex
	domains map[int64][]sites.Domain
}

func (f *fakeSites) DomainsByUser(_ context.Context, uid int64) (map[int64][]sites.Domain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return map[int64][]sites.Domain{1: append([]sites.Domain(nil), f.domains[uid]...)}, nil
}

type fakeResolver struct {
	mu  sync.Mutex
	ns  map[string][]string
	txt map[string][]string
	err error
}

func (f *fakeResolver) LookupTXT(_ context.Context, n string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.txt[n], nil
}

func (f *fakeResolver) setTXT(name string, values ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txt[name] = values
}

func (f *fakeResolver) LookupNS(_ context.Context, n string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ns[n], f.err
}

func (f *fakeResolver) set(domain string, ns ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ns[domain] = ns
}

type env struct {
	t      *testing.T
	svc    *Service
	helper *realHelper
	sites  *fakeSites
	res    *fakeResolver
	auth   *auth.Service
	user   auth.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("нужен python3 для настоящего dns-sync.py")
	}
	db := testdb.Open(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rt", "dns"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &realHelper{t: t, dir: filepath.Join(root, "rt"), zones: filepath.Join(root, "zones"), conf: filepath.Join(root, "named.conf.vladhost")}
	fs := &fakeSites{domains: map[int64][]sites.Domain{}}
	res := &fakeResolver{ns: map[string][]string{}, txt: map[string][]string{}}
	e := &env{t: t, helper: h, sites: fs, res: res}
	e.svc = New(db, Config{NS: []string{"ns.vladinc.ru", "NS2.vladinc.ru."}, Hostmaster: "hostmaster.vladinc.ru", ServerIPs: []string{"203.0.113.10"}, BaseDomain: "vladinc.ru",
		Dir: h.dir, Applier: h, Sites: fs, Resolver: res})
	e.auth = auth.NewService(db, []byte(strings.Repeat("s", 32)), time.Minute, time.Hour)
	e.user = e.newUser()
	return e
}

func (e *env) newUser() auth.User {
	name := fmt.Sprintf("dn%d", time.Now().UnixNano()%1_000_000_000)
	u, err := e.auth.CreateAdmin(bg, name+"@example.com", name, "password123")
	if err != nil {
		e.t.Fatal(err)
	}
	return *u
}

func (e *env) attach(u auth.User, domains ...string) {
	e.sites.mu.Lock()
	defer e.sites.mu.Unlock()
	for _, d := range domains {
		e.sites.domains[u.ID] = append(e.sites.domains[u.ID], sites.Domain{Host: d, Kind: "custom"})
	}
}

func (e *env) zoneFile(domain string) string {
	raw, err := os.ReadFile(filepath.Join(e.helper.zones, domain+".zone"))
	if err != nil {
		e.t.Fatalf("%s: %v", domain, err)
	}
	return string(raw)
}

func (e *env) enable(u auth.User, domain string) *Zone {
	e.t.Helper()
	e.attach(u, domain)
	z, err := e.svc.EnableZone(bg, u.ID, domain)
	if err != nil {
		e.t.Fatalf("EnableZone %s: %v", domain, err)
	}
	return z
}

func code(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func TestEnableZoneWritesDefaultsAndConfig(t *testing.T) {
	e := newEnv(t)
	// домен вводят как придётся: пробелы, регистр и точка в конце убираются
	e.attach(e.user, "example.com")
	z, err := e.svc.EnableZone(bg, e.user.ID, "  Example.COM.")
	if err != nil {
		t.Fatal(err)
	}
	text := e.zoneFile("example.com")
	for _, want := range []string{"$ORIGIN example.com.", "@ IN NS ns.vladinc.ru.", "@ IN NS ns2.vladinc.ru.", "@ 300 IN A 203.0.113.10", "www 300 IN CNAME example.com."} {
		if !strings.Contains(text, want) {
			t.Errorf("в зоне нет %q:\n%s", want, text)
		}
	}
	conf, err := os.ReadFile(e.helper.conf)
	if err != nil || !strings.Contains(string(conf), `zone "example.com" { type primary;`) {
		t.Fatalf("%s %v", conf, err)
	}
	if z.Serial < time.Now().Add(-time.Minute).Unix() {
		t.Fatalf("серийный номер %d", z.Serial)
	}
	list, _ := e.svc.List(bg, e.user.ID)
	if len(list) != 1 || len(list[0].Records) != 2 {
		t.Fatalf("%+v", list)
	}
}

func TestEnableZoneRules(t *testing.T) {
	e := newEnv(t)
	other := e.newUser()
	for _, tc := range []struct{ domain, want string }{
		{"blog.user.vladinc.ru", "validation.dns_domain_base"},
		{"x.vladinc.ru", "validation.dns_domain_base"},
		{"vladinc.ru", "validation.dns_domain_base"},
		{"not a domain", "validation.dns_domain"},
		{"localhost", "validation.dns_domain"},
		{"a..com", "validation.dns_domain"},
		{"", "validation.dns_domain"},
	} {
		if _, err := e.svc.EnableZone(bg, e.user.ID, tc.domain); code(err) != tc.want {
			t.Errorf("%q: %v (код %q, ждали %q)", tc.domain, err, code(err), tc.want)
		}
	}
	// добавить можно любой домен: без привязки к сайту зона ждёт подтверждения
	free, err := e.svc.EnableZone(bg, e.user.ID, "free.org")
	if err != nil || free.Verified || free.Token == "" {
		t.Fatalf("%+v %v", free, err)
	}
	// подключён к сайту этого пользователя — подтверждён сразу
	e.attach(e.user, "mine.com")
	mine, err := e.svc.EnableZone(bg, e.user.ID, "mine.com")
	if err != nil || !mine.Verified {
		t.Fatalf("%+v %v", mine, err)
	}
	if _, err := e.svc.EnableZone(bg, e.user.ID, "mine.com"); code(err) != "dns_zone_taken" {
		t.Fatalf("повтор: %v", err)
	}
	if _, err := e.svc.EnableZone(bg, e.user.ID, "FREE.org."); code(err) != "dns_zone_taken" {
		t.Fatalf("повтор неподтверждённого: %v", err)
	}
	// чужой домен, подтверждённый другим: другой пользователь может подать заявку, но подтвердить не сможет
	theirs, err := e.svc.EnableZone(bg, other.ID, "mine.com")
	if err != nil || theirs.Verified {
		t.Fatalf("заявка на чужой домен: %+v %v", theirs, err)
	}
	if _, err := e.svc.Verify(bg, other.ID, theirs.ID); code(err) != "dns_not_verified" {
		t.Fatalf("без записи: %v", err)
	}
	e.res.setTXT("_vladhost-verify.mine.com", "vladhost-verify="+theirs.Token)
	if _, err := e.svc.Verify(bg, other.ID, theirs.ID); code(err) != "dns_zone_taken" {
		t.Fatalf("домен уже подтверждён другим пользователем: %v", err)
	}
	// «подсказки» — свои домены с сайтов, для которых у пользователя ещё нет зоны
	e.attach(e.user, "later.net")
	if el, _ := e.svc.Eligible(bg, e.user.ID); len(el) != 1 || el[0] != "later.net" {
		t.Fatalf("%v", el)
	}
	if el, _ := e.svc.Eligible(bg, other.ID); len(el) != 0 {
		t.Fatalf("%v", el)
	}
}

func TestVerifyByTXTPublishesTheZoneAndRevokesRivals(t *testing.T) {
	e := newEnv(t)
	rival := e.newUser()
	john, err := e.svc.EnableZone(bg, e.user.ID, "shop.org")
	if err != nil {
		t.Fatal(err)
	}
	mary, err := e.svc.EnableZone(bg, rival.ID, "shop.org")
	if err != nil {
		t.Fatalf("две заявки на один домен допустимы, пока он не подтверждён: %v", err)
	}
	// неподтверждённая зона на сервер не попадает, но записи можно готовить заранее
	if _, err := e.svc.SaveRecord(bg, e.user.ID, john.ID, 0, RecordInput{Name: "blog", Type: "A", Value: "203.0.113.50"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.helper.zones, "shop.org.zone")); err == nil {
		t.Fatal("неподтверждённая зона не должна обслуживаться")
	}
	list, _ := e.svc.List(bg, e.user.ID)
	if len(list) != 1 || list[0].Verified || list[0].VerifyName != "_vladhost-verify.shop.org" || list[0].VerifyValue != "vladhost-verify="+john.Token || len(list[0].Records) != 3 {
		t.Fatalf("%+v", list)
	}
	if st, _ := e.svc.Status(bg, e.user.ID, "shop.org"); st.Zone || st.Delegated {
		t.Fatalf("неподтверждённая зона не считается: %+v", st)
	}
	e.res.set("shop.org", "ns.vladinc.ru", "ns2.vladinc.ru")
	if err := e.svc.ApplyMail(bg, e.user.ID, "shop.org", nil); code(err) != "dns_not_found" {
		t.Fatalf("почта ставится только в подтверждённую зону: %v", err)
	}

	// чужой код не подходит, свой — подходит
	if _, err := e.svc.Verify(bg, e.user.ID, john.ID); code(err) != "dns_not_verified" {
		t.Fatalf("%v", err)
	}
	e.res.setTXT("_vladhost-verify.shop.org", "vladhost-verify="+mary.Token)
	if _, err := e.svc.Verify(bg, e.user.ID, john.ID); code(err) != "dns_not_verified" {
		t.Fatalf("чужой код: %v", err)
	}
	e.res.setTXT("_vladhost-verify.shop.org", "vladhost-verify="+john.Token)
	z, err := e.svc.Verify(bg, e.user.ID, john.ID)
	if err != nil || !z.Verified {
		t.Fatalf("%+v %v", z, err)
	}
	text := e.zoneFile("shop.org")
	if !strings.Contains(text, "blog 300 IN A 203.0.113.50") || !strings.Contains(text, "@ 300 IN A 203.0.113.10") {
		t.Fatalf("после подтверждения зона обслуживается:\n%s", text)
	}
	// заявка соперника отозвана: ей больше нет ни в списке, ни в базе
	if list, _ := e.svc.List(bg, rival.ID); len(list) != 0 {
		t.Fatalf("заявка соперника должна исчезнуть: %+v", list)
	}
	if _, err := e.svc.Verify(bg, rival.ID, mary.ID); code(err) != "dns_not_found" {
		t.Fatalf("%v", err)
	}
	// повторное подтверждение безвредно, а после подтверждения на домен нельзя подать заявку так, чтобы вытеснить владельца
	if _, err := e.svc.Verify(bg, e.user.ID, john.ID); err != nil {
		t.Fatal(err)
	}
	if again, err := e.svc.EnableZone(bg, rival.ID, "shop.org"); err != nil || again.Verified {
		t.Fatalf("новая заявка остаётся неподтверждённой: %+v %v", again, err)
	}
	// чужую зону подтвердить нельзя
	if _, err := e.svc.Verify(bg, rival.ID, john.ID); code(err) != "dns_not_found" {
		t.Fatalf("%v", err)
	}
	if err := e.svc.DeleteZone(bg, e.user.ID, john.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.helper.zones, "shop.org.zone")); err == nil {
		t.Fatal("удалённая зона должна исчезнуть с сервера")
	}
}

func TestSiteDomainVerifiesAfterTheFactAndUnverifiedDeleteIsQuiet(t *testing.T) {
	e := newEnv(t)
	z, err := e.svc.EnableZone(bg, e.user.ID, "later.com")
	if err != nil || z.Verified {
		t.Fatalf("%+v %v", z, err)
	}
	// потом домен подключили к сайту — этого достаточно, TXT не нужен
	e.attach(e.user, "later.com")
	if v, err := e.svc.Verify(bg, e.user.ID, z.ID); err != nil || !v.Verified {
		t.Fatalf("%+v %v", v, err)
	}
	if !strings.Contains(e.zoneFile("later.com"), "$ORIGIN later.com.") {
		t.Fatal("зона должна обслуживаться")
	}
	// удаление неподтверждённой зоны исполнителя не беспокоит
	u, _ := e.svc.EnableZone(bg, e.user.ID, "quiet.org")
	e.helper.mu.Lock()
	before := e.helper.calls
	e.helper.mu.Unlock()
	if err := e.svc.DeleteZone(bg, e.user.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	e.helper.mu.Lock()
	after := e.helper.calls
	e.helper.mu.Unlock()
	if after != before {
		t.Fatalf("вызовов исполнителя: было %d, стало %d", before, after)
	}
}

func TestZoneLimit(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < MaxZones; i++ {
		e.enable(e.user, fmt.Sprintf("d%d.com", i))
	}
	e.attach(e.user, "one-more.com")
	if _, err := e.svc.EnableZone(bg, e.user.ID, "one-more.com"); code(err) != "dns_zone_limit" {
		t.Fatalf("%v", err)
	}
}

func TestRecordCrudAndSerial(t *testing.T) {
	e := newEnv(t)
	z := e.enable(e.user, "example.com")
	serial := func() int64 {
		list, _ := e.svc.List(bg, e.user.ID)
		var s int64
		e.svc.db.Model(&Zone{}).Where("id = ?", list[0].ID).Select("serial").Scan(&s)
		return s
	}
	s0 := serial()
	r, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, RecordInput{Name: "Mail", Type: "a", Value: "203.0.113.20", TTL: 600})
	if err != nil || r.Name != "mail" || r.Type != "A" || r.TTL != 600 {
		t.Fatalf("%+v %v", r, err)
	}
	if !strings.Contains(e.zoneFile("example.com"), "mail 600 IN A 203.0.113.20") {
		t.Fatal("запись не попала в зону")
	}
	if serial() <= s0 {
		t.Fatal("серийный номер должен расти")
	}
	// пустое имя — вершина зоны, TTL по умолчанию; MX и TXT
	if _, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, RecordInput{Type: "MX", Value: "Mail.Example.COM.", Priority: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, RecordInput{Type: "TXT", Value: `v=spf1 include:"x" -all`}); err != nil {
		t.Fatal(err)
	}
	text := e.zoneFile("example.com")
	if !strings.Contains(text, "@ 300 IN MX 10 mail.example.com.") || !strings.Contains(text, `@ 300 IN TXT "v=spf1 include:\"x\" -all"`) {
		t.Fatalf("%s", text)
	}
	// замена записи
	upd, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, r.ID, RecordInput{Name: "mail", Type: "A", Value: "203.0.113.21", TTL: 300})
	if err != nil || upd.ID != r.ID || upd.Value != "203.0.113.21" {
		t.Fatalf("%+v %v", upd, err)
	}
	if strings.Contains(e.zoneFile("example.com"), "203.0.113.20") {
		t.Fatal("старое значение осталось")
	}
	if err := e.svc.DeleteRecord(bg, e.user.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.zoneFile("example.com"), "mail 300 IN A") {
		t.Fatal("запись не удалена")
	}
	if err := e.svc.DeleteRecord(bg, e.user.ID, r.ID); code(err) != "dns_not_found" {
		t.Fatalf("повторное удаление: %v", err)
	}
	// удаление зоны
	if err := e.svc.DeleteZone(bg, e.user.ID, z.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.helper.zones, "example.com.zone")); err == nil {
		t.Fatal("файл зоны должен исчезнуть")
	}
	if list, _ := e.svc.List(bg, e.user.ID); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
}

func TestRecordValidation(t *testing.T) {
	e := newEnv(t)
	z := e.enable(e.user, "example.com")
	long := strings.Repeat("x", maxTXT+1)
	cases := []struct {
		name string
		in   RecordInput
		want string
	}{
		{"тип", RecordInput{Type: "NS", Value: "ns1.example.org"}, "validation.dns_type"},
		{"soa", RecordInput{Type: "SOA", Value: "x"}, "validation.dns_type"},
		{"имя с пробелом", RecordInput{Name: "bad name", Type: "A", Value: "1.2.3.4"}, "validation.dns_name"},
		{"имя с точками", RecordInput{Name: "a..b", Type: "A", Value: "1.2.3.4"}, "validation.dns_name"},
		{"звёздочка в середине", RecordInput{Name: "a.*.b", Type: "A", Value: "1.2.3.4"}, "validation.dns_name"},
		{"ttl", RecordInput{Type: "A", Value: "1.2.3.4", TTL: 301}, "validation.dns_ttl"},
		{"a", RecordInput{Type: "A", Value: "999.1.1.1"}, "validation.dns_value"},
		{"a нулевой", RecordInput{Type: "A", Value: "0.0.0.0"}, "validation.dns_value"},
		{"a с нулём в начале", RecordInput{Type: "A", Value: "010.0.0.1"}, "validation.dns_value"},
		{"a два адреса", RecordInput{Type: "A", Value: "1.1.1.1 2.2.2.2"}, "validation.dns_value"},
		{"a из ipv6", RecordInput{Type: "A", Value: "::1"}, "validation.dns_value"},
		{"aaaa", RecordInput{Type: "AAAA", Value: "1.2.3.4"}, "validation.dns_value"},
		{"cname на ip", RecordInput{Name: "w", Type: "CNAME", Value: "1.2.3.4"}, "validation.dns_value"},
		{"cname с пробелом", RecordInput{Name: "w", Type: "CNAME", Value: "x.example.org ; evil"}, "validation.dns_value"},
		{"cname у вершины", RecordInput{Type: "CNAME", Value: "x.example.org"}, "validation.dns_cname_apex"},
		{"mx приоритет", RecordInput{Type: "MX", Value: "m.example.org", Priority: 70000}, "validation.dns_priority"},
		{"mx на ip", RecordInput{Type: "MX", Value: "1.2.3.4", Priority: 10}, "validation.dns_value"},
		{"txt пустой", RecordInput{Type: "TXT", Value: "  "}, "validation.dns_value"},
		{"txt длинный", RecordInput{Type: "TXT", Value: long}, "validation.dns_value"},
		{"txt с переводом строки", RecordInput{Type: "TXT", Value: "a\nb"}, "validation.dns_value"},
		{"srv", RecordInput{Name: "_s._tcp", Type: "SRV", Value: "0 80", Priority: 1}, "validation.dns_value"},
		{"srv порт", RecordInput{Name: "_s._tcp", Type: "SRV", Value: "5 0 host.example.org", Priority: 1}, "validation.dns_value"},
		{"caa метка", RecordInput{Type: "CAA", Value: "0 evil letsencrypt.org"}, "validation.dns_value"},
		{"caa флаг", RecordInput{Type: "CAA", Value: "1 issue letsencrypt.org"}, "validation.dns_value"},
	}
	for _, tc := range cases {
		if _, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, tc.in); code(err) != tc.want {
			t.Errorf("%s: %v (код %q, ждали %q)", tc.name, err, code(err), tc.want)
		}
	}
	// допустимые редкие записи
	for _, in := range []RecordInput{
		{Name: "_sip._tcp", Type: "SRV", Value: "5 5060 sip.example.org.", Priority: 10},
		{Type: "CAA", Value: "0 issue letsencrypt.org"},
		{Name: "*.dev", Type: "AAAA", Value: "2001:DB8::1"},
	} {
		if _, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, in); err != nil {
			t.Errorf("%+v: %v", in, err)
		}
	}
	if !strings.Contains(e.zoneFile("example.com"), "*.dev 300 IN AAAA 2001:db8::1") {
		t.Fatal("ipv6 должен записываться в каноническом виде")
	}
}

func TestRecordConflictsAndLimit(t *testing.T) {
	e := newEnv(t)
	z := e.enable(e.user, "example.com") // www CNAME, @ A
	save := func(in RecordInput) error {
		_, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, in)
		return err
	}
	if err := save(RecordInput{Name: "www", Type: "A", Value: "1.2.3.4"}); code(err) != "dns_cname_conflict" {
		t.Fatalf("A рядом с CNAME: %v", err)
	}
	if err := save(RecordInput{Name: "www", Type: "CNAME", Value: "other.example.org"}); code(err) != "dns_cname_conflict" {
		t.Fatalf("второй CNAME: %v", err)
	}
	if err := save(RecordInput{Name: "mail", Type: "A", Value: "1.2.3.4"}); err != nil {
		t.Fatal(err)
	}
	if err := save(RecordInput{Name: "mail", Type: "CNAME", Value: "x.example.org"}); code(err) != "dns_cname_conflict" {
		t.Fatalf("CNAME рядом с A: %v", err)
	}
	if err := save(RecordInput{Name: "mail", Type: "A", Value: "1.2.3.4"}); code(err) != "dns_record_duplicate" {
		t.Fatalf("повтор: %v", err)
	}
	if err := save(RecordInput{Name: "mail", Type: "A", Value: "1.2.3.5"}); err != nil {
		t.Fatalf("второй адрес у одного имени допустим: %v", err)
	}
	for i := 0; ; i++ {
		err := save(RecordInput{Name: fmt.Sprintf("h%d", i), Type: "A", Value: "1.2.3.4"})
		if err != nil {
			if code(err) != "dns_record_limit" {
				t.Fatalf("%v", err)
			}
			break
		}
		if i > MaxRecords {
			t.Fatal("лимит не сработал")
		}
	}
	// чужая запись и чужая зона недоступны
	other := e.newUser()
	if _, err := e.svc.SaveRecord(bg, other.ID, z.ID, 0, RecordInput{Type: "A", Value: "1.2.3.4"}); code(err) != "dns_not_found" {
		t.Fatalf("%v", err)
	}
	list, _ := e.svc.List(bg, e.user.ID)
	if err := e.svc.DeleteRecord(bg, other.ID, list[0].Records[0].ID); code(err) != "dns_not_found" {
		t.Fatalf("%v", err)
	}
	if err := e.svc.DeleteZone(bg, other.ID, z.ID); code(err) != "dns_not_found" {
		t.Fatalf("%v", err)
	}
	if _, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 999999, RecordInput{Type: "A", Value: "1.2.3.4"}); code(err) != "dns_not_found" {
		t.Fatalf("несуществующая запись: %v", err)
	}
}

func TestDelegationStates(t *testing.T) {
	e := newEnv(t)
	z := e.enable(e.user, "example.com")
	cases := []struct {
		ns   []string
		want string
	}{
		{[]string{"ns.vladinc.ru.", "NS2.vladinc.ru"}, DelegationOK},
		{[]string{"ns.vladinc.ru"}, DelegationPartial},
		{[]string{"ns.vladinc.ru", "ns2.vladinc.ru", "ns.majordomo.ru"}, DelegationMixed},
		{[]string{"ns.majordomo.ru", "ns2.majordomo.ru"}, DelegationNone},
		{nil, DelegationNone},
	}
	for _, tc := range cases {
		e.res.set("example.com", tc.ns...)
		d, err := e.svc.Delegation(bg, e.user.ID, z.ID)
		if err != nil || d.State != tc.want {
			t.Errorf("%v: %+v %v (ждали %s)", tc.ns, d, err, tc.want)
		}
		if got := d.Delegated(); got != (tc.want == DelegationOK || tc.want == DelegationPartial) {
			t.Errorf("%v: Delegated() = %v", tc.ns, got)
		}
	}
	e.res.err = errors.New("timeout")
	e.res.set("example.com")
	if d, _ := e.svc.Delegation(bg, e.user.ID, z.ID); d.State != DelegationUnknown {
		t.Fatalf("ошибка DNS: %+v", d)
	}
	other := e.newUser()
	if _, err := e.svc.Delegation(bg, other.ID, z.ID); code(err) != "dns_not_found" {
		t.Fatalf("%v", err)
	}
}

func TestApplyMailReplacesOldMailRecordsAndNeedsDelegation(t *testing.T) {
	e := newEnv(t)
	z := e.enable(e.user, "example.com")
	mine := []MailRecord{
		{Kind: "mx", Type: "MX", Name: "example.com", Value: "mail.vladinc.ru", Priority: 10},
		{Kind: "spf", Type: "TXT", Name: "example.com", Value: "v=spf1 mx ip4:203.0.113.10 ~all"},
		{Kind: "dkim", Type: "TXT", Name: "vh1._domainkey.example.com", Value: "v=DKIM1; k=rsa; p=ABC"},
		{Kind: "dmarc", Type: "TXT", Name: "_dmarc.example.com", Value: "v=DMARC1; p=none; rua=mailto:postmaster@example.com"},
	}
	if err := e.svc.ApplyMail(bg, e.user.ID, "example.com", mine); code(err) != "dns_not_delegated" {
		t.Fatalf("без делегирования: %v", err)
	}
	e.res.set("example.com", "ns.vladinc.ru", "ns2.vladinc.ru")
	// у пользователя уже есть свои записи: старый MX, чужой SPF, ключ DKIM с другим значением и посторонний TXT
	for _, in := range []RecordInput{
		{Type: "MX", Value: "old.example.org", Priority: 5},
		{Type: "TXT", Value: "v=spf1 include:_spf.google.com ~all"},
		{Type: "TXT", Value: "google-site-verification=abc"},
		{Name: "vh1._domainkey", Type: "TXT", Value: "v=DKIM1; k=rsa; p=OLD"},
	} {
		if _, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, in); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.svc.ApplyMail(bg, e.user.ID, "example.com", mine); err != nil {
		t.Fatal(err)
	}
	text := e.zoneFile("example.com")
	for _, want := range []string{"@ 300 IN MX 10 mail.vladinc.ru.", `@ 300 IN TXT "v=spf1 mx ip4:203.0.113.10 ~all"`, `vh1._domainkey 300 IN TXT "v=DKIM1; k=rsa; p=ABC"`,
		`_dmarc 300 IN TXT "v=DMARC1; p=none; rua=mailto:postmaster@example.com"`, `@ 300 IN TXT "google-site-verification=abc"`, "@ 300 IN A 203.0.113.10"} {
		if !strings.Contains(text, want) {
			t.Errorf("в зоне нет %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"old.example.org", "_spf.google.com", "p=OLD"} {
		if strings.Contains(text, gone) {
			t.Errorf("старое осталось: %q", gone)
		}
	}
	// повторное нажатие ничего не дублирует
	if err := e.svc.ApplyMail(bg, e.user.ID, "example.com", mine); err != nil {
		t.Fatal(err)
	}
	list, _ := e.svc.List(bg, e.user.ID)
	managed := 0
	for _, r := range list[0].Records {
		if r.Managed == ManagedMail {
			managed++
		}
	}
	if managed != 4 || len(list[0].Records) != 2+4+1 { // A, CNAME www + четыре записи почты + чужой TXT
		t.Fatalf("записей %d, из них почтовых %d", len(list[0].Records), managed)
	}
	// чужая зона и домен без зоны
	other := e.newUser()
	if err := e.svc.ApplyMail(bg, other.ID, "example.com", mine); code(err) != "dns_not_found" {
		t.Fatalf("%v", err)
	}
	if err := e.svc.ApplyMail(bg, e.user.ID, "nothing.org", mine); code(err) != "dns_not_found" {
		t.Fatalf("%v", err)
	}
	// имя вне зоны отвергается
	bad := []MailRecord{{Kind: "spf", Type: "TXT", Name: "other.org", Value: "v=spf1 -all"}}
	if err := e.svc.ApplyMail(bg, e.user.ID, "example.com", bad); code(err) != "validation.dns_name" {
		t.Fatalf("%v", err)
	}
	st, _ := e.svc.Status(bg, e.user.ID, "example.com")
	if !st.Zone || !st.Delegated || st.Delegation.State != DelegationOK {
		t.Fatalf("%+v", st)
	}
	st, _ = e.svc.Status(bg, e.user.ID, "nothing.org")
	if st.Zone || st.Delegated {
		t.Fatalf("%+v", st)
	}
}

func TestSyncFailureKeepsDataAndIsRetried(t *testing.T) {
	e := newEnv(t)
	e.helper.failures = 1
	e.attach(e.user, "example.com")
	z, err := e.svc.EnableZone(bg, e.user.ID, "example.com")
	if code(err) != "dns_sync_failed" || z == nil {
		t.Fatalf("%v", err)
	}
	if list, _ := e.svc.List(bg, e.user.ID); len(list) != 1 {
		t.Fatal("данные сохранены, несмотря на сбой синхронизации")
	}
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	go e.svc.Watch(ctx, 50*time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(e.helper.zones, "example.com.zone")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Watch не применил состояние")
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.helper.down = true
	if _, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, RecordInput{Name: "x", Type: "A", Value: "1.2.3.4"}); code(err) != "dns_helper_down" {
		t.Fatalf("%v", err)
	}
	e.helper.down = false
	e.helper.fail = "record_ttl"
	if _, err := e.svc.SaveRecord(bg, e.user.ID, z.ID, 0, RecordInput{Name: "y", Type: "A", Value: "1.2.3.4"}); code(err) != "dns_sync_failed" {
		t.Fatalf("%v", err)
	}
}

func TestDisabledWithoutConfig(t *testing.T) {
	var s *Service
	if s.Enabled() {
		t.Fatal("nil-служба выключена")
	}
	if (&Service{}).Enabled() {
		t.Fatal("без серверов имён и папки DNS выключен")
	}
}

func TestNoZonesMeansNoStartupSync(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(bg)
	go e.svc.Watch(ctx, 20*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	cancel()
	if e.helper.calls != 0 {
		t.Fatalf("без зон исполнителя беспокоить не нужно: %d вызовов", e.helper.calls)
	}
}

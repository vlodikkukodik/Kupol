package httpapi_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vladhost/internal/sites"
)

const serverIP = "203.0.113.10"

// fakeDNS — подменяемый DNS: ответы по имени; отсутствие записи = "no such host".
type fakeDNS struct {
	mu   sync.Mutex
	recs map[string][]string
	fail map[string]bool // ошибка, не связанная с отсутствием записи
	// relative — имена, запрошенные без точки в конце (это ошибка: см. LookupIPAddr).
	relative []string
}

func (f *fakeDNS) set(host string, ips ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recs == nil {
		f.recs = map[string][]string{}
	}
	f.recs[host] = ips
}

func (f *fakeDNS) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Как у настоящего резолвера сервера: относительное имя достраивается search-доменом хостера, у которого
	// есть wildcard-запись на наш IP, — любое имя «оказывается» нашим. Проверка обязана запрашивать абсолютные имена.
	if !strings.HasSuffix(host, ".") {
		f.relative = append(f.relative, host)
		return []net.IPAddr{{IP: net.ParseIP(serverIP)}}, nil
	}
	host = strings.TrimSuffix(host, ".")
	if f.fail[host] {
		return nil, &net.DNSError{Err: "server misbehaving", Name: host, IsTemporary: true}
	}
	ips, ok := f.recs[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	var out []net.IPAddr
	for _, s := range ips {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out, nil
}

type domainResp struct {
	Domain struct {
		ID      int64    `json:"id"`
		Host    string   `json:"host"`
		Status  string   `json:"status"`
		Problem string   `json:"problem"`
		Found   []string `json:"found"`
		Error   string   `json:"error"`
	} `json:"domain"`
}

// withDomains включает свои домены: подменённый DNS и папка привязок во временном каталоге.
func (e *env) withDomains(perSite, perUser int) (*fakeDNS, string) {
	e.t.Helper()
	dns := &fakeDNS{}
	dir := filepath.Join(e.t.TempDir(), "domains")
	e.sites.ConfigureDomains(sites.DomainConfig{ServerIPs: []string{serverIP}, MappingDir: dir, Resolver: dns, PerSite: perSite, PerUser: perUser})
	return dns, dir
}

func (e *env) addDomain(tok string, siteID int64, host string) (int, domainResp, string) {
	e.t.Helper()
	w := e.do("POST", "/api/sites/"+itoa(siteID)+"/domains", map[string]string{"host": host}, tok)
	if w.Code != 201 {
		return w.Code, domainResp{}, w.Body.String()
	}
	return w.Code, decode[domainResp](e.t, w), w.Body.String()
}

func TestDomainsUnavailableWithoutConfig(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, _ := e.createSite(tok, "blog")
	w := e.do("POST", "/api/sites/"+itoa(id)+"/domains", map[string]string{"host": "example.com"}, tok)
	if w.Code != 409 || decode[errBody](t, w).Error.Code != "domains_unavailable" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if body := e.do("GET", "/api/sites", nil, tok).Body.String(); !strings.Contains(body, `"available":false`) {
		t.Fatalf("список сайтов должен сообщать, что домены недоступны: %s", body)
	}
}

// Регрессия: на боевом сервере search-домен хостера (с wildcard на наш IP) делал любое чужое имя «нашим»,
// и можно было занять чужой домен, не владея им.
func TestDNSCheckIsNotFooledBySearchDomain(t *testing.T) {
	e := newEnv(t)
	dns, _ := e.withDomains(10, 10)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, _ := e.createSite(tok, "blog")

	_, d, body := e.addDomain(tok, id, "victim-domain.com") // в DNS записей нет
	if d.Domain.Status != "pending_dns" || d.Domain.Problem != "no_a" {
		t.Fatalf("чужой домен без записи не должен считаться подтверждённым: %+v %s", d.Domain, body)
	}
	if len(dns.relative) != 0 {
		t.Fatalf("DNS запрошен по относительным именам %v: их достраивает search-домен", dns.relative)
	}
	if _, err := os.Stat(filepath.Join(e.certs, "queue", "issue-victim-domain.com")); err == nil {
		t.Fatal("заявка на сертификат не должна ставиться без верной A-записи")
	}
}

func TestDomainNameValidation(t *testing.T) {
	e := newEnv(t)
	e.withDomains(50, 50)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, _ := e.createSite(tok, "blog")

	bad := map[string]string{
		"":                               "domain_invalid",
		"   ":                            "domain_invalid",
		"http://example.com":             "domain_invalid",
		"example.com/path":               "domain_invalid",
		"user@example.com":               "domain_invalid",
		"exa mple.com":                   "domain_invalid",
		"*.example.com":                  "domain_invalid",
		"example..com":                   "domain_invalid",
		"-example.com":                   "domain_invalid",
		"example-.com":                   "domain_invalid",
		"example":                        "domain_invalid",
		"com":                            "domain_invalid",
		"co.uk":                          "domain_invalid",
		"github.io":                      "domain_invalid",
		"192.168.0.1":                    "domain_invalid",
		"example.123":                    "domain_invalid",
		"[::1]":                          "domain_invalid",
		strings.Repeat("a", 64) + ".com": "domain_invalid",
		"example.com:8080":               "domain_invalid",
		"exa\x00mple.com":                "domain_invalid",
		"vladinc.ru":                     "domain_reserved",
		"app.vladinc.ru":                 "domain_reserved",
		"blog.john.vladinc.ru":           "domain_reserved",
		"WWW.VLADINC.RU.":                "domain_reserved",
		"printer.local":                  "domain_reserved",
		"site.internal":                  "domain_reserved",
		"my.test":                        "domain_reserved",
		"x.home.arpa":                    "domain_reserved",
	}
	for host, code := range bad {
		status, _, body := e.addDomain(tok, id, host)
		if status != 422 || !strings.Contains(body, `"code":"`+code+`"`) || !strings.Contains(body, `"field":"host"`) {
			t.Errorf("%q → %d %s, ожидали %s", host, status, body, code)
		}
	}

	// Нормализация: регистр, точка в конце, пробелы, IDN → punycode.
	for in, want := range map[string]string{
		"  Example.COM. ":    "example.com",
		"Blog.Example.co.uk": "blog.example.co.uk",
		"münchen.de":         "xn--mnchen-3ya.de",
		"пример.рф":          "xn--e1afmkfd.xn--p1ai",
	} {
		status, d, body := e.addDomain(tok, id, in)
		if status != 201 || d.Domain.Host != want {
			t.Errorf("%q → %d %s, ожидали хост %q", in, status, body, want)
		}
	}
}

func TestDomainDNSStates(t *testing.T) {
	e := newEnv(t)
	dns, mapping := e.withDomains(10, 10)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, siteHost := e.createSite(tok, "blog")
	check := func(did int64) domainResp {
		t.Helper()
		w := e.do("POST", "/api/sites/"+itoa(id)+"/domains/"+itoa(did)+"/check", nil, tok)
		if w.Code != 200 {
			t.Fatalf("check: %d %s", w.Code, w.Body)
		}
		return decode[domainResp](t, w)
	}

	// Записи нет.
	_, d, _ := e.addDomain(tok, id, "example.com")
	if d.Domain.Status != "pending_dns" || d.Domain.Problem != "no_a" {
		t.Fatalf("нет записи: %+v", d.Domain)
	}
	// Привязка в шлюзе создаётся сразу и указывает на адрес сайта.
	if b, err := os.ReadFile(filepath.Join(mapping, "example.com")); err != nil || strings.TrimSpace(string(b)) != siteHost {
		t.Fatalf("привязка: %q %v", b, err)
	}

	// Чужой IP.
	dns.set("example.com", "198.51.100.5")
	if r := check(d.Domain.ID); r.Domain.Status != "pending_dns" || r.Domain.Problem != "wrong_ip" || len(r.Domain.Found) != 1 || r.Domain.Found[0] != "198.51.100.5" {
		t.Fatalf("чужой IP: %+v", r.Domain)
	}
	// Наш IP и ещё чужой (round-robin): тоже неверно.
	dns.set("example.com", serverIP, "198.51.100.5")
	if r := check(d.Domain.ID); r.Domain.Problem != "wrong_ip" {
		t.Fatalf("смесь адресов: %+v", r.Domain)
	}
	// Только AAAA.
	dns.set("example.com", "2001:db8::1")
	if r := check(d.Domain.ID); r.Domain.Problem != "no_a" || len(r.Domain.Found) != 1 {
		t.Fatalf("только AAAA: %+v", r.Domain)
	}
	// Верный A, но есть AAAA: сервер работает только по IPv4.
	dns.set("example.com", serverIP, "2001:db8::1")
	if r := check(d.Domain.ID); r.Domain.Problem != "has_aaaa" || r.Domain.Status != "pending_dns" {
		t.Fatalf("AAAA: %+v", r.Domain)
	}
	// Ошибка DNS-сервера отличается от «записи нет».
	dns.fail = map[string]bool{"example.com": true}
	if r := check(d.Domain.ID); r.Domain.Problem != "lookup" {
		t.Fatalf("сбой DNS: %+v", r.Domain)
	}
	dns.fail = nil

	// Верная запись: домен переходит к выпуску сертификата и появляется заявка.
	dns.set("example.com", serverIP)
	r := check(d.Domain.ID)
	if r.Domain.Status != "pending_cert" || r.Domain.Problem != "" {
		t.Fatalf("верный DNS: %+v", r.Domain)
	}
	if _, err := os.Stat(filepath.Join(e.certs, "queue", "issue-example.com")); err != nil {
		t.Fatalf("заявка на сертификат: %v", err)
	}
	// IPv4 в виде ::ffff:a.b.c.d считается тем же адресом.
	dns.set("mapped.example.org", "::ffff:"+serverIP)
	_, m, _ := e.addDomain(tok, id, "mapped.example.org")
	if m.Domain.Status != "pending_cert" {
		t.Fatalf("IPv4-mapped: %+v", m.Domain)
	}
}

func TestDomainCertLifecycleAndRemoval(t *testing.T) {
	e := newEnv(t)
	dns, mapping := e.withDomains(10, 10)
	go e.sites.WatchDomains(t.Context(), 30*time.Millisecond)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, _ := e.createSite(tok, "blog")

	dns.set("shop.example.com", serverIP)
	_, d, _ := e.addDomain(tok, id, "shop.example.com")
	if d.Domain.Status != "pending_cert" {
		t.Fatalf("%+v", d.Domain)
	}
	state := func() (string, string) {
		list := decode[struct {
			Sites []struct {
				Domains []struct {
					Host, Status, Error string
				} `json:"domains"`
			} `json:"sites"`
		}](t, e.do("GET", "/api/sites", nil, tok))
		for _, dm := range list.Sites[0].Domains {
			if dm.Host == "shop.example.com" {
				return dm.Status, dm.Error
			}
		}
		return "", ""
	}
	waitState := func(want string) string {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			st, msg := state()
			if st == want {
				return msg
			}
			if time.Now().After(deadline) {
				t.Fatalf("статус %q, ожидали %q", st, want)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	writeStatus := func(text string) {
		dir := filepath.Join(e.certs, "status")
		_ = os.MkdirAll(dir, 0o755)
		if err := os.WriteFile(filepath.Join(dir, "shop.example.com"), []byte(text+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Ошибка выпуска доходит до пользователя.
	writeStatus("error: too many certificates already issued")
	if msg := waitState("failed"); msg != "too many certificates already issued" {
		t.Fatalf("текст ошибки: %q", msg)
	}
	// Повтор: DNS верный → снова pending_cert; старый файл статуса не закрывает новую заявку.
	_ = os.Remove(filepath.Join(e.certs, "queue", "issue-shop.example.com"))
	if w := e.do("POST", "/api/sites/"+itoa(id)+"/domains/"+itoa(d.Domain.ID)+"/check", nil, tok); decode[domainResp](t, w).Domain.Status != "pending_cert" {
		t.Fatalf("повтор: %s", w.Body)
	}
	if _, err := os.Stat(filepath.Join(e.certs, "queue", "issue-shop.example.com")); err != nil {
		t.Fatal("повтор должен ставить новую заявку")
	}
	time.Sleep(200 * time.Millisecond)
	if st, _ := state(); st != "pending_cert" {
		t.Fatalf("устаревший статус применился: %q", st)
	}
	writeStatus("ok")
	waitState("active")

	// Удаление: привязка убрана, выпускателю поставлена заявка на удаление; чужой не может.
	mary := e.user(adm, "mary")
	if w := e.do("DELETE", "/api/sites/"+itoa(id)+"/domains/"+itoa(d.Domain.ID), nil, mary); w.Code != 404 {
		t.Fatalf("чужой сайт: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/sites/"+itoa(id)+"/domains/"+itoa(d.Domain.ID), nil, tok); w.Code != 204 {
		t.Fatalf("удаление: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(mapping, "shop.example.com")); !os.IsNotExist(err) {
		t.Fatal("привязка должна быть удалена")
	}
	if _, err := os.Stat(filepath.Join(e.certs, "queue", "delete-shop.example.com")); err != nil {
		t.Fatal("заявка на удаление сертификата")
	}
	if w := e.do("DELETE", "/api/sites/"+itoa(id)+"/domains/"+itoa(d.Domain.ID), nil, tok); w.Code != 404 {
		t.Fatalf("повторное удаление: %d", w.Code)
	}
	// Домен освободился: его можно подключить другому.
	if status, _, body := e.addDomain(mary, mustSite(t, e, mary, "shop"), "shop.example.com"); status != 201 {
		t.Fatalf("освобождённый домен: %d %s", status, body)
	}
}

func mustSite(t *testing.T, e *env, tok, slug string) int64 {
	t.Helper()
	id, _ := e.createSite(tok, slug)
	return id
}

func TestDomainOwnershipUniquenessAndLimits(t *testing.T) {
	e := newEnv(t)
	e.withDomains(2, 3)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	johnSite, _ := e.createSite(john, "blog")
	marySite, _ := e.createSite(mary, "shop")

	if status, _, body := e.addDomain(john, johnSite, "one.example.com"); status != 201 {
		t.Fatal(body)
	}
	// Уникальность глобальна: чужой домен подключить нельзя, в том числе с другим регистром и точкой.
	for _, host := range []string{"one.example.com", "ONE.example.com.", "  one.EXAMPLE.com"} {
		if status, _, body := e.addDomain(mary, marySite, host); status != 409 || !strings.Contains(body, "domain_taken") {
			t.Errorf("%q: %d %s", host, status, body)
		}
	}
	// Чужой сайт недоступен.
	if status, _, _ := e.addDomain(mary, johnSite, "steal.example.com"); status != 404 {
		t.Errorf("домен на чужой сайт: %d", status)
	}
	if w := e.do("POST", "/api/sites/"+itoa(johnSite)+"/domains", map[string]string{"host": "a.example.com"}, ""); w.Code != 401 {
		t.Errorf("аноним: %d", w.Code)
	}

	// Лимит на сайт (2).
	if status, _, body := e.addDomain(john, johnSite, "two.example.com"); status != 201 {
		t.Fatal(body)
	}
	w := e.do("POST", "/api/sites/"+itoa(johnSite)+"/domains", map[string]string{"host": "three.example.com"}, john)
	if w.Code != 403 || decode[errBody](t, w).Error.Code != "domain_limit_site" {
		t.Fatalf("лимит на сайт: %d %s", w.Code, w.Body)
	}
	if got := decode[msgBody](t, e.doLang("it", "POST", "/api/sites/"+itoa(johnSite)+"/domains", map[string]string{"host": "three.example.com"}, john)).Error.Message; got != "Si possono collegare al massimo 2 domini per sito" {
		t.Fatalf("сообщение с числом: %q", got)
	}
	// Освободилось место — можно снова.
	e.do("DELETE", "/api/sites/"+itoa(johnSite)+"/domains/"+itoa(firstDomain(t, e, john)), nil, john)
	if status, _, body := e.addDomain(john, johnSite, "three.example.com"); status != 201 {
		t.Fatalf("после удаления: %d %s", status, body)
	}
}

func firstDomain(t *testing.T, e *env, tok string) int64 {
	t.Helper()
	list := decode[struct {
		Sites []struct {
			Domains []struct {
				ID int64 `json:"id"`
			} `json:"domains"`
		} `json:"sites"`
	}](t, e.do("GET", "/api/sites", nil, tok))
	return list.Sites[0].Domains[0].ID
}

func TestDomainPerUserLimit(t *testing.T) {
	e := newEnv(t)
	e.withDomains(10, 2)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	for _, h := range []string{"a.example.com", "b.example.com"} {
		if status, _, body := e.addDomain(john, id, h); status != 201 {
			t.Fatal(body)
		}
	}
	w := e.do("POST", "/api/sites/"+itoa(id)+"/domains", map[string]string{"host": "c.example.com"}, john)
	if w.Code != 403 || decode[errBody](t, w).Error.Code != "domain_limit_user" {
		t.Fatalf("лимит на аккаунт: %d %s", w.Code, w.Body)
	}
}

func TestDeletingSiteReleasesItsDomains(t *testing.T) {
	e := newEnv(t)
	_, mapping := e.withDomains(10, 10)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	for _, h := range []string{"a.example.com", "b.example.org"} {
		if status, _, body := e.addDomain(john, id, h); status != 201 {
			t.Fatal(body)
		}
	}
	if w := e.do("DELETE", "/api/sites/"+itoa(id), nil, john); w.Code != 204 {
		t.Fatalf("удаление сайта: %d", w.Code)
	}
	for _, h := range []string{"a.example.com", "b.example.org"} {
		if _, err := os.Stat(filepath.Join(mapping, h)); !os.IsNotExist(err) {
			t.Errorf("привязка %s должна быть удалена", h)
		}
		if _, err := os.Stat(filepath.Join(e.certs, "queue", "delete-"+h)); err != nil {
			t.Errorf("заявка на удаление сертификата %s: %v", h, err)
		}
	}
	var n int64
	e.db.Table("domains").Count(&n)
	if n != 0 {
		t.Fatalf("домены сайта должны удаляться каскадом: осталось %d", n)
	}
}

func TestStaleDomainIsReleased(t *testing.T) {
	e := newEnv(t)
	_, mapping := e.withDomains(10, 10)
	go e.sites.WatchDomains(t.Context(), 30*time.Millisecond)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	_, fresh, _ := e.addDomain(john, id, "fresh.example.com")
	_, old, _ := e.addDomain(john, id, "old.example.com")
	// Домен, который две недели не получил верную A-запись, освобождается (его можно занять другому).
	if err := e.db.Exec("UPDATE domains SET created_at = now() - interval '15 days' WHERE id = ?", old.Domain.ID).Error; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int64
		e.db.Table("domains").Where("id = ?", old.Domain.ID).Count(&n)
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("просроченный домен не освобождён")
		}
		time.Sleep(30 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(mapping, "old.example.com")); !os.IsNotExist(err) {
		t.Fatal("привязка просроченного домена должна быть удалена")
	}
	var n int64
	e.db.Table("domains").Where("id = ?", fresh.Domain.ID).Count(&n)
	if n != 1 {
		t.Fatal("свежий домен трогать нельзя")
	}
}

func TestDomainErrorsAreLocalized(t *testing.T) {
	e := newEnv(t)
	e.withDomains(10, 10)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	for lang, want := range map[string]string{
		"it": "Nome di dominio non valido. Indica un dominio come example.com o blog.example.com",
		"ru": "Некорректное имя домена. Укажите домен вида example.com или blog.example.com",
	} {
		w := e.doLang(lang, "POST", "/api/sites/"+itoa(id)+"/domains", map[string]string{"host": "nope"}, john)
		if got := decode[msgBody](t, w).Error; w.Code != 422 || got.Message != want || got.Field != "host" {
			t.Errorf("%s: %d %+v", lang, w.Code, got)
		}
	}
}

func TestDomainsListedWithSites(t *testing.T) {
	e := newEnv(t)
	dns, _ := e.withDomains(10, 10)
	dns.set("example.com", serverIP)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	e.addDomain(john, id, "example.com")
	body := e.do("GET", "/api/sites", nil, john).Body.String()
	for _, want := range []string{`"host":"example.com"`, `"status":"pending_cert"`, `"server_ips":["` + serverIP + `"]`, `"available":true`, `"per_site":10`} {
		if !strings.Contains(body, want) {
			t.Errorf("в списке сайтов нет %s: %s", want, body)
		}
	}
	// Обычные ответы без доменов содержат пустой список, а не null.
	if b := e.do("POST", "/api/sites/"+itoa(id)+"/ftp", nil, john).Body.String(); strings.Contains(b, `"domains":null`) {
		t.Fatal("domains: null")
	}
}

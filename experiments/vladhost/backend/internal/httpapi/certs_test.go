package httpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vladhost/internal/auth"
	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/sites"
	"vladhost/internal/testdb"
)

type certSite struct {
	Sites []struct {
		Host        string  `json:"host"`
		CertStatus  string  `json:"cert_status"`
		CertError   string  `json:"cert_error"`
		CertRenewAt *string `json:"cert_renew_at"`
		Cert        *struct {
			Issuer   string   `json:"issuer"`
			NotAfter string   `json:"not_after"`
			Names    []string `json:"names"`
		} `json:"cert"`
		Domains []struct {
			Host        string  `json:"host"`
			Status      string  `json:"status"`
			Error       string  `json:"error"`
			CertRenewAt *string `json:"cert_renew_at"`
			Cert        *struct {
				NotAfter string `json:"not_after"`
			} `json:"cert"`
		} `json:"domains"`
	} `json:"sites"`
}

func (e *env) writeCertFile(sub, host, content string) {
	e.t.Helper()
	dir := filepath.Join(e.certs, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, host), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func certInfoFile(notAfter time.Time) string {
	return "not_before=" + notAfter.Add(-90*24*time.Hour).UTC().Format(time.RFC3339) + "\nnot_after=" + notAfter.UTC().Format(time.RFC3339) +
		"\nissuer=Let's Encrypt (R11)\nnames=blog.john.vladinc.ru\n"
}

func (e *env) certState(tok string) certSite {
	e.t.Helper()
	return decode[certSite](e.t, e.do("GET", "/api/sites", nil, tok))
}

func (e *env) waitCert(tok, want string) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := e.certState(tok).Sites[0].CertStatus; got == want {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("статус %q, ожидали %q", e.certState(tok).Sites[0].CertStatus, want)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

// ageRequest откатывает время последней заявки: иначе пауза между ручными перевыпусками не даст сделать второй.
func (e *env) ageSiteRequest(id int64) {
	e.t.Helper()
	if err := e.db.Exec("UPDATE sites SET cert_requested_at = now() - interval '4 days' WHERE id = ?", id).Error; err != nil {
		e.t.Fatal(err)
	}
}

func TestSiteCertInfoAndRenew(t *testing.T) {
	e := newEnv(t)
	go e.sites.WatchCerts(t.Context(), 30*time.Millisecond)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, host := e.createSite(john, "blog")
	renew := func(tok, h string) (int, string) {
		w := e.do("POST", "/api/sites/"+itoa(id)+"/certs/renew", map[string]string{"host": h}, tok)
		return w.Code, w.Body.String()
	}

	// Пока сертификат выпускается, сведений нет и перевыпускать нечего.
	if st := e.certState(john).Sites[0]; st.CertStatus != "pending" || st.Cert != nil {
		t.Fatalf("до выпуска: %+v", st)
	}
	if code, body := renew(john, host); code != 409 || !strings.Contains(body, "cert_renew_state") {
		t.Fatalf("во время выпуска: %d %s", code, body)
	}

	// Выпущен: приходят издатель, срок и имена.
	until := time.Now().Add(60 * 24 * time.Hour).UTC().Truncate(time.Second)
	e.writeCertFile("info", host, certInfoFile(until))
	e.writeCertFile("status", host, "ok")
	e.waitCert(john, "active")
	st := e.certState(john).Sites[0]
	if st.Cert == nil || st.Cert.Issuer != "Let's Encrypt (R11)" || st.Cert.NotAfter != until.Format(time.RFC3339) || len(st.Cert.Names) != 1 {
		t.Fatalf("сведения: %+v", st.Cert)
	}
	if st.CertRenewAt == nil {
		t.Fatal("сразу после выпуска ручной перевыпуск закрыт паузой: cert_renew_at должен быть задан")
	}
	if code, body := renew(john, host); code != 429 || !strings.Contains(body, "cert_renew_cooldown") || !strings.Contains(body, "72") {
		t.Fatalf("пауза: %d %s", code, body)
	}

	// Чужие и посторонние запросы.
	e.ageSiteRequest(id)
	if code, _ := renew(mary, host); code != 404 {
		t.Errorf("чужой сайт: %d", code)
	}
	if code, _ := renew("", host); code != 401 {
		t.Errorf("без токена: %d", code)
	}
	if code, _ := renew(john, "someone-else.example.com"); code != 404 {
		t.Errorf("имя, которого нет у сайта: %d", code)
	}
	if w := e.do("POST", "/api/sites/"+itoa(id)+"/certs/renew", "nope", john); w.Code != 400 {
		t.Errorf("не объект: %d", w.Code)
	}
	if e.certState(john).Sites[0].CertStatus != "active" {
		t.Fatal("отказы не должны менять статус")
	}

	// Перевыпуск: заявка выпускателю, статус pending, снова пауза.
	if code, body := renew(john, strings.ToUpper(host)); code != 202 {
		t.Fatalf("перевыпуск: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(e.certs, "queue", "renew-"+host)); err != nil {
		t.Fatal("заявка на перевыпуск не поставлена")
	}
	if st := e.certState(john).Sites[0]; st.CertStatus != "pending" || st.CertRenewAt == nil {
		t.Fatalf("после заявки: %+v", st)
	}
	if code, _ := renew(john, host); code != 409 {
		t.Fatalf("повторно во время перевыпуска: %d", code)
	}

	// Неудача: прежний сертификат ещё действует, поэтому сайт остаётся рабочим, а причина видна.
	_ = os.Remove(filepath.Join(e.certs, "status", host))
	time.Sleep(50 * time.Millisecond)
	e.writeCertFile("status", host, "error: too many certificates already issued")
	e.waitCert(john, "active")
	if st := e.certState(john).Sites[0]; st.CertError != "too many certificates already issued" || st.Cert == nil {
		t.Fatalf("после неудачного перевыпуска: %+v", st)
	}
	// Успех очищает причину.
	e.ageSiteRequest(id)
	if code, _ := renew(john, host); code != 202 {
		t.Fatal("повторный перевыпуск")
	}
	_ = os.Remove(filepath.Join(e.certs, "status", host))
	time.Sleep(50 * time.Millisecond)
	e.writeCertFile("status", host, "ok")
	e.waitCert(john, "active")
	if st := e.certState(john).Sites[0]; st.CertError != "" {
		t.Fatalf("причина должна очиститься: %q", st.CertError)
	}
}

func TestFailedIssueWithoutCertStaysFailed(t *testing.T) {
	e := newEnv(t)
	go e.sites.WatchCerts(t.Context(), 30*time.Millisecond)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	_, host := e.createSite(john, "blog")
	e.writeCertFile("status", host, "error: DNS problem")
	e.waitCert(john, "failed") // сведений о сертификате нет — работающего HTTPS нет
	if st := e.certState(john).Sites[0]; st.Cert != nil || st.CertError != "DNS problem" {
		t.Fatalf("%+v", st)
	}
	// Просроченный сертификат не считается работающим: повторный отказ оставляет статус failed.
	e.writeCertFile("info", host, certInfoFile(time.Now().Add(-24*time.Hour)))
	sid := decode[struct {
		Sites []struct {
			ID int64 `json:"id"`
		} `json:"sites"`
	}](t, e.do("GET", "/api/sites", nil, john)).Sites[0].ID
	if w := e.do("POST", "/api/sites/"+itoa(sid)+"/cert/retry", nil, john); w.Code != 200 {
		t.Fatalf("повтор: %d %s", w.Code, w.Body)
	}
	_ = os.Remove(filepath.Join(e.certs, "status", host))
	time.Sleep(60 * time.Millisecond)
	e.writeCertFile("status", host, "error: still broken")
	e.waitCert(john, "failed")
}

func TestRenewSubdomainAndCustomDomain(t *testing.T) {
	e := newEnv(t)
	dns, _ := e.withDomains(10, 10)
	go e.sites.WatchDomains(t.Context(), 30*time.Millisecond)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, host := e.createSite(john, "blog")
	dns.set("shop.example.com", serverIP)
	_, custom, _ := e.addDomain(john, id, "shop.example.com")
	_, sub, _ := e.addSub(john, id, "docs", "")
	renew := func(tok, h string) (int, string) {
		w := e.do("POST", "/api/sites/"+itoa(id)+"/certs/renew", map[string]string{"host": h}, tok)
		return w.Code, w.Body.String()
	}
	for _, d := range []string{custom.Domain.Host, sub.Domain.Host} {
		if code, body := renew(john, d); code != 409 || !strings.Contains(body, "cert_renew_state") {
			t.Fatalf("%s пока выпускается: %d %s", d, code, body)
		}
	}
	// Выпущены.
	until := time.Now().Add(45 * 24 * time.Hour).UTC().Truncate(time.Second)
	for _, d := range []string{custom.Domain.Host, sub.Domain.Host} {
		e.writeCertFile("info", d, certInfoFile(until))
		e.writeCertFile("status", d, "ok")
	}
	waitDomains := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			ok := true
			for _, dm := range e.certState(john).Sites[0].Domains {
				ok = ok && dm.Status == want
			}
			if ok {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("домены не стали %s: %+v", want, e.certState(john).Sites[0].Domains)
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	waitDomains("active")
	for _, dm := range e.certState(john).Sites[0].Domains {
		if dm.Cert == nil || dm.CertRenewAt == nil {
			t.Fatalf("у %s есть сведения и пауза: %+v", dm.Host, dm)
		}
	}
	if code, _ := renew(john, sub.Domain.Host); code != 429 {
		t.Fatalf("пауза домена: %d", code)
	}
	if err := e.db.Exec("UPDATE domains SET cert_requested_at = now() - interval '4 days'").Error; err != nil {
		t.Fatal(err)
	}
	if code, _ := renew(mary, custom.Domain.Host); code != 404 {
		t.Fatalf("чужой: %d", code)
	}
	for _, d := range []string{custom.Domain.Host, sub.Domain.Host} {
		if code, body := renew(john, d); code != 202 {
			t.Fatalf("%s: %d %s", d, code, body)
		}
		if _, err := os.Stat(filepath.Join(e.certs, "queue", "renew-"+d)); err != nil {
			t.Fatalf("заявка для %s не поставлена", d)
		}
	}
	// Неудача при действующем сертификате: имя остаётся работающим.
	for _, d := range []string{custom.Domain.Host, sub.Domain.Host} {
		_ = os.Remove(filepath.Join(e.certs, "status", d))
	}
	time.Sleep(60 * time.Millisecond)
	for _, d := range []string{custom.Domain.Host, sub.Domain.Host} {
		e.writeCertFile("status", d, "error: rate limited")
	}
	waitDomains("active")
	for _, dm := range e.certState(john).Sites[0].Domains {
		if dm.Error != "rate limited" {
			t.Fatalf("причина неудачи не видна: %+v", dm)
		}
	}
	// Адрес сайта не путается с доменами.
	if code, _ := renew(john, host); code != 409 && code != 429 {
		t.Fatalf("сайт без сертификата: %d", code)
	}
}

func TestRenewUnavailableWithoutCertIssuer(t *testing.T) {
	db := testdb.Open(t)
	svc := auth.NewService(db, []byte(strings.Repeat("s", 32)), 15*time.Minute, time.Hour)
	root := t.TempDir()
	siteSvc := sites.NewService(db, root, "vladinc.ru", "", sites.Limits{MaxSites: 1, DiskQuotaBytes: 1 << 20}) // каталога сертификатов нет
	cfg := config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000}
	e := &env{t: t, r: httpapi.New(svc, siteSvc, cfg), svc: svc, root: root, certs: t.TempDir(), sites: siteSvc, db: db}
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, host := e.createSite(john, "blog")
	// Каталог сертификатов не настроен (разработка): сервис сообщает об этом, а не молчит.
	w := e.do("POST", "/api/sites/"+itoa(id)+"/certs/renew", map[string]string{"host": host}, john)
	if w.Code != 409 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

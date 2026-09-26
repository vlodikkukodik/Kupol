package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/mailhost"
	"vladhost/internal/runtimes"
)

// mailHelper играет исполнителя почты: запоминает, сколько раз его просили применить состояние.
type mailHelper struct {
	mu    sync.Mutex
	syncs int
}

func (h *mailHelper) Do(_ context.Context, r runtimes.Request, _ time.Duration) (runtimes.Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Action == mailhost.ActionMailSync {
		h.syncs++
	}
	if r.Action == mailhost.ActionMailLog {
		return runtimes.Result{OK: true, Output: `{"events":[{"t":"2026-09-25 14:22:27","kind":"delivered","id":"1xA6ol-000000037ku-33mU","from":"a@else.org","to":"info@` + r.Host + `","host":"","via":"mailbox","detail":"Saved"},` +
			`{"t":"2026-09-25 14:22:28","kind":"bogus","id":"","from":"","to":"","host":"","via":"","detail":"x"}],"queue":null}`}, nil
	}
	return runtimes.Result{OK: true}, nil
}

func (e *env) withMailHost() (*mailHelper, string) {
	e.t.Helper()
	dir := e.t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "mail"), 0o755); err != nil {
		e.t.Fatal(err)
	}
	h := &mailHelper{}
	svc := mailhost.New(e.db, mailhost.Config{Host: "mail.vladinc.ru", ServerIP: serverIP, BaseDomain: "vladinc.ru", Dir: dir, Applier: h, Sites: e.sites, Resolver: noDNS{}})
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000}, httpapi.WithMailHost(svc))
	return h, dir
}

type noDNS struct{}

func (noDNS) LookupMX(context.Context, string) ([]string, error)  { return nil, nil }
func (noDNS) LookupTXT(context.Context, string) ([]string, error) { return nil, nil }

func TestMailHostHiddenWhenNotConfigured(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	for _, tc := range [][2]string{{"GET", "/api/mail"}, {"POST", "/api/mail/domains"}, {"PATCH", "/api/mail/domains/1"}, {"DELETE", "/api/mail/domains/1"},
		{"GET", "/api/mail/domains/1/dns"}, {"GET", "/api/mail/domains/1/log"}, {"POST", "/api/mail/domains/1/mailboxes"}, {"PUT", "/api/mail/domains/1/aliases"}, {"PATCH", "/api/mail/mailboxes/1"},
		{"PUT", "/api/mail/mailboxes/1/password"}, {"PUT", "/api/mail/mailboxes/1/rules"}, {"DELETE", "/api/mail/mailboxes/1"}, {"DELETE", "/api/mail/aliases/1"}} {
		if w := e.do(tc[0], tc[1], map[string]any{}, tok); w.Code != 404 {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	if !strings.Contains(e.do("GET", "/api/me", nil, tok).Body.String(), `"mailhost_enabled":false`) {
		t.Fatal("флаг выключенной почты должен быть в /me")
	}
}

func TestMailHostThroughAPI(t *testing.T) {
	e := newEnv(t)
	dns, _ := e.withDomains(10, 10)
	h, dir := e.withMailHost()
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	if w := e.do("GET", "/api/mail", nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
	if !strings.Contains(e.do("GET", "/api/me", nil, john).Body.String(), `"mailhost_enabled":true`) {
		t.Fatal("флаг почты должен быть в /me")
	}
	site, _ := e.createSite(john, "blog")
	dns.set("mine.example.com", serverIP)
	if code, _, body := e.addDomain(john, site, "mine.example.com"); code != 201 {
		t.Fatalf("домен сайта: %d %s", code, body)
	}

	type overview struct {
		Info struct {
			Host     string   `json:"host"`
			Eligible []string `json:"eligible_domains"`
		} `json:"info"`
		Domains []struct {
			ID        int64  `json:"id"`
			Domain    string `json:"domain"`
			Enabled   bool   `json:"enabled"`
			Mailboxes []struct {
				ID    int64  `json:"id"`
				Local string `json:"local"`
			} `json:"mailboxes"`
			Aliases []struct {
				ID    int64    `json:"id"`
				Local string   `json:"local"`
				To    []string `json:"to"`
			} `json:"aliases"`
		} `json:"domains"`
	}
	ov := decode[overview](t, e.do("GET", "/api/mail", nil, john))
	if ov.Info.Host != "mail.vladinc.ru" || len(ov.Domains) != 0 || len(ov.Info.Eligible) != 1 || ov.Info.Eligible[0] != "mine.example.com" {
		t.Fatalf("%+v", ov)
	}

	// чужой домен и чужая ошибка формы
	if w := e.do("POST", "/api/mail/domains", map[string]string{"domain": "not-mine.org"}, john); w.Code != 422 || decode[errBody](t, w).Error.Code != "validation.mail_domain_not_attached" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w := e.do("POST", "/api/mail/domains", map[string]string{"domain": "Mine.Example.com"}, john)
	if w.Code != 201 || strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "dkim_private") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	dom := decode[struct {
		Domain struct {
			ID int64 `json:"id"`
		} `json:"domain"`
	}](t, w).Domain.ID
	base := fmt.Sprintf("/api/mail/domains/%d", dom)

	if w := e.do("POST", "/api/mail/domains", map[string]string{"domain": "mine.example.com"}, john); w.Code != 409 {
		t.Fatalf("повторное подключение: %d %s", w.Code, w.Body)
	}
	// другой пользователь ничего не видит и не может
	for _, tc := range [][2]string{{"GET", base + "/dns"}, {"DELETE", base}, {"POST", base + "/mailboxes"}, {"PUT", base + "/aliases"}} {
		if w := e.do(tc[0], tc[1], map[string]any{"local": "x", "to": []string{"a@b.com"}}, mary); w.Code != 404 {
			t.Errorf("чужой %s %s: %d", tc[0], tc[1], w.Code)
		}
	}

	dnsResp := decode[struct {
		Records []struct{ Kind, State string } `json:"records"`
	}](t, e.do("GET", base+"/dns", nil, john))
	kinds := map[string]string{}
	for _, r := range dnsResp.Records {
		kinds[r.Kind] = r.State
	}
	if len(kinds) != 4 || kinds["mx"] != "missing" || kinds["dkim"] != "missing" {
		t.Fatalf("%+v", kinds)
	}

	// журнал: события домена приходят от исполнителя, неизвестные виды отбрасываются
	jl := decode[struct {
		Events []struct{ Kind, To, Via string } `json:"events"`
		Queue  []any                            `json:"queue"`
	}](t, e.do("GET", base+"/log", nil, john))
	if len(jl.Events) != 1 || jl.Events[0].Kind != "delivered" || jl.Events[0].To != "info@mine.example.com" || jl.Events[0].Via != "mailbox" || jl.Queue == nil {
		t.Fatalf("журнал: %+v", jl)
	}

	// ящик со сгенерированным паролем: пароль показывается один раз
	w = e.do("POST", base+"/mailboxes", map[string]any{"local": "Info", "quota_mb": 100}, john)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	box := decode[struct {
		Mailbox  struct{ ID int64 } `json:"mailbox"`
		Password string             `json:"password"`
	}](t, w)
	if len(box.Password) < mailhost.MinPassword || strings.Contains(w.Body.String(), "SHA512") {
		t.Fatalf("пароль %q, тело %s", box.Password, w.Body)
	}
	if w := e.do("POST", base+"/mailboxes", map[string]any{"local": "info"}, john); w.Code != 409 {
		t.Fatalf("занятое имя: %d", w.Code)
	}
	if w := e.do("POST", base+"/mailboxes", map[string]any{"local": "short", "password": "123"}, john); w.Code != 422 || decode[errBody](t, w).Error.Code != "validation.mail_password" {
		t.Fatalf("короткий пароль: %d %s", w.Code, w.Body)
	}
	w = e.do("PUT", fmt.Sprintf("/api/mail/mailboxes/%d/password", box.Mailbox.ID), map[string]string{"password": "my-very-own-pass"}, john)
	if w.Code != 200 || decode[struct{ Password string }](t, w).Password != "" {
		t.Fatalf("свой пароль не возвращается: %d %s", w.Code, w.Body)
	}
	if w := e.do("PATCH", fmt.Sprintf("/api/mail/mailboxes/%d", box.Mailbox.ID), map[string]any{"quota_mb": 5}, john); w.Code != 422 {
		t.Fatalf("квота: %d", w.Code)
	}
	if w := e.do("PATCH", fmt.Sprintf("/api/mail/mailboxes/%d", box.Mailbox.ID), map[string]any{"quota_mb": 300, "enabled": false}, john); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	// автоответчик и пересылка
	rules := fmt.Sprintf("/api/mail/mailboxes/%d/rules", box.Mailbox.ID)
	w = e.do("PUT", rules, map[string]any{"autoreply": map[string]any{"enabled": true, "subject": "Я в отпуске", "body": "Отвечу позже", "from": "2026-10-01", "to": "2026-10-15", "days": 2},
		"forward": map[string]any{"to": []string{"boss@gmail.com"}, "keep_copy": true}}, john)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"keep_copy":true`) || !strings.Contains(w.Body.String(), `"subject":"Я в отпуске"`) {
		t.Fatalf("правила: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", rules, map[string]any{"autoreply": map[string]any{"enabled": true, "subject": "", "body": "b"}, "forward": map[string]any{"to": []string{}, "keep_copy": true}}, john); w.Code != 422 ||
		decode[errBody](t, w).Error.Code != "validation.mail_autoreply_text" {
		t.Fatalf("пустая тема: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", rules, map[string]any{"autoreply": map[string]any{}, "forward": map[string]any{"to": []string{"x y"}, "keep_copy": true}}, john); w.Code != 422 {
		t.Fatalf("плохой адрес пересылки: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", rules, map[string]any{}, mary); w.Code != 404 {
		t.Fatalf("чужой ящик: %d", w.Code)
	}

	// алиас
	if w := e.do("PUT", base+"/aliases", map[string]any{"local": "sales", "to": []string{"boss@gmail.com", "info@mine.example.com"}}, john); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", base+"/aliases", map[string]any{"local": "sales", "to": []string{"a b"}}, john); w.Code != 422 {
		t.Fatalf("плохой получатель: %d", w.Code)
	}
	ov = decode[overview](t, e.do("GET", "/api/mail", nil, john))
	if len(ov.Domains) != 1 || len(ov.Domains[0].Mailboxes) != 1 || len(ov.Domains[0].Aliases) != 2 { // postmaster + sales
		t.Fatalf("%+v", ov)
	}
	var alias struct {
		ID int64
	}
	for _, a := range ov.Domains[0].Aliases {
		if a.Local == "sales" {
			alias.ID = a.ID
		}
	}

	// на исполнитель ушло состояние, а хеш пароля наружу не выходит
	raw, err := os.ReadFile(filepath.Join(dir, "mail", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if json.Unmarshal(raw, &st) != nil || !strings.Contains(string(raw), "mine.example.com") || !strings.Contains(string(raw), "SHA512-CRYPT") {
		t.Fatalf("состояние: %s", raw)
	}
	if h.syncs == 0 {
		t.Fatal("исполнитель не вызывался")
	}

	if w := e.do("DELETE", fmt.Sprintf("/api/mail/aliases/%d", alias.ID), nil, john); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/mail/mailboxes/%d", box.Mailbox.ID), nil, john); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if w := e.do("PATCH", base, map[string]any{"enabled": false}, john); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", base, nil, john); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if ov = decode[overview](t, e.do("GET", "/api/mail", nil, john)); len(ov.Domains) != 0 {
		t.Fatalf("домен должен исчезнуть: %+v", ov)
	}
}

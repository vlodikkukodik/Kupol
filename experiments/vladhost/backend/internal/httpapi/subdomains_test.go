package httpapi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vladhost/internal/sites"
)

type subResp struct {
	Domain struct {
		ID     int64  `json:"id"`
		Host   string `json:"host"`
		Kind   string `json:"kind"`
		Dir    string `json:"dir"`
		Status string `json:"status"`
	} `json:"domain"`
}

func (e *env) addSub(tok string, siteID int64, label, dir string) (int, subResp, string) {
	e.t.Helper()
	w := e.do("POST", "/api/sites/"+itoa(siteID)+"/subdomains", map[string]string{"label": label, "dir": dir}, tok)
	if w.Code != 201 {
		return w.Code, subResp{}, w.Body.String()
	}
	return w.Code, decode[subResp](e.t, w), w.Body.String()
}

func readMapping(t *testing.T, dir, host string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, host))
	if err != nil {
		t.Fatalf("привязка %s: %v", host, err)
	}
	return string(b)
}

func TestSubdomainsUnavailableWithoutConfig(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, _ := e.createSite(tok, "blog")
	if status, _, body := e.addSub(tok, id, "docs", ""); status != 409 || !strings.Contains(body, "subdomains_unavailable") {
		t.Fatalf("%d %s", status, body)
	}
}

func TestSubdomainLifecycle(t *testing.T) {
	e := newEnv(t)
	dns, mapping := e.withDomains(10, 10)
	go e.sites.WatchDomains(t.Context(), 30*time.Millisecond)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, host := e.createSite(tok, "blog")

	status, sub, body := e.addSub(tok, id, "Docs", "/docs/manual/")
	if status != 201 {
		t.Fatalf("создание: %d %s", status, body)
	}
	d := sub.Domain
	if d.Host != "docs."+host || d.Kind != "sub" || d.Dir != "docs/manual" {
		t.Fatalf("поддомен: %+v", d)
	}
	// DNS не проверяется (записей в подменённом DNS нет), сертификат заказан сразу.
	if d.Status != "pending_cert" || len(dns.relative) != 0 {
		t.Fatalf("статус %q: поддомен на нашем домене не ждёт A-запись", d.Status)
	}
	if _, err := os.Stat(filepath.Join(e.certs, "queue", "issue-"+d.Host)); err != nil {
		t.Fatal("заявка на сертификат не поставлена")
	}
	if got := readMapping(t, mapping, d.Host); got != host+"\ndocs/manual\n" {
		t.Fatalf("привязка: %q", got)
	}
	if fi, err := os.Stat(filepath.Join(e.root, host, "public", "docs", "manual")); err != nil || !fi.IsDir() {
		t.Fatalf("папка не создана: %v", err)
	}

	// Сертификат выпущен → active.
	if err := os.MkdirAll(filepath.Join(e.certs, "status"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.certs, "status", d.Host), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		list := decode[struct {
			Sites []struct {
				Domains []struct{ Host, Kind, Status, Dir string } `json:"domains"`
			} `json:"sites"`
		}](t, e.do("GET", "/api/sites", nil, tok))
		if dm := list.Sites[0].Domains; len(dm) == 1 && dm[0].Status == "active" && dm[0].Kind == "sub" && dm[0].Dir == "docs/manual" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("поддомен не стал active: %+v", list)
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Удаление: привязка убрана, сертификату поставлена заявка на удаление.
	if w := e.do("DELETE", "/api/sites/"+itoa(id)+"/domains/"+itoa(d.ID), nil, tok); w.Code != 204 {
		t.Fatalf("удаление: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(mapping, d.Host)); !os.IsNotExist(err) {
		t.Fatal("привязка должна быть удалена")
	}
	if _, err := os.Stat(filepath.Join(e.certs, "queue", "delete-"+d.Host)); err != nil {
		t.Fatal("заявка на удаление сертификата")
	}
	// Папка с файлами остаётся: удаляется имя, а не содержимое сайта.
	if _, err := os.Stat(filepath.Join(e.root, host, "public", "docs", "manual")); err != nil {
		t.Fatal("папка сайта не должна удаляться вместе с поддоменом")
	}
}

func TestSubdomainValidation(t *testing.T) {
	e := newEnv(t)
	e.withDomains(10, 10)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	id, _ := e.createSite(tok, "blog")
	for _, label := range []string{"", " ", "a.b", "-a", "a-", "a--b", "a b", "a_b", "имя", "a/b", "..", "*", strings.Repeat("a", 33), "xn--e1afmkfd"} {
		status, _, body := e.addSub(tok, id, label, "")
		if status != 422 || !strings.Contains(body, "validation.sub_label") || !strings.Contains(body, `"field":"label"`) {
			t.Errorf("метка %q: %d %s", label, status, body)
		}
	}
	for _, dir := range []string{"..", "../x", ".git", "a/.ssh", `a\b`, strings.Repeat("a/", 9) + "z"} {
		status, _, body := e.addSub(tok, id, "ok", dir)
		if status != 422 || !strings.Contains(body, "validation.domain_dir") {
			t.Errorf("папка %q: %d %s", dir, status, body)
		}
	}
	// Недопустимая папка не оставляет за собой поддомен.
	if list := e.do("GET", "/api/sites", nil, tok).Body.String(); strings.Contains(list, "ok.blog.john") {
		t.Fatalf("после отказа остался поддомен: %s", list)
	}
	for _, label := range []string{"a", "1", "www", "my-docs", strings.Repeat("a", 32)} {
		if status, _, body := e.addSub(tok, id, label, ""); status != 201 && !strings.Contains(body, "sub_limit_site") {
			t.Errorf("метка %q должна подходить: %d %s", label, status, body)
		}
	}
}

func TestSubdomainUniquenessOwnershipAndLimits(t *testing.T) {
	e := newEnv(t)
	e.withDomains(10, 10)
	e.sites.ConfigureDomains(sites.DomainConfig{
		ServerIPs: []string{serverIP}, MappingDir: filepath.Join(t.TempDir(), "m"), Resolver: &fakeDNS{},
		PerSite: 10, PerUser: 10, PerSiteSub: 2, PerUserSub: 3,
	})
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, _ := e.createSite(john, "blog")
	mid, _ := e.createSite(mary, "blog")

	if status, _, body := e.addSub(john, id, "docs", ""); status != 201 {
		t.Fatalf("%d %s", status, body)
	}
	if status, _, body := e.addSub(john, id, "DOCS", ""); status != 409 || !strings.Contains(body, "sub_taken") {
		t.Fatalf("повтор: %d %s", status, body)
	}
	// Такая же метка на сайте другого пользователя — другое имя.
	if status, _, body := e.addSub(mary, mid, "docs", ""); status != 201 {
		t.Fatalf("тот же label у другого сайта: %d %s", status, body)
	}
	// Чужой сайт недоступен.
	if status, _, _ := e.addSub(mary, id, "hack", ""); status != 404 {
		t.Fatalf("чужой сайт: %d", status)
	}
	if status, _, _ := e.addSub("", id, "hack", ""); status != 401 {
		t.Fatalf("без токена: %d", status)
	}

	// Лимит на сайт (2).
	if status, _, body := e.addSub(john, id, "api", ""); status != 201 {
		t.Fatalf("%d %s", status, body)
	}
	if status, _, body := e.addSub(john, id, "more", ""); status != 403 || !strings.Contains(body, "sub_limit_site") {
		t.Fatalf("лимит сайта: %d %s", status, body)
	}
	// Поддомены не съедают лимит своих доменов и наоборот.
	if status, _, body := e.addDomain(john, id, "example.org"); status != 201 {
		t.Fatalf("свой домен при заполненных поддоменах: %d %s", status, body)
	}
}

func TestSubdomainPerUserLimit(t *testing.T) {
	e := newEnv(t)
	e.sites.ConfigureDomains(sites.DomainConfig{
		ServerIPs: []string{serverIP}, MappingDir: filepath.Join(t.TempDir(), "m"), Resolver: &fakeDNS{},
		PerSite: 10, PerUser: 10, PerSiteSub: 5, PerUserSub: 2,
	})
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	for _, l := range []string{"a", "b"} {
		if status, _, body := e.addSub(john, id, l, ""); status != 201 {
			t.Fatalf("%s: %d %s", l, status, body)
		}
	}
	if status, _, body := e.addSub(john, id, "c", ""); status != 403 || !strings.Contains(body, "sub_limit_user") {
		t.Fatalf("лимит аккаунта: %d %s", status, body)
	}
}

func TestChangingDomainFolder(t *testing.T) {
	e := newEnv(t)
	dns, mapping := e.withDomains(10, 10)
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	id, host := e.createSite(john, "blog")
	dns.set("shop.example.com", serverIP)

	// Свой домен сразу с папкой.
	w := e.do("POST", "/api/sites/"+itoa(id)+"/domains", map[string]string{"host": "shop.example.com", "dir": "shop"}, john)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	d := decode[subResp](t, w).Domain
	if d.Kind != "custom" || d.Dir != "shop" || readMapping(t, mapping, "shop.example.com") != host+"\nshop\n" {
		t.Fatalf("свой домен с папкой: %+v %q", d, readMapping(t, mapping, "shop.example.com"))
	}
	url := "/api/sites/" + itoa(id) + "/domains/" + itoa(d.ID)

	// Смена папки переписывает привязку.
	w = e.do("PATCH", url, map[string]string{"dir": "/store/v2/"}, john)
	if w.Code != 200 || decode[subResp](t, w).Domain.Dir != "store/v2" || readMapping(t, mapping, "shop.example.com") != host+"\nstore/v2\n" {
		t.Fatalf("смена: %d %s %q", w.Code, w.Body, readMapping(t, mapping, "shop.example.com"))
	}
	if _, err := os.Stat(filepath.Join(e.root, host, "public", "store", "v2")); err != nil {
		t.Fatal("новая папка не создана")
	}
	// Пустая папка — снова весь сайт, в файле привязки одна строка.
	w = e.do("PATCH", url, map[string]string{"dir": ""}, john)
	if w.Code != 200 || readMapping(t, mapping, "shop.example.com") != host+"\n" {
		t.Fatalf("сброс папки: %d %q", w.Code, readMapping(t, mapping, "shop.example.com"))
	}
	// Ошибки.
	if w := e.do("PATCH", url, map[string]string{"dir": "../x"}, john); w.Code != 422 || !strings.Contains(w.Body.String(), "validation.domain_dir") {
		t.Errorf("недопустимая папка: %d %s", w.Code, w.Body)
	}
	if got := readMapping(t, mapping, "shop.example.com"); got != host+"\n" {
		t.Errorf("после отказа привязка изменилась: %q", got)
	}
	if w := e.do("PATCH", url, map[string]any{}, john); w.Code != 400 {
		t.Errorf("без поля dir: %d", w.Code)
	}
	if w := e.do("PATCH", url, map[string]string{"dir": "x"}, mary); w.Code != 404 {
		t.Errorf("чужой: %d", w.Code)
	}
	if w := e.do("PATCH", url, map[string]string{"dir": "x"}, ""); w.Code != 401 {
		t.Errorf("без токена: %d", w.Code)
	}
	if w := e.do("PATCH", "/api/sites/"+itoa(id)+"/domains/99999", map[string]string{"dir": "x"}, john); w.Code != 404 {
		t.Errorf("несуществующий: %d", w.Code)
	}
}

func TestDomainFolderThroughSymlinkIsRejected(t *testing.T) {
	e := newEnv(t)
	e.withDomains(10, 10)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, host := e.createSite(john, "blog")
	outside := t.TempDir()
	pub := filepath.Join(e.root, host, "public")
	if err := os.MkdirAll(pub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(pub, "escape")); err != nil {
		t.Skip("симлинки недоступны:", err)
	}
	for _, dir := range []string{"escape", "escape/inner"} {
		if status, _, body := e.addSub(john, id, "x"+strings.ReplaceAll(dir, "/", ""), dir); status != 422 {
			t.Errorf("папка %q через ссылку: %d %s", dir, status, body)
		}
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("за пределами сайта созданы файлы: %v", ents)
	}
}

func TestDeletingSiteReleasesSubdomains(t *testing.T) {
	e := newEnv(t)
	_, mapping := e.withDomains(10, 10)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	id, _ := e.createSite(john, "blog")
	_, sub, _ := e.addSub(john, id, "docs", "")
	if w := e.do("DELETE", "/api/sites/"+itoa(id), nil, john); w.Code != 204 {
		t.Fatalf("удаление сайта: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(mapping, sub.Domain.Host)); !os.IsNotExist(err) {
		t.Fatal("привязка поддомена осталась")
	}
	if _, err := os.Stat(filepath.Join(e.certs, "queue", "delete-"+sub.Domain.Host)); err != nil {
		t.Fatal("сертификат поддомена должен быть отозван")
	}
}

func TestDomainConfigReportsSubdomainLimit(t *testing.T) {
	e := newEnv(t)
	e.withDomains(10, 10)
	adm, _ := e.admin()
	john := e.user(adm, "john")
	e.createSite(john, "blog")
	if body := e.do("GET", "/api/sites", nil, john).Body.String(); !strings.Contains(body, `"per_site_sub":5`) {
		t.Fatalf("domain_config: %s", body)
	}
}

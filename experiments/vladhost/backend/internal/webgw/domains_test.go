package webgw_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vladhost/internal/webgw"
)

// withDomains включает обслуживание своих доменов и возвращает папку привязок.
func (f *fixture) withDomains() string {
	f.t.Helper()
	dir := filepath.Join(f.base, "_domains")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		f.t.Fatal(err)
	}
	f.h = webgw.New(webgw.Options{Root: f.base, BaseDomain: "vladinc.ru", DomainsDir: dir})
	return dir
}

func bind(t *testing.T, dir, domain, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, domain), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCustomDomainServesSite(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "<h1>my site</h1>", "about.html": "about"})
	dir := f.withDomains()
	bind(t, dir, "example.com", host+"\n")
	bind(t, dir, "www.example.com", host)

	for _, h := range []string{"example.com", "www.example.com", "EXAMPLE.com", "example.com.", "example.com:443"} {
		r := f.doHost(h, "GET", "/")
		if r.Code != 200 || r.body != "<h1>my site</h1>" {
			t.Errorf("%s → %d %.60q", h, r.Code, r.body)
		}
	}
	if r := f.doHost("example.com", "GET", "/about.html"); r.body != "about" {
		t.Fatalf("вложенный путь: %q", r.body)
	}
	// Сайт по адресу на нашем домене продолжает работать.
	if r := f.get("/"); r.Code != 200 {
		t.Fatalf("основной адрес: %d", r.Code)
	}
	// Редиректы ведут на свой домен, а не на адрес сайта.
	f.write("docs/index.html", "d")
	rd := f.doHost("example.com", "GET", "/docs")
	if rd.Code != 301 || rd.header("Location") != "https://example.com/docs/" {
		t.Fatalf("редирект каталога: %d %q", rd.Code, rd.header("Location"))
	}
	// .htaccess видит настоящий домен посетителя.
	f.write(".htaccess", "RewriteEngine On\nRewriteCond %{HTTP_HOST} ^www\\.(.+)$ [NC]\nRewriteRule ^ https://%1%{REQUEST_URI} [R=301,L]\n")
	rw := f.doHost("www.example.com", "GET", "/about.html")
	if rw.Code != 301 || rw.header("Location") != "https://example.com/about.html" {
		t.Fatalf("www → без www: %d %q", rw.Code, rw.header("Location"))
	}
	if r := f.doHost("example.com", "GET", "/about.html"); r.Code != 200 {
		t.Fatalf("без www: %d", r.Code)
	}
}

func TestCustomDomainIsNotServedWithoutBinding(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "secret site"})
	dir := f.withDomains()
	for _, h := range []string{"example.com", "other.example.org", "localhost", "127.0.0.1", "example", "a..com"} {
		r := f.doHost(h, "GET", "/")
		if r.Code != 404 || strings.Contains(r.body, "secret site") || !strings.Contains(r.body, "Сайт не найден") {
			t.Errorf("%s без привязки: %d", h, r.Code)
		}
	}
	// Удалённая привязка: домен перестаёт обслуживаться сразу.
	bind(t, dir, "example.com", host)
	if r := f.doHost("example.com", "GET", "/"); r.Code != 200 {
		t.Fatalf("с привязкой: %d", r.Code)
	}
	if err := os.Remove(filepath.Join(dir, "example.com")); err != nil {
		t.Fatal(err)
	}
	if r := f.doHost("example.com", "GET", "/"); r.Code != 404 {
		t.Fatalf("после удаления привязки: %d", r.Code)
	}
}

func TestCustomDomainBindingIsNotTrusted(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "site"})
	dir := f.withDomains()
	if err := os.WriteFile(filepath.Join(f.base, "secret.txt"), []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Содержимое привязки попадает в путь к файлам, поэтому принимается только настоящий адрес сайта.
	for i, content := range []string{
		"../..", "..", "/etc", "blog.john.vladinc.ru/../../..", "blog.john.vladinc.ru\n../x", "", "  ", "app.vladinc.ru",
		"nobody.nobody.vladinc.ru", "blog.john.vladinc.ru.evil.com", strings.Repeat("a", 500), "blog.john.vladinc.ru\x00",
	} {
		domain := "d" + string(rune('a'+i)) + ".example.com"
		bind(t, dir, domain, content)
		r := f.doHost(domain, "GET", "/")
		if r.Code == 200 || strings.Contains(r.body, "TOP-SECRET") || strings.Contains(r.body, "site") && content != "blog.john.vladinc.ru\x00" {
			t.Errorf("привязка %q дала %d %.40q", content, r.Code, r.body)
		}
	}
	// Привязка-симлинк наружу и привязка-каталог не читаются.
	if err := os.Symlink(filepath.Join(f.base, "secret.txt"), filepath.Join(dir, "link.example.com")); err != nil {
		t.Fatal(err)
	}
	if r := f.doHost("link.example.com", "GET", "/"); r.Code != 404 || strings.Contains(r.body, "TOP-SECRET") {
		t.Fatalf("симлинк-привязка: %d", r.Code)
	}
	if err := os.Mkdir(filepath.Join(dir, "dir.example.com"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := f.doHost("dir.example.com", "GET", "/"); r.Code != 404 {
		t.Fatalf("каталог вместо привязки: %d", r.Code)
	}
}

func TestBindingsForOurOwnDomainAreIgnored(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "site"})
	dir := f.withDomains()
	// Даже если файл привязки для имени под нашим доменом каким-то образом появился, он не используется:
	// такие имена обслуживаются только по правилу {сайт}.{пользователь}.домен.
	for _, h := range []string{"app.vladinc.ru", "vladinc.ru", "www.vladinc.ru", "evil.vladinc.ru", "a.b.c.vladinc.ru"} {
		bind(t, dir, h, host)
		if r := f.doHost(h, "GET", "/"); r.Code != 404 {
			t.Errorf("%s: %d", h, r.Code)
		}
	}
}

func TestCustomDomainsDisabledByDefault(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "site"}) // DomainsDir не задан
	if err := os.MkdirAll(filepath.Join(f.base, "_domains"), 0o755); err != nil {
		t.Fatal(err)
	}
	bind(t, filepath.Join(f.base, "_domains"), "example.com", host)
	if r := f.doHost("example.com", "GET", "/"); r.Code != 404 {
		t.Fatalf("без DomainsDir свои домены не обслуживаются: %d", r.Code)
	}
}

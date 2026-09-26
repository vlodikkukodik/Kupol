package webgw_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const subHost = "docs.blog.john.vladinc.ru"

func TestDomainBoundToFolderServesItAsRoot(t *testing.T) {
	f := newSite(t, map[string]string{
		"index.html":          "<h1>main</h1>",
		"shop/index.html":     "<h1>shop</h1>",
		"shop/item.html":      "item",
		"shop/deep/page.html": "deep",
		"secret.txt":          "top-level secret",
	})
	dir := f.withDomains()
	bind(t, dir, "shop.example.com", host+"\nshop\n")
	bind(t, dir, "example.com", host+"\n")

	if r := f.doHost("shop.example.com", "GET", "/"); r.Code != 200 || r.body != "<h1>shop</h1>" {
		t.Fatalf("корень папки: %d %q", r.Code, r.body)
	}
	if r := f.doHost("shop.example.com", "GET", "/item.html"); r.body != "item" {
		t.Fatalf("файл папки: %q", r.body)
	}
	if r := f.doHost("shop.example.com", "GET", "/deep/page.html"); r.body != "deep" {
		t.Fatalf("вложенный: %q", r.body)
	}
	// Файлы вне папки недоступны, выйти наверх нельзя ни «..», ни закодированным «..».
	for _, p := range []string{"/secret.txt", "/../secret.txt", "/%2e%2e/secret.txt", "/deep/../../secret.txt", "/shop/item.html"} {
		if r := f.doHost("shop.example.com", "GET", p); r.Code == 200 && strings.Contains(r.body, "secret") || (p == "/shop/item.html" && r.Code == 200) {
			t.Errorf("%s вышел за папку: %d %q", p, r.Code, r.body)
		}
	}
	// Тот же сайт по другому имени отдаёт всё целиком.
	if r := f.doHost("example.com", "GET", "/"); r.body != "<h1>main</h1>" {
		t.Fatalf("имя без папки: %q", r.body)
	}
	if r := f.doHost("example.com", "GET", "/shop/item.html"); r.body != "item" {
		t.Fatalf("имя без папки видит папку как раньше: %q", r.body)
	}
	if r := f.get("/secret.txt"); r.body != "top-level secret" {
		t.Fatalf("основной адрес: %q", r.body)
	}
}

func TestFolderRedirectsAndErrorPagesStayOnTheirHost(t *testing.T) {
	f := newSite(t, map[string]string{"app/docs/index.html": "docs", "app/.htaccess": "ErrorDocument 404 /oops.html\n", "app/oops.html": "custom 404"})
	dir := f.withDomains()
	bind(t, dir, "app.example.com", host+"\napp\n")
	r := f.doHost("app.example.com", "GET", "/docs")
	if r.Code != 301 || r.header("Location") != "/docs/" && r.header("Location") != "https://app.example.com/docs/" {
		t.Fatalf("редирект каталога: %d %q", r.Code, r.header("Location"))
	}
	if loc := r.header("Location"); strings.Contains(loc, "/app/") || strings.Contains(loc, "vladinc.ru") {
		t.Fatalf("редирект выдал внутренний путь или адрес сайта: %q", loc)
	}
	if r := f.doHost("app.example.com", "GET", "/missing"); r.Code != 404 || r.body != "custom 404" {
		t.Fatalf("ErrorDocument из .htaccess папки: %d %q", r.Code, r.body)
	}
}

func TestHtaccessOfEachNameIsIndependent(t *testing.T) {
	f := newSite(t, map[string]string{
		"a/.htaccess": "Redirect 301 /x /to-a\n",
		"b/.htaccess": "Redirect 301 /x /to-b\n",
		".htaccess":   "Redirect 301 /x /to-root\n",
	})
	dir := f.withDomains()
	bind(t, dir, "a.example.com", host+"\na\n")
	bind(t, dir, "b.example.com", host+"\nb\n")
	bind(t, dir, "root.example.com", host+"\n")
	// Кэш .htaccess не должен смешивать настройки разных имён одного сайта.
	for i := 0; i < 2; i++ {
		for h, want := range map[string]string{"a.example.com": "/to-a", "b.example.com": "/to-b", "root.example.com": "/to-root"} {
			r := f.doHost(h, "GET", "/x")
			if loc := r.header("Location"); r.Code != 301 || !strings.HasSuffix(loc, want) {
				t.Fatalf("%s: %d %q, ожидали ...%s", h, r.Code, loc, want)
			}
		}
	}
}

func TestFolderListingIsRootedInFolder(t *testing.T) {
	f := newSite(t, map[string]string{"pub/.htaccess": "Options +Indexes\n", "pub/one.txt": "1", "hidden-sibling.txt": "no"})
	dir := f.withDomains()
	bind(t, dir, "files.example.com", host+"\npub\n")
	r := f.doHost("files.example.com", "GET", "/")
	if r.Code != 200 || !strings.Contains(r.body, "one.txt") || strings.Contains(r.body, "hidden-sibling") || strings.Contains(r.body, ">pub<") {
		t.Fatalf("листинг: %d %s", r.Code, r.body)
	}
}

func TestSubdomainMapsOnlyToItsOwnSite(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "<h1>main</h1>", "docs/index.html": "<h1>docs</h1>"})
	dir := f.withDomains()

	// Без привязки поддомен не существует.
	if r := f.doHost(subHost, "GET", "/"); r.Code != 404 {
		t.Fatalf("поддомен без привязки: %d", r.Code)
	}
	bind(t, dir, subHost, host+"\ndocs\n")
	if r := f.doHost(subHost, "GET", "/"); r.Code != 200 || r.body != "<h1>docs</h1>" {
		t.Fatalf("поддомен с папкой: %d %q", r.Code, r.body)
	}
	// Без папки — весь сайт.
	bind(t, dir, "www."+host, host+"\n")
	if r := f.doHost("www."+host, "GET", "/"); r.body != "<h1>main</h1>" {
		t.Fatalf("поддомен без папки: %q", r.body)
	}
	// Привязка поддомена к чужому сайту игнорируется, даже если файл подменили.
	other := filepath.Join(f.base, "victim.mary.vladinc.ru", "public")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "index.html"), []byte("victim"), 0o644); err != nil {
		t.Fatal(err)
	}
	bind(t, dir, "steal."+host, "victim.mary.vladinc.ru\n")
	if r := f.doHost("steal."+host, "GET", "/"); r.Code == 200 {
		t.Fatalf("поддомен показал чужой сайт: %q", r.body)
	}
	// Свои домены могут указывать на любой сайт своего владельца (проверка владения — в панели).
	bind(t, dir, "mine.example.com", host+"\n")
	if r := f.doHost("mine.example.com", "GET", "/"); r.body != "<h1>main</h1>" {
		t.Fatalf("свой домен: %q", r.body)
	}
	// Имена под нашим доменом без привязки и разной глубины не обслуживаются.
	for _, h := range []string{"x.y.z.blog.john.vladinc.ru", "vladinc.ru", "john.vladinc.ru", "a.b.vladinc.ru.evil.com"} {
		if r := f.doHost(h, "GET", "/"); r.Code == 200 {
			t.Errorf("%s обслужен: %q", h, r.body)
		}
	}
}

func TestHostileMappingsAreRejected(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "main", "ok/index.html": "ok"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "index.html"), []byte("OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(f.pub, "link")); err != nil {
		t.Skip("симлинки недоступны:", err)
	}
	dir := f.withDomains()
	for i, d := range []string{"..", "../..", "ok/../..", "/etc", "/", "ok/../ok", "./ok", "ok/", ".git", "ok/.hidden", `ok\..`, "ok\x00", "link", "link/sub", strings.Repeat("a/", 120), "missing"} {
		name := "h" + string(rune('a'+i)) + ".example.com"
		bind(t, dir, name, host+"\n"+d+"\n")
		if r := f.doHost(name, "GET", "/"); r.Code == 200 {
			t.Errorf("папка %q принята: %q", d, r.body)
		}
	}
	// Мусор вместо адреса сайта.
	for i, site := range []string{"", "../x", "a/b.john.vladinc.ru", "blog.john.vladinc.ru.evil", "BLOG.john.vladinc.ru "} {
		name := "g" + string(rune('a'+i)) + ".example.com"
		bind(t, dir, name, site+"\n")
		if r := f.doHost(name, "GET", "/"); r.Code == 200 && r.body != "main" {
			t.Errorf("привязка %q: %d %q", site, r.Code, r.body)
		}
	}
}

func TestOldSingleLineMappingStillWorks(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "main"})
	dir := f.withDomains()
	bind(t, dir, "old.example.com", host) // без перевода строки, как писали раньше
	if r := f.doHost("old.example.com", "GET", "/"); r.body != "main" {
		t.Fatalf("%d %q", r.Code, r.body)
	}
}

func TestFolderNamesWithSpacesAndUnicode(t *testing.T) {
	f := newSite(t, map[string]string{"мой сайт/index.html": "unicode"})
	dir := f.withDomains()
	bind(t, dir, "u.example.com", host+"\nмой сайт\n")
	if r := f.doHost("u.example.com", "GET", "/"); r.body != "unicode" {
		t.Fatalf("%d %q", r.Code, r.body)
	}
}

package webgw_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vladhost/internal/sitecfg"
)

// setSettings записывает settings.json сайта фикстуры.
func (f *fixture) setSettings(s sitecfg.Settings) {
	f.t.Helper()
	data, err := json.Marshal(s)
	if err != nil {
		f.t.Fatal(err)
	}
	f.setSettingsRaw(string(data))
}

func (f *fixture) setSettingsRaw(raw string) {
	f.t.Helper()
	p := filepath.Join(f.base, host, sitecfg.FileName)
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		f.t.Fatal(err)
	}
	// Кэш шлюза сверяет время и размер: разводим время, чтобы одинаковый по размеру файл тоже считался новым.
	future := time.Now().Add(time.Duration(len(raw)+int(time.Now().UnixNano()%1000)) * time.Second)
	_ = os.Chtimes(p, future, future)
}

func TestRootDirBecomesSiteRoot(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "OLD", "dist/index.html": "<h1>built</h1>", "dist/app.js": "js", "secret.txt": "top secret"})
	f.setSettings(sitecfg.Settings{RootDir: "dist"})
	if r := f.get("/"); r.Code != 200 || r.body != "<h1>built</h1>" {
		t.Fatalf("корень из настроек: %d %q", r.Code, r.body)
	}
	if r := f.get("/app.js"); r.body != "js" {
		t.Fatalf("файл корня: %q", r.body)
	}
	for _, p := range []string{"/secret.txt", "/../secret.txt", "/%2e%2e/secret.txt", "/dist/app.js"} {
		if r := f.get(p); r.Code == 200 {
			t.Errorf("%s вышел за корень сайта: %q", p, r.body)
		}
	}
	// Убрали настройку — снова весь public.
	f.setSettings(sitecfg.Settings{})
	if r := f.get("/"); r.body != "OLD" {
		t.Fatalf("после сброса: %q", r.body)
	}
}

func TestRootDirMissingIsNotFoundAndNamesWithOwnFolderIgnoreIt(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "main", "dist/index.html": "built", "shop/index.html": "shop"})
	dir := f.withDomains()
	bind(t, dir, "shop.example.com", host+"\nshop\n")
	bind(t, dir, "plain.example.com", host+"\n")

	f.setSettings(sitecfg.Settings{RootDir: "dist"})
	if r := f.doHost("plain.example.com", "GET", "/"); r.body != "built" {
		t.Fatalf("имя без папки отдаёт корень из настроек: %q", r.body)
	}
	if r := f.doHost("shop.example.com", "GET", "/"); r.body != "shop" {
		t.Fatalf("имя со своей папкой не зависит от корня: %q", r.body)
	}
	f.setSettings(sitecfg.Settings{RootDir: "not-there"})
	if r := f.get("/"); r.Code != 404 {
		t.Fatalf("корневой папки нет: %d %q", r.Code, r.body)
	}
	if r := f.doHost("shop.example.com", "GET", "/"); r.body != "shop" {
		t.Fatalf("папка имени работает и при сломанном корне: %q", r.body)
	}
}

func TestCustomIndexFiles(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "default", "home.html": "home", "start.htm": "start", "docs/start.htm": "docs-start"})
	f.setSettings(sitecfg.Settings{Index: []string{"start.htm", "home.html"}})
	if r := f.get("/"); r.body != "start" {
		t.Fatalf("порядок индексных файлов: %q", r.body)
	}
	if r := f.get("/docs/"); r.body != "docs-start" {
		t.Fatalf("в подкаталоге: %q", r.body)
	}
	// Заданный список заменяет умолчание: index.html не подхватывается сам.
	f.setSettings(sitecfg.Settings{Index: []string{"absent.html"}})
	if r := f.get("/"); r.Code != 403 {
		t.Fatalf("нет ни одного индексного файла: %d %q", r.Code, r.body)
	}
	// .htaccess сильнее настроек панели.
	f.write(".htaccess", "DirectoryIndex home.html\n")
	if r := f.get("/"); r.body != "home" {
		t.Fatalf("DirectoryIndex из .htaccess: %q", r.body)
	}
}

func TestAutoindexSetting(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x", "files/a.txt": "a", "files/b.txt": "b"})
	if r := f.get("/files/"); r.Code != 403 {
		t.Fatalf("по умолчанию листинга нет: %d", r.Code)
	}
	f.setSettings(sitecfg.Settings{Autoindex: true})
	r := f.get("/files/")
	if r.Code != 200 || !strings.Contains(r.body, "a.txt") || !strings.Contains(r.body, "b.txt") {
		t.Fatalf("листинг включён: %d %s", r.Code, r.body)
	}
	// Директива .htaccess сильнее панели — в обе стороны.
	f.write("files/.htaccess", "Options -Indexes\n")
	if r := f.get("/files/"); r.Code != 403 {
		t.Fatalf("-Indexes в .htaccess: %d", r.Code)
	}
	f.setSettings(sitecfg.Settings{})
	f.write("files/.htaccess", "Options    +Indexes\n") // другой размер: кэш .htaccess сверяет размер и время изменения
	if r := f.get("/files/"); r.Code != 200 {
		t.Fatalf("+Indexes в .htaccess: %d", r.Code)
	}
}

func TestErrorPagesFromSettings(t *testing.T) {
	f := newSite(t, map[string]string{
		"index.html": "ok", "errors/404.html": "<h1>my 404</h1>", "errors/403.html": "<h1>my 403</h1>", "sub/.htaccess": "ErrorDocument 404 /errors/other.html\n",
		"errors/other.html": "from htaccess",
	})
	f.setSettings(sitecfg.Settings{ErrorPages: map[string]string{"404": "errors/404.html", "403": "errors/403.html", "500": "errors/none.html"}})

	r := f.get("/missing")
	if r.Code != 404 || r.body != "<h1>my 404</h1>" || !strings.HasPrefix(r.header("Content-Type"), "text/html") {
		t.Fatalf("своя 404: %d %q %q", r.Code, r.body, r.header("Content-Type"))
	}
	if r := f.do("HEAD", "/missing"); r.Code != 404 || r.body != "" {
		t.Fatalf("HEAD: %d %q", r.Code, r.body)
	}
	if r := f.get("/.htpasswd"); r.Code != 403 || r.body != "<h1>my 403</h1>" {
		t.Fatalf("своя 403: %d %q", r.Code, r.body)
	}
	// ErrorDocument из .htaccess сильнее страницы из панели.
	if r := f.get("/sub/missing"); r.Code != 404 || r.body != "from htaccess" {
		t.Fatalf("приоритет .htaccess: %d %q", r.Code, r.body)
	}
	// Файла страницы нет — страница по умолчанию, а не пустой ответ.
	f.setSettings(sitecfg.Settings{ErrorPages: map[string]string{"404": "errors/gone.html"}})
	if r := f.get("/missing"); r.Code != 404 || r.body == "" || strings.Contains(r.body, "my 404") {
		t.Fatalf("нет файла страницы: %d %.80q", r.Code, r.body)
	}
	// Страницами ошибок не становятся скрытые и служебные файлы.
	f.write(".secret.html", "hidden")
	f.setSettings(sitecfg.Settings{})
	f.setSettingsRaw(`{"error_pages":{"404":".secret.html","403":"sub/.htaccess","500":"../x.html"}}`)
	if r := f.get("/missing"); strings.Contains(r.body, "hidden") {
		t.Fatalf("скрытый файл стал страницей ошибки: %q", r.body)
	}
	if r := f.get("/.htpasswd"); strings.Contains(r.body, "ErrorDocument") {
		t.Fatalf(".htaccess стал страницей ошибки: %q", r.body)
	}
}

func TestErrorPagesRelativeToRootDir(t *testing.T) {
	f := newSite(t, map[string]string{"dist/index.html": "ok", "dist/404.html": "<h1>dist 404</h1>", "404.html": "<h1>outer 404</h1>"})
	f.setSettings(sitecfg.Settings{RootDir: "dist", ErrorPages: map[string]string{"404": "404.html"}})
	if r := f.get("/nope"); r.body != "<h1>dist 404</h1>" {
		t.Fatalf("страница ошибки ищется в корне сайта: %q", r.body)
	}
}

func TestWWWRedirect(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "site"})
	dir := f.withDomains()
	bind(t, dir, "example.com", host+"\n")
	bind(t, dir, "www.example.com", host+"\n")
	bind(t, dir, "alone.org", host+"\n") // www.alone.org не подключён
	bind(t, dir, "other.net", host+"\n")
	bind(t, dir, "www.other.net", "victim.mary.vladinc.ru\n") // подключён к чужому сайту

	loc := func(r resp) string { return r.header("Location") }

	f.setSettings(sitecfg.Settings{WWW: "add"})
	r := f.doHost("example.com", "GET", "/docs/a.html?x=1&y=2")
	if r.Code != 301 || loc(r) != "https://www.example.com/docs/a.html?x=1&y=2" {
		t.Fatalf("add: %d %q", r.Code, loc(r))
	}
	if cc := r.header("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Fatalf("редирект не должен кэшироваться навечно: %q", cc)
	}
	if r := f.doHost("www.example.com", "GET", "/"); r.Code != 200 {
		t.Fatalf("сам www не переадресуется (цикл): %d", r.Code)
	}
	// Второе имя не подключено или чужое — переадресовывать некуда, сайт отдаётся как есть.
	for _, h := range []string{"alone.org", "other.net"} {
		if r := f.doHost(h, "GET", "/"); r.Code != 200 {
			t.Errorf("%s: переадресация на неподключённое имя: %d %q", h, r.Code, loc(r))
		}
	}
	// POST не переадресуется.
	if r := f.doHost("example.com", "POST", "/"); r.Code == 301 {
		t.Fatalf("POST переадресован")
	}

	f.setSettings(sitecfg.Settings{WWW: "remove"})
	r = f.doHost("www.example.com", "GET", "/p?q=1")
	if r.Code != 301 || loc(r) != "https://example.com/p?q=1" {
		t.Fatalf("remove: %d %q", r.Code, loc(r))
	}
	if r := f.doHost("example.com", "GET", "/"); r.Code != 200 {
		t.Fatalf("голый домен не переадресуется (цикл): %d", r.Code)
	}
	if r := f.doHost("www.other.net", "GET", "/"); r.Code == 301 && strings.Contains(loc(r), "other.net") {
		t.Fatalf("чужой www переадресован")
	}

	f.setSettings(sitecfg.Settings{})
	if r := f.doHost("example.com", "GET", "/"); r.Code != 200 {
		t.Fatalf("настройка выключена: %d", r.Code)
	}
}

func TestWWWRedirectForSiteAddressUsesWWWSubdomain(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "site"})
	dir := f.withDomains()
	f.setSettings(sitecfg.Settings{WWW: "add"})
	// Поддомена www ещё нет — редиректа нет.
	if r := f.get("/"); r.Code != 200 {
		t.Fatalf("без поддомена www: %d", r.Code)
	}
	bind(t, dir, "www."+host, host+"\n")
	r := f.get("/a?b=c")
	if r.Code != 301 || r.header("Location") != "https://www."+host+"/a?b=c" {
		t.Fatalf("%d %q", r.Code, r.header("Location"))
	}
	if r := f.doHost("www."+host, "GET", "/"); r.Code != 200 {
		t.Fatalf("цикл на www-поддомене: %d", r.Code)
	}
}

func TestHSTSHeader(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	if r := f.get("/"); r.header("Strict-Transport-Security") != "" {
		t.Fatal("HSTS по умолчанию выключен")
	}
	f.setSettings(sitecfg.Settings{HSTS: true})
	for _, p := range []string{"/", "/missing", "/.htpasswd"} {
		if hv := f.get(p).header("Strict-Transport-Security"); !strings.HasPrefix(hv, "max-age=") || strings.Contains(hv, "includeSubDomains") {
			t.Errorf("%s: %q", p, hv)
		}
	}
}

func TestSettingsFileChangesAreNoticed(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "a", "b/index.html": "b", "c/index.html": "c"})
	for _, dir := range []string{"b", "c", "b", ""} {
		f.setSettings(sitecfg.Settings{RootDir: dir})
		want := map[string]string{"": "a", "b": "b", "c": "c"}[dir]
		if r := f.get("/"); r.body != want {
			t.Fatalf("root_dir=%q: %q, ожидали %q", dir, r.body, want)
		}
	}
	// Файл удалили — умолчания.
	f.setSettings(sitecfg.Settings{RootDir: "b"})
	if err := os.Remove(filepath.Join(f.base, host, sitecfg.FileName)); err != nil {
		t.Fatal(err)
	}
	if r := f.get("/"); r.body != "a" {
		t.Fatalf("после удаления файла настроек: %q", r.body)
	}
}

func TestHostileSettingsFileIsHarmless(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "main"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "index.html"), []byte("OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"root_dir":"../../..","www":"evil"}`, `{"root_dir":"` + outside + `"}`, `{"root_dir":"/etc"}`, `{"index":["../../etc/passwd"]}`,
		`not json`, ``, `{"root_dir":".git"}`, `[]`,
	} {
		f.setSettingsRaw(raw)
		r := f.get("/")
		// Либо настройка отброшена и сайт отдаётся как обычно, либо корень не найден; чужие файлы не отдаются никогда.
		if strings.Contains(r.body, "OUTSIDE") || strings.Contains(r.body, "root:") || (r.Code != 200 && r.Code != 404) || (r.Code == 200 && r.body != "main") {
			t.Errorf("%q: %d %.60q", raw, r.Code, r.body)
		}
	}
}

func TestSettingsFileIsNotPublic(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	f.setSettings(sitecfg.Settings{HSTS: true})
	for _, p := range []string{"/settings.json", "/../settings.json", "/%2e%2e/settings.json"} {
		if r := f.get(p); r.Code == 200 {
			t.Errorf("%s отдал настройки: %q", p, r.body)
		}
	}
}

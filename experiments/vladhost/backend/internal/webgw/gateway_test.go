package webgw_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"vladhost/internal/webgw"
)

const host = "blog.john.vladinc.ru"

type fixture struct {
	t    *testing.T
	base string // корень сайтов
	pub  string // public сайта blog.john
	h    *webgw.Handler
}

func newSite(t *testing.T, files map[string]string) *fixture {
	t.Helper()
	base := t.TempDir()
	pub := filepath.Join(base, host, "public")
	if err := os.MkdirAll(pub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, base: base, pub: pub, h: webgw.New(webgw.Options{Root: base, BaseDomain: "vladinc.ru"})}
	for name, body := range files {
		f.write(name, body)
	}
	return f
}

func (f *fixture) write(name, body string) {
	f.t.Helper()
	p := filepath.Join(f.pub, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

type resp struct {
	*httptest.ResponseRecorder
	body string
}

func (f *fixture) do(method, target string, hdr ...string) resp {
	f.t.Helper()
	return f.doHost(host, method, target, hdr...)
}

func (f *fixture) doHost(h, method, target string, hdr ...string) resp {
	f.t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Host = h
	req.RemoteAddr = "203.0.113.7:5555"
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	b, _ := io.ReadAll(w.Result().Body)
	return resp{w, string(b)}
}

func (f *fixture) get(target string, hdr ...string) resp { return f.do("GET", target, hdr...) }

func (r resp) header(k string) string { return r.Result().Header.Get(k) }

func mustStatus(t *testing.T, r resp, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("статус %d, ожидали %d; тело: %.200s", r.Code, want, r.body)
	}
}

func TestServesFilesWithHeaders(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "<h1>hello</h1>", "css/a.css": "b{}", "data.bin": "\x00\x01binary", "note.xyz": "plain words"})
	r := f.get("/")
	mustStatus(t, r, 200)
	if r.body != "<h1>hello</h1>" || r.header("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("индекс: %q %q", r.body, r.header("Content-Type"))
	}
	if r.header("X-Content-Type-Options") != "nosniff" || r.header("ETag") == "" || r.header("Last-Modified") == "" {
		t.Fatalf("заголовки: %v", r.Result().Header)
	}
	if ct := f.get("/css/a.css").header("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Fatalf("css: %q", ct)
	}
	if ct := f.get("/data.bin").header("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("бинарный без расширения из таблицы: %q", ct)
	}
	if ct := f.get("/note.xyz").header("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("неизвестное расширение определяется по содержимому: %q", ct)
	}

	// Условные запросы и Range.
	etag := r.header("ETag")
	mustStatus(t, f.get("/", "If-None-Match", etag), 304)
	part := f.get("/css/a.css", "Range", "bytes=0-0")
	mustStatus(t, part, 206)
	if part.body != "b" {
		t.Fatalf("Range: %q", part.body)
	}
	head := f.do("HEAD", "/")
	mustStatus(t, head, 200)
	if head.body != "" || head.header("Content-Type") == "" {
		t.Fatalf("HEAD: тело %q", head.body)
	}
}

func TestUnknownSiteAndHostValidation(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "x"})
	for _, h := range []string{"nobody.john.vladinc.ru", "app.vladinc.ru", "vladinc.ru", "a.b.c.vladinc.ru", "blog.john.evil.com", "BLOG.JOHN.VLADINC.RU.", "..john.vladinc.ru", "blog.john.vladinc.ru.evil.com", "blog%2fx.john.vladinc.ru"} {
		r := f.doHost(h, "GET", "/")
		if h == "BLOG.JOHN.VLADINC.RU." {
			mustStatus(t, r, 200) // регистр и точка в конце не важны
			continue
		}
		mustStatus(t, r, 404)
		if !strings.Contains(r.body, "Сайт не найден") {
			t.Errorf("%s: страница «сайт не найден»: %.100s", h, r.body)
		}
	}
	// Порт в Host игнорируется.
	mustStatus(t, f.doHost(host+":443", "GET", "/"), 200)
	// Страница на итальянском по Accept-Language.
	r := f.doHost("nobody.john.vladinc.ru", "GET", "/", "Accept-Language", "it-IT,it;q=0.9")
	if !strings.Contains(r.body, "Sito non trovato") || r.header("Content-Language") != "it" {
		t.Fatalf("итальянский: %.120s", r.body)
	}
}

func TestEmptySiteAndDefaultPages(t *testing.T) {
	f := newSite(t, nil)
	r := f.get("/")
	mustStatus(t, r, 200)
	if !strings.Contains(r.body, "Здесь скоро появится сайт") || r.header("Cache-Control") != "no-store" {
		t.Fatalf("пустой сайт: %.150s", r.body)
	}
	it := f.get("/", "Accept-Language", "it")
	if !strings.Contains(it.body, "Qui arriverà presto un sito") || !strings.Contains(it.body, `lang="it"`) {
		t.Fatalf("пустой сайт по-итальянски: %.150s", it.body)
	}
	// Скрытые файлы (например, только .htaccess) не делают сайт «непустым».
	f.write(".htaccess", "Options -Indexes\n")
	mustStatus(t, f.get("/"), 200)
	// Любой другой адрес пустого сайта — 404 со страницей по умолчанию, а не файл из папки пользователя.
	nf := f.get("/nothing.html")
	mustStatus(t, nf, 404)
	if !strings.Contains(nf.body, "Страница не найдена") || !strings.Contains(nf.body, ">404<") {
		t.Fatalf("404: %.200s", nf.body)
	}
	if entries, _ := os.ReadDir(f.pub); len(entries) != 1 {
		t.Fatalf("шлюз не должен создавать файлы в папке сайта: %v", entries)
	}
	if !strings.Contains(f.get("/nothing", "Accept-Language", "it").body, "Pagina non trovata") {
		t.Error("404 по-итальянски")
	}

	// Сайт с файлами, но без index: корень закрыт (403), а не «пустой сайт».
	f.write("about.html", "about")
	r = f.get("/")
	mustStatus(t, r, 403)
	if !strings.Contains(r.body, "Доступ запрещён") {
		t.Fatalf("403: %.150s", r.body)
	}
	mustStatus(t, f.get("/nothing.html"), 404)
	// HEAD и страница ошибки: без тела.
	if h := f.do("HEAD", "/nothing.html"); h.Code != 404 || h.body != "" {
		t.Fatalf("HEAD 404: %d %q", h.Code, h.body)
	}
}

func TestDirectoryHandling(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "root", "docs/index.html": "docs", "bare/x.txt": "x", "other/home.html": "home"})
	r := f.get("/docs?a=1")
	mustStatus(t, r, 301)
	if loc := r.header("Location"); loc != "https://"+host+"/docs/?a=1" {
		t.Fatalf("редирект на слэш: %q", loc)
	}
	if body := f.get("/docs/").body; body != "docs" {
		t.Fatalf("индекс папки: %q", body)
	}
	mustStatus(t, f.get("/bare/"), 403)
	f.write("other/.htaccess", "DirectoryIndex home.html\n")
	if body := f.get("/other/").body; body != "home" {
		t.Fatalf("DirectoryIndex: %q", body)
	}
}

func TestDirectoryListing(t *testing.T) {
	f := newSite(t, map[string]string{
		".htaccess": "Options +Indexes\n", "b.txt": "12345", "a/inner.txt": "i", "<script>alert(1)</script>.txt": "x",
		".secret": "s", "keep.env": "k",
	})
	f.write("sub/.htaccess", "Options -Indexes\n")
	f.write("sub/f.txt", "f")
	r := f.get("/")
	mustStatus(t, r, 200)
	if strings.Contains(r.body, "<script>alert") || !strings.Contains(r.body, "&lt;script&gt;") {
		t.Fatalf("имя файла должно экранироваться: %s", r.body)
	}
	if strings.Contains(r.body, ".secret") || strings.Contains(r.body, ".htaccess") {
		t.Fatal("скрытые файлы не листингуются")
	}
	if !strings.Contains(r.body, "b.txt") || !strings.Contains(r.body, `href="a/"`) || !strings.Contains(r.body, "5 B") {
		t.Fatalf("листинг: %s", r.body)
	}
	if strings.Index(r.body, "a/") > strings.Index(r.body, "b.txt") {
		t.Error("папки идут первыми")
	}
	mustStatus(t, f.get("/sub/"), 403) // подкаталог переопределил Indexes
	if !strings.Contains(f.get("/a/", "Accept-Language", "it").body, "Su") {
		t.Error("листинг в подпапке по-итальянски")
	}
}

func TestHiddenAndProtectedFiles(t *testing.T) {
	f := newSite(t, map[string]string{
		"index.html": "i", ".htaccess": "Header set X-A 1\n", ".htpasswd": "u:p", ".env": "SECRET=1", ".git/config": "[core]",
		".well-known/acme.txt": "ok", "sub/.htaccess": "x", "sub/.hidden": "h",
	})
	for path, want := range map[string]int{
		"/.htaccess": 403, "/.htpasswd": 403, "/sub/.htaccess": 403, "/.HTACCESS": 403, // запрет без учёта регистра: на регистронезависимой ФС это тот же файл

		"/.env": 404, "/.git/config": 404, "/sub/.hidden": 404, "/.well-known/acme.txt": 200,
	} {
		r := f.get(path)
		if r.Code != want {
			t.Errorf("%s → %d, ожидали %d", path, r.Code, want)
		}
		if strings.Contains(r.body, "SECRET") || strings.Contains(r.body, "Header set") || strings.Contains(r.body, "[core]") {
			t.Errorf("%s: утечка содержимого", path)
		}
	}
}

func TestPathTraversalAndSymlinks(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "ok", "sub/page.html": "p"})
	if err := os.WriteFile(filepath.Join(f.base, "secret.txt"), []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.base, host, "other.txt"), []byte("TOP-SECRET-SIBLING"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Симлинки, подложенные в обход панели: наружу они вести не должны.
	if err := os.Symlink(filepath.Join(f.base, "secret.txt"), filepath.Join(f.pub, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.base, filepath.Join(f.pub, "linkdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("index.html", filepath.Join(f.pub, "inner-link.html")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"/../secret.txt", "/../../secret.txt", "/sub/../../secret.txt", "/%2e%2e/secret.txt", "/%2e%2e%2fsecret.txt",
		"/..%2fsecret.txt", "/sub/%2e%2e/%2e%2e/secret.txt", "/link.txt", "/linkdir/secret.txt", "/linkdir/", "/inner-link.html",
		"/..\\secret.txt", "/../other.txt", "//../secret.txt", "/%00", "/index.html%00.txt",
	} {
		r := f.get(p)
		if strings.Contains(r.body, "TOP-SECRET") {
			t.Errorf("%s отдал файл за пределами сайта", p)
		}
	}
	// «Выше корня» схлопывается в корень сайта: это тот же сайт, а не соседний файл.
	if r := f.get("/../index.html"); r.body != "ok" {
		t.Errorf("нормализация: %q", r.body)
	}
	mustStatus(t, f.get("/link.txt"), 404)
	mustStatus(t, f.get("/linkdir/secret.txt"), 404)
	mustStatus(t, f.get("/inner-link.html"), 404) // симлинки не поддерживаются вообще
}

func TestErrorDocuments(t *testing.T) {
	f := newSite(t, map[string]string{
		"index.html": "i", "err/404.html": "<h1>my 404</h1>", "err/403.html": "my 403",
		".htaccess": "ErrorDocument 404 /err/404.html\nErrorDocument 403 /err/403.html\nErrorDocument 410 \"gone text\"\nErrorDocument 500 https://example.org/oops\nErrorDocument 401 /missing.html\nRedirect gone /old\nHeader always set X-Err yes\nOptions -Indexes\n",
	})
	r := f.get("/nope")
	mustStatus(t, r, 404)
	if r.body != "<h1>my 404</h1>" || r.header("Content-Type") != "text/html; charset=utf-8" || r.header("X-Err") != "yes" {
		t.Fatalf("свой 404: %q %v", r.body, r.Result().Header)
	}
	f.write("empty/x.txt", "x")
	r = f.get("/empty/")
	mustStatus(t, r, 403)
	if r.body != "my 403" {
		t.Fatalf("свой 403: %q", r.body)
	}
	r = f.get("/old")
	mustStatus(t, r, 410)
	if r.body != "gone text" || !strings.HasPrefix(r.header("Content-Type"), "text/plain") {
		t.Fatalf("текстовый ErrorDocument: %q", r.body)
	}
	// Файл ErrorDocument отсутствует: показывается страница по умолчанию.
	f.write(".htaccess", "ErrorDocument 404 /missing-doc.html\n")
	r = f.get("/nope")
	mustStatus(t, r, 404)
	if !strings.Contains(r.body, "Страница не найдена") {
		t.Fatalf("запасная страница: %.100s", r.body)
	}
	// ErrorDocument не должен раскрывать скрытые файлы и файлы за паролем.
	f.write(".htaccess", "ErrorDocument 404 /.env\n")
	f.write(".env", "SECRET=1")
	if r := f.get("/nope"); strings.Contains(r.body, "SECRET") {
		t.Fatal("ErrorDocument раскрыл скрытый файл")
	}
	f.write(".htaccess", "ErrorDocument 404 /../../etc/passwd\n")
	if r := f.get("/nope"); strings.Contains(r.body, "root:") {
		t.Fatal("ErrorDocument вышел за пределы сайта")
	}
	f.write(".htaccess", "ErrorDocument 404 https://example.org/notfound\n")
	r = f.get("/nope")
	mustStatus(t, r, 302)
	if r.header("Location") != "https://example.org/notfound" {
		t.Fatalf("ErrorDocument-URL: %q", r.header("Location"))
	}
}

func TestRewriteEndToEnd(t *testing.T) {
	f := newSite(t, map[string]string{
		"index.html": "spa", "about.html": "about page", "assets/app.js": "js",
		".htaccess": "RewriteEngine On\nRewriteCond %{REQUEST_FILENAME}.html -f\nRewriteRule ^(.*)$ $1.html [L]\nRewriteCond %{REQUEST_FILENAME} !-f\nRewriteCond %{REQUEST_FILENAME} !-d\nRewriteRule ^ /index.html [L]\n",
	})
	for path, want := range map[string]string{"/about": "about page", "/assets/app.js": "js", "/deep/link/x": "spa", "/": "spa"} {
		if r := f.get(path); r.Code != 200 || r.body != want {
			t.Errorf("%s → %d %q, ожидали %q", path, r.Code, r.body, want)
		}
	}
	f.write(".htaccess", "RewriteEngine On\nRewriteRule ^old/(.*)$ /new/$1 [R=301,L]\nRewriteRule ^private - [F]\nRewriteRule ^gone - [G]\nRewriteRule ^ext$ https://example.org/x?y=1 [R=302]\n")
	r := f.get("/old/a/b?x=1")
	mustStatus(t, r, 301)
	if r.header("Location") != "https://"+host+"/new/a/b?x=1" {
		t.Fatalf("Location: %q", r.header("Location"))
	}
	mustStatus(t, f.get("/private"), 403)
	mustStatus(t, f.get("/gone"), 410)
	if r := f.get("/ext"); r.Code != 302 || r.header("Location") != "https://example.org/x?y=1" {
		t.Fatalf("внешний: %d %q", r.Code, r.header("Location"))
	}
	// Правило-петля не должно зависать: после лимита проходов — 500.
	f.write(".htaccess", "RewriteEngine On\nRewriteRule ^a$ /b [L]\nRewriteRule ^b$ /a [L]\n")
	done := make(chan resp, 1)
	go func() { done <- f.get("/a") }()
	select {
	case r := <-done:
		mustStatus(t, r, 500)
	case <-time.After(3 * time.Second):
		t.Fatal("петля переписывания зависла")
	}
	// Переписывание не выводит за пределы сайта и не отдаёт скрытое.
	f.write(".htaccess", "RewriteEngine On\nRewriteRule ^leak$ /../../secret.txt [L]\nRewriteRule ^env$ /.env [L]\nRewriteRule ^ht$ /.htaccess [L]\n")
	f.write(".env", "SECRET")
	for _, p := range []string{"/leak", "/env", "/ht"} {
		if r := f.get(p); strings.Contains(r.body, "SECRET") || strings.Contains(r.body, "RewriteRule") {
			t.Errorf("%s раскрыл файл", p)
		}
	}
}

func TestRedirectDirectives(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "i", ".htaccess": "Redirect 301 /old /new\nRedirectMatch 302 ^/blog/(\\d+)$ /posts/$1\nRedirect gone /dead\n"})
	if r := f.get("/old/page"); r.Code != 301 || r.header("Location") != "https://"+host+"/new/page" {
		t.Fatalf("%d %q", r.Code, r.header("Location"))
	}
	if r := f.get("/blog/42"); r.Code != 302 || r.header("Location") != "https://"+host+"/posts/42" {
		t.Fatalf("%d %q", r.Code, r.header("Location"))
	}
	mustStatus(t, f.get("/dead"), 410)
}

func TestHeadersExpiresTypes(t *testing.T) {
	f := newSite(t, map[string]string{
		"index.html": "i", "app.js": "js", "pic.png": "png", "x.cust": "c", "vendor.js.gz": "gzdata",
		".htaccess": `Header set X-Frame-Options DENY
Header set Set-Cookie "evil=1; Domain=.vladinc.ru"
Header set Location "https://evil.example"
<FilesMatch "\.js$">
Header set Cache-Control "public, max-age=31536000, immutable"
</FilesMatch>
ExpiresActive On
ExpiresByType image/png "access plus 1 month"
AddType application/x-cust .cust
AddEncoding gzip .gz
`,
	})
	r := f.get("/index.html")
	if r.header("X-Frame-Options") != "DENY" || r.header("Set-Cookie") != "" || r.header("Location") != "" {
		t.Fatalf("заголовки: %v", r.Result().Header)
	}
	if cc := f.get("/app.js").header("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("FilesMatch: %q", cc)
	}
	png := f.get("/pic.png")
	if png.header("Cache-Control") != "max-age=2592000" || png.header("Expires") == "" {
		t.Fatalf("Expires: %v", png.Result().Header)
	}
	if ct := f.get("/x.cust").header("Content-Type"); ct != "application/x-cust" {
		t.Fatalf("AddType: %q", ct)
	}
	gz := f.get("/vendor.js.gz")
	if gz.header("Content-Encoding") != "gzip" || !strings.HasPrefix(gz.header("Content-Type"), "application/javascript") {
		t.Fatalf("AddEncoding: %v", gz.Result().Header)
	}
	// Заголовки применяются и к страницам ошибок (Header always).
	f.write(".htaccess", "Header always set X-Robots-Tag noindex\nHeader set X-Ok 1\n")
	nf := f.get("/missing")
	if nf.Code != 404 || nf.header("X-Robots-Tag") != "noindex" || nf.header("X-Ok") != "" {
		t.Fatalf("always на 404: %v", nf.Result().Header)
	}
}

func TestAccessRules(t *testing.T) {
	f := newSite(t, map[string]string{
		"index.html": "i", "dump.sql": "SELECT", "config.ini": "pw", "ok.txt": "ok",
		".htaccess": "<FilesMatch \"\\.(sql|bak)$\">\nRequire all denied\n</FilesMatch>\n<Files \"config.ini\">\nOrder allow,deny\nDeny from all\n</Files>\n",
	})
	mustStatus(t, f.get("/dump.sql"), 403)
	mustStatus(t, f.get("/config.ini"), 403)
	mustStatus(t, f.get("/ok.txt"), 200)
	// Подкаталог может разрешить обратно.
	f.write("pub/dump.sql", "public dump")
	f.write("pub/.htaccess", "Require all granted\n")
	mustStatus(t, f.get("/pub/dump.sql"), 200)
}

func TestBasicAuth(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	f := newSite(t, map[string]string{
		"index.html": "public", "private/data.txt": "classified",
		"private/.htpasswd": "alice:" + string(hash) + "\nbob:" + string(hash) + "\n",
		"private/.htaccess": "AuthType Basic\nAuthName \"Staff only\"\nAuthUserFile /home/whoever/private/.htpasswd\nRequire valid-user\n",
	})
	mustStatus(t, f.get("/index.html"), 200)
	r := f.get("/private/data.txt")
	mustStatus(t, r, 401)
	if !strings.Contains(r.header("WWW-Authenticate"), `Basic realm="Staff only"`) || strings.Contains(r.body, "classified") {
		t.Fatalf("вызов: %q", r.header("WWW-Authenticate"))
	}
	basic := func(u, p string) string {
		req, _ := http.NewRequest("GET", "/", nil)
		req.SetBasicAuth(u, p)
		return req.Header.Get("Authorization")
	}
	if r := f.get("/private/data.txt", "Authorization", basic("alice", "s3cret")); r.Code != 200 || r.body != "classified" {
		t.Fatalf("верный пароль: %d %q", r.Code, r.body)
	}
	for _, c := range [][2]string{{"alice", "wrong"}, {"nobody", "s3cret"}, {"", ""}, {"alice", ""}} {
		if r := f.get("/private/data.txt", "Authorization", basic(c[0], c[1])); r.Code != 401 || strings.Contains(r.body, "classified") {
			t.Errorf("%v: %d", c, r.Code)
		}
	}
	// Файл паролей нельзя скачать.
	mustStatus(t, f.get("/private/.htpasswd", "Authorization", basic("alice", "s3cret")), 403)

	// Только перечисленные пользователи.
	f.write("private/.htaccess", "AuthType Basic\nAuthName x\nAuthUserFile .htpasswd\nRequire user bob\n")
	mustStatus(t, f.get("/private/data.txt", "Authorization", basic("alice", "s3cret")), 401)
	mustStatus(t, f.get("/private/data.txt", "Authorization", basic("bob", "s3cret")), 200)

	// Нет файла паролей — ошибка настройки (500), а не открытый доступ.
	f.write("private/.htaccess", "AuthType Basic\nAuthName x\nAuthUserFile nothere\nRequire valid-user\n")
	r = f.get("/private/data.txt", "Authorization", basic("alice", "s3cret"))
	if r.Code != 500 || strings.Contains(r.body, "classified") {
		t.Fatalf("без файла паролей: %d", r.Code)
	}
}

func TestBasicAuthBruteForceLimit(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	f := newSite(t, map[string]string{
		"p/data.txt": "x", "p/.htpasswd": "alice:" + string(hash),
		"p/.htaccess": "AuthType Basic\nAuthName x\nAuthUserFile .htpasswd\nRequire valid-user\n",
	})
	bad := func() string {
		req, _ := http.NewRequest("GET", "/", nil)
		req.SetBasicAuth("alice", "nope")
		return req.Header.Get("Authorization")
	}
	for range 10 {
		mustStatus(t, f.get("/p/data.txt", "Authorization", bad()), 401)
	}
	r := f.get("/p/data.txt", "Authorization", bad())
	mustStatus(t, r, 429)
	if r.header("Retry-After") == "" {
		t.Error("нужен Retry-After")
	}
	// Даже верный пароль с заблокированного адреса временно не принимается.
	good, _ := http.NewRequest("GET", "/", nil)
	good.SetBasicAuth("alice", "s3cret")
	mustStatus(t, f.get("/p/data.txt", "Authorization", good.Header.Get("Authorization")), 429)

	// Другой клиент (адрес из X-Forwarded-For от локального nginx) не блокируется.
	req := httptest.NewRequest("GET", "/p/data.txt", nil)
	req.Host = host
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	req.Header.Set("Authorization", good.Header.Get("Authorization"))
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("другой клиент: %d", w.Code)
	}
	// Адрес из заголовка учитывается только от локального прокси: снаружи его подделать нельзя.
	req = httptest.NewRequest("GET", "/p/data.txt", nil)
	req.Host = host
	req.RemoteAddr = "203.0.113.7:9"
	req.Header.Set("X-Forwarded-For", "198.51.100.77")
	req.Header.Set("Authorization", good.Header.Get("Authorization"))
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Fatalf("подделка X-Forwarded-For не должна обходить блокировку: %d", w.Code)
	}
}

func TestMethods(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "i"})
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		r := f.do(m, "/")
		mustStatus(t, r, 405)
		if r.header("Allow") == "" || !strings.Contains(r.body, "GET") {
			t.Errorf("%s: Allow %q", m, r.header("Allow"))
		}
	}
	r := f.do("OPTIONS", "/")
	mustStatus(t, r, 204)
	if !strings.Contains(r.header("Allow"), "GET") {
		t.Errorf("OPTIONS Allow: %q", r.header("Allow"))
	}
}

func TestHtaccessChangesApplyImmediately(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "i", ".htaccess": "Header set X-V one\n"})
	if f.get("/").header("X-V") != "one" {
		t.Fatal("первое значение")
	}
	f.write(".htaccess", "Header set X-V two-two\n") // другой размер: кэш обязан инвалидироваться
	if v := f.get("/").header("X-V"); v != "two-two" {
		t.Fatalf("после правки .htaccess: %q", v)
	}
	if err := os.Remove(filepath.Join(f.pub, ".htaccess")); err != nil {
		t.Fatal(err)
	}
	if v := f.get("/").header("X-V"); v != "" {
		t.Fatalf("после удаления .htaccess: %q", v)
	}
}

func TestOversizedAndBrokenHtaccessDoNotBreakSite(t *testing.T) {
	f := newSite(t, map[string]string{"index.html": "i", ".htaccess": strings.Repeat("# x\n", 40_000)})
	if r := f.get("/"); r.Code != 200 || r.body != "i" {
		t.Fatalf("огромный .htaccess: %d", r.Code)
	}
	f.write(".htaccess", "RewriteRule ( broken\n<Files\nHeader set X \"unterminated\n\x00\xff\n")
	if r := f.get("/"); r.Code != 200 || r.body != "i" {
		t.Fatalf("битый .htaccess: %d", r.Code)
	}
	// .htaccess в подкаталоге, которого нет, и очень глубокий путь не роняют шлюз.
	mustStatus(t, f.get("/"+strings.Repeat("d/", 200)), 404)
}

func TestNestedHtaccessInherits(t *testing.T) {
	f := newSite(t, map[string]string{
		"index.html": "i", "a/b/page.html": "p",
		".htaccess":     "Header set X-Root root\nHeader set X-Both root\n",
		"a/.htaccess":   "Header set X-A a\n",
		"a/b/.htaccess": "Header set X-Both deep\n",
	})
	r := f.get("/a/b/page.html")
	mustStatus(t, r, 200)
	if r.header("X-Root") != "root" || r.header("X-A") != "a" || r.header("X-Both") != "deep" {
		t.Fatalf("наследование заголовков: %v", r.Result().Header)
	}
}

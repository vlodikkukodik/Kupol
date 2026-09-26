package htaccess

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// fakeSite — файловая система для условий -f и -d.
type fakeSite struct{ files, dirs map[string]bool }

func site(files ...string) fakeSite {
	s := fakeSite{files: map[string]bool{}, dirs: map[string]bool{"/": true}}
	for _, f := range files {
		s.files["/"+strings.TrimPrefix(f, "/")] = true
		parts := strings.Split(strings.Trim(f, "/"), "/")
		for i := 1; i < len(parts); i++ {
			s.dirs["/"+strings.Join(parts[:i], "/")] = true
		}
	}
	return s
}

func (s fakeSite) env(query string) Env {
	return Env{
		Host: "blog.john.vladinc.ru", Method: "GET", Query: query, RemoteAddr: "203.0.113.5",
		Header: func(n string) string {
			return map[string]string{"User-Agent": "TestBot/1.0", "Accept-Language": "it"}[n]
		},
		IsFile:     func(p string) bool { return s.files[p] },
		IsDir:      func(p string) bool { return s.dirs[strings.TrimSuffix(p, "/")] || p == "/" },
		IsNonEmpty: func(p string) bool { return s.files[p] },
	}
}

// resolve прогоняет правила так же, как шлюз: до 10 проходов, пока URL меняется.
func resolve(t *testing.T, src, urlPath, query string, fs fakeSite) Outcome {
	t.Helper()
	cfg := Parse([]byte(src))
	st := State{Path: urlPath, Query: query}
	for range 10 {
		eff := Merge([]Dir{{URL: "/", Cfg: cfg}})
		changed := false
		for _, set := range eff.RewriteSets {
			out, ch := set.Apply(st, fs.env(st.Query))
			if out.RedirectStatus > 0 || out.Status > 0 || out.End {
				return out
			}
			st, changed = out.State, changed || ch
		}
		if !changed {
			break
		}
	}
	return Outcome{State: st}
}

func TestRewrite(t *testing.T) {
	spa := "RewriteEngine On\nRewriteCond %{REQUEST_FILENAME} !-f\nRewriteCond %{REQUEST_FILENAME} !-d\nRewriteRule ^ /index.html [L]\n"
	cases := []struct {
		name, src, path, query string
		files                  []string
		wantPath, wantQuery    string
		wantRedirect           int
		wantURL                string
		wantStatus             int
	}{
		{name: "SPA: несуществующий путь → index.html", src: spa, path: "/app/dashboard", files: []string{"index.html"}, wantPath: "/index.html"},
		{name: "SPA: существующий файл не трогаем", src: spa, path: "/style.css", files: []string{"index.html", "style.css"}, wantPath: "/style.css"},
		{name: "SPA: существующая папка не трогаем", src: spa, path: "/img", files: []string{"img/a.png", "index.html"}, wantPath: "/img"},
		{name: "SPA сохраняет query", src: spa, path: "/x", query: "a=1", files: []string{"index.html"}, wantPath: "/index.html", wantQuery: "a=1"},
		{name: "без RewriteEngine On правила не работают", src: "RewriteRule ^ /index.html [L]\n", path: "/x", wantPath: "/x"},
		{name: "RewriteEngine Off", src: "RewriteEngine Off\nRewriteRule ^ /index.html [L]\n", path: "/x", wantPath: "/x"},
		{
			name: "чистые URL: /about → about.html",
			src:  "RewriteEngine On\nRewriteCond %{REQUEST_FILENAME}.html -f\nRewriteRule ^(.*)$ $1.html [L]\n",
			path: "/about", files: []string{"about.html"}, wantPath: "/about.html",
		},
		{
			name: "чистые URL не срабатывают без файла",
			src:  "RewriteEngine On\nRewriteCond %{REQUEST_FILENAME}.html -f\nRewriteRule ^(.*)$ $1.html [L]\n",
			path: "/nope", files: []string{"about.html"}, wantPath: "/nope",
		},
		{
			name: "http→https не нужен: HTTPS всегда on",
			src:  "RewriteEngine On\nRewriteCond %{HTTPS} off\nRewriteRule ^(.*)$ https://%{HTTP_HOST}/$1 [R=301,L]\n",
			path: "/p", wantPath: "/p",
		},
		{
			name: "редирект на другой хост сохраняет query",
			src:  "RewriteEngine On\nRewriteRule ^old/(.*)$ https://example.org/new/$1 [R=301,L]\n",
			path: "/old/page", query: "q=1", wantRedirect: 301, wantURL: "https://example.org/new/page?q=1",
		},
		{
			name: "внутренний редирект R по относительному пути",
			src:  "RewriteEngine On\nRewriteRule ^shop$ /store [R]\n",
			path: "/shop", wantRedirect: 302, wantURL: "/store",
		},
		{
			name: "флаг F",
			src:  "RewriteEngine On\nRewriteRule ^secret - [F]\n",
			path: "/secret", wantStatus: 403,
		},
		{
			name: "флаг G",
			src:  "RewriteEngine On\nRewriteRule ^gone - [G]\n",
			path: "/gone", wantStatus: 410,
		},
		{
			name: "NC: регистр не важен",
			src:  "RewriteEngine On\nRewriteRule ^ABOUT$ /about.html [NC,L]\n",
			path: "/about", wantPath: "/about.html",
		},
		{
			name: "QSA дописывает старый запрос",
			src:  "RewriteEngine On\nRewriteRule ^p/(\\d+)$ /page.html?id=$1 [QSA,L]\n",
			path: "/p/7", query: "lang=it", wantPath: "/page.html", wantQuery: "id=7&lang=it",
		},
		{
			name: "без QSA старый запрос отбрасывается, если в подстановке есть свой",
			src:  "RewriteEngine On\nRewriteRule ^p/(\\d+)$ /page.html?id=$1 [L]\n",
			path: "/p/7", query: "lang=it", wantPath: "/page.html", wantQuery: "id=7",
		},
		{
			name: "QSD отбрасывает запрос",
			src:  "RewriteEngine On\nRewriteRule ^x$ /y [QSD,L]\n",
			path: "/x", query: "a=1", wantPath: "/y",
		},
		{
			name: "OR-условия",
			src:  "RewriteEngine On\nRewriteCond %{HTTP_USER_AGENT} curl [NC,OR]\nRewriteCond %{HTTP_USER_AGENT} testbot [NC]\nRewriteRule ^ /bots.html [L]\n",
			path: "/a", wantPath: "/bots.html",
		},
		{
			name: "AND-условия: второе ложно",
			src:  "RewriteEngine On\nRewriteCond %{HTTP_USER_AGENT} testbot [NC]\nRewriteCond %{REQUEST_URI} ^/b\nRewriteRule ^ /bots.html [L]\n",
			path: "/a", wantPath: "/a",
		},
		{
			name: "заголовок Accept-Language через %{HTTP:...}",
			src:  "RewriteEngine On\nRewriteCond %{HTTP:Accept-Language} ^it\nRewriteRule ^$ /it/ [L]\n",
			path: "/", wantPath: "/it/",
		},
		{
			name: "отрицание шаблона",
			src:  "RewriteEngine On\nRewriteRule !^api/ /index.html [L]\n",
			path: "/page", wantPath: "/index.html",
		},
		{
			name: "цепочка C: второе правило не применяется, если первое не совпало",
			src:  "RewriteEngine On\nRewriteRule ^a$ /a2 [C]\nRewriteRule ^a2$ /a3 [L]\n",
			path: "/b", wantPath: "/b",
		},
		{
			name: "цепочка C: применяются оба",
			src:  "RewriteEngine On\nRewriteRule ^a$ /a2 [C]\nRewriteRule ^a2$ /a3 [L]\n",
			path: "/a", wantPath: "/a3",
		},
		{
			name: "S=1 пропускает следующее правило",
			src:  "RewriteEngine On\nRewriteRule ^x$ /x1 [S=1]\nRewriteRule ^x1$ /skipped\nRewriteRule ^x1$ /x2 [L]\n",
			path: "/x", wantPath: "/x2",
		},
		{
			name: "выход выше корня невозможен",
			src:  "RewriteEngine On\nRewriteRule ^go$ /../../etc/passwd [L]\n",
			path: "/go", wantPath: "/etc/passwd",
		},
		{
			name: "END останавливает всё",
			src:  "RewriteEngine On\nRewriteRule ^a$ /b [END]\nRewriteRule ^b$ /c [L]\n",
			path: "/a", wantPath: "/b",
		},
		{
			name: "петля ограничена десятью проходами и не виснет",
			src:  "RewriteEngine On\nRewriteRule ^a$ /b [L]\nRewriteRule ^b$ /a [L]\n",
			path: "/a", wantPath: "/a", // после 10 проходов останавливаемся
		},
		{
			name: "переменная DOCUMENT_ROOT пуста: путь внутри сайта",
			src:  "RewriteEngine On\nRewriteCond %{DOCUMENT_ROOT}%{REQUEST_URI} !-f\nRewriteRule ^ /404.html [L]\n",
			path: "/missing", files: []string{"404.html"}, wantPath: "/404.html",
		},
		{
			name: "нераспознанное условие -F считается ложным",
			src:  "RewriteEngine On\nRewriteCond %{REQUEST_URI} -F\nRewriteRule ^ /x [L]\n",
			path: "/a", wantPath: "/a",
		},
		{
			name: "P (прокси) не выполняется",
			src:  "RewriteEngine On\nRewriteRule ^api/(.*)$ http://127.0.0.1:9000/$1 [P]\n",
			path: "/api/x", wantPath: "/api/x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := resolve(t, tc.src, tc.path, tc.query, site(tc.files...))
			if out.RedirectStatus != tc.wantRedirect {
				t.Fatalf("редирект %d %q, ожидали %d", out.RedirectStatus, out.RedirectURL, tc.wantRedirect)
			}
			if tc.wantRedirect > 0 {
				if tc.wantURL != "" && out.RedirectURL != tc.wantURL {
					t.Fatalf("URL редиректа %q, ожидали %q", out.RedirectURL, tc.wantURL)
				}
				return
			}
			if out.Status != tc.wantStatus {
				t.Fatalf("статус %d, ожидали %d", out.Status, tc.wantStatus)
			}
			if tc.wantStatus > 0 {
				return
			}
			if out.Path != tc.wantPath || out.Query != tc.wantQuery {
				t.Fatalf("путь %q?%q, ожидали %q?%q", out.Path, out.Query, tc.wantPath, tc.wantQuery)
			}
		})
	}
}

func TestRewriteWWWRedirectTarget(t *testing.T) {
	// Хост в тесте без www, поэтому проверяем условие с подстановкой %1 отдельно.
	src := "RewriteEngine On\nRewriteCond %{HTTP_HOST} ^(blog)\\.(.*)$ [NC]\nRewriteRule ^(.*)$ https://www.%1.%2/$1 [R=301,L]\n"
	out := resolve(t, src, "/p", "", site())
	if out.RedirectStatus != 301 || out.RedirectURL != "https://www.blog.john.vladinc.ru/p" {
		t.Fatalf("%d %q", out.RedirectStatus, out.RedirectURL)
	}
}

func TestPerDirectoryRewriteBase(t *testing.T) {
	root := Parse([]byte("RewriteEngine On\nRewriteRule ^top$ /top.html [L]\n"))
	sub := Parse([]byte("RewriteEngine On\nRewriteRule ^page$ page.html [L]\n"))
	e := Merge([]Dir{{URL: "/", Cfg: root}, {URL: "/docs/", Cfg: sub}})
	if len(e.RewriteSets) != 1 || e.RewriteSets[0].Dir != "/docs/" {
		t.Fatalf("правила подкаталога должны заменять родительские: %+v", e.RewriteSets)
	}
	out, changed := e.RewriteSets[0].Apply(State{Path: "/docs/page"}, site().env(""))
	if !changed || out.Path != "/docs/page.html" {
		t.Fatalf("относительная подстановка должна идти от каталога .htaccess: %q", out.Path)
	}
	// RewriteOptions Inherit подмешивает правила родителя.
	sub2 := Parse([]byte("RewriteEngine On\nRewriteOptions Inherit\nRewriteRule ^page$ page.html [L]\n"))
	e = Merge([]Dir{{URL: "/", Cfg: root}, {URL: "/docs/", Cfg: sub2}})
	if len(e.RewriteSets) != 2 {
		t.Fatalf("Inherit: ожидали 2 набора, получили %d", len(e.RewriteSets))
	}
}

func TestRedirects(t *testing.T) {
	cfg := Parse([]byte(`
Redirect 301 /old /new
Redirect /tmp https://example.org/tmp
Redirect gone /removed
RedirectMatch 302 ^/blog/(\d+)/(.*)$ /posts/$1/$2
RedirectPermanent /a /b
`))
	if len(cfg.Diags) != 0 {
		t.Fatalf("замечания: %+v", cfg.Diags)
	}
	e := Merge([]Dir{{URL: "/", Cfg: cfg}})
	for _, tc := range []struct {
		in     string
		status int
		target string
		ok     bool
	}{
		{"/old", 301, "/new", true},
		{"/old/page", 301, "/new/page", true},
		{"/older", 0, "", false}, // префикс только по границе сегмента
		{"/tmp/x", 302, "https://example.org/tmp/x", true},
		{"/removed", 410, "", true},
		{"/blog/12/hello", 302, "/posts/12/hello", true},
		{"/a", 301, "/b", true},
		{"/other", 0, "", false},
	} {
		st, target, ok := e.MatchRedirect(tc.in)
		if ok != tc.ok || st != tc.status || (ok && st != 410 && target != tc.target) {
			t.Errorf("%s → %d %q %v, ожидали %d %q %v", tc.in, st, target, ok, tc.status, tc.target, tc.ok)
		}
	}
}

func TestDiagnostics(t *testing.T) {
	src := `
# комментарий
DirectoryIndex home.html index.html
Options -Indexes +FollowSymLinks
php_value upload_max_filesize 64M
AddHandler application/x-httpd-php .php
AddType application/x-httpd-php .php
SetEnv FOO bar
Header set X-Frame-Options "DENY" env=secure
Header set Set-Cookie "a=b"
RequestHeader set X-Test 1
RewriteEngine On
RewriteRule ^(?!api)(.*)$ /index.html [L]
RewriteRule ^a$ /b [P]
RewriteCond %{SOMETHING_ODD} x
RewriteRule ^c$ /d [L]
<IfModule mod_php7.c>
php_flag display_errors On
</IfModule>
<IfModule !mod_rewrite.c>
Redirect / /no
</IfModule>
<Directory /var/www>
Options None
</Directory>
ErrorDocument 404 relative.html
ErrorDocument abc /x
<FilesMatch "(?=x)">
Header set X-A b
</FilesMatch>
Require ip 10.0.0.0/8
`
	cfg := Parse([]byte(src))
	got := map[string]string{}
	for _, d := range cfg.Diags {
		got[strings.ToLower(d.Directive)+"/"+d.Code] = d.Detail
	}
	for _, want := range []string{
		"php_value/unsupported", "addhandler/unsupported", "addtype/unsupported", "setenv/unsupported",
		"header/unsupported", "requestheader/unsupported", "rewriterule/regex", "rewriterule/unsupported",
		"rewritecond/unsupported", "<directory>/unsupported", "errordocument/syntax", "<filesmatch>/regex", "require/unsupported",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("нет замечания %s; есть: %v", want, got)
		}
	}
	// Блоки неизвестных модулей пропускаются молча; поддерживаемое работает.
	for _, d := range cfg.Diags {
		if strings.EqualFold(d.Directive, "php_flag") || strings.EqualFold(d.Directive, "Redirect") {
			t.Errorf("директива из пропущенного блока не должна попадать в разбор: %+v", d)
		}
	}
	if len(cfg.DirectoryIndex) != 2 || cfg.DirectoryIndex[0] != "home.html" {
		t.Errorf("DirectoryIndex: %v", cfg.DirectoryIndex)
	}
	if cfg.Indexes == nil || *cfg.Indexes {
		t.Error("Options -Indexes")
	}
	if len(cfg.Redirects) != 0 {
		t.Error("Redirect внутри <IfModule !mod_rewrite.c> должен быть пропущен: mod_rewrite «загружен»")
	}
	// Set-Cookie запрещён: иначе сайт пользователя мог бы подбросить cookie на весь родительский домен.
	for _, h := range cfg.Headers {
		if strings.EqualFold(h.Name, "Set-Cookie") {
			t.Error("Header set Set-Cookie не должен приниматься")
		}
	}
}

func TestLimits(t *testing.T) {
	big := Parse([]byte(strings.Repeat("# x\n", MaxSize)))
	if len(big.Diags) != 1 || big.Diags[0].Code != DiagTooBig {
		t.Fatalf("большой файл: %+v", big.Diags)
	}
	many := Parse([]byte(strings.Repeat("Header set X-A 1\n", maxDirectives+50)))
	found := false
	for _, d := range many.Diags {
		found = found || d.Code == DiagTooMany
	}
	if !found || len(many.Headers) > maxDirectives {
		t.Fatalf("лимит директив: %d заголовков, замечания %+v", len(many.Headers), many.Diags)
	}
}

func TestHeaders(t *testing.T) {
	cfg := Parse([]byte(`
Header set X-Frame-Options "SAMEORIGIN"
Header always set X-Always yes
Header append Cache-Control "no-transform"
Header merge Vary Accept-Encoding
Header unset X-Powered-By
Header add Link "<a>"
Header edit Server ^nginx apache
<FilesMatch "\.(js|css)$">
Header set Cache-Control "public, max-age=31536000, immutable"
</FilesMatch>
<Files "secret.txt">
Header set X-Secret 1
</Files>
`))
	if len(cfg.Diags) != 0 {
		t.Fatalf("замечания: %+v", cfg.Diags)
	}
	e := Merge([]Dir{{URL: "/", Cfg: cfg}})

	h := http.Header{}
	h.Set("X-Powered-By", "x")
	h.Set("Vary", "Accept-Encoding")
	h.Set("Server", "nginx")
	h.Set("Cache-Control", "max-age=1")
	e.ApplyHeaders(h, 200, "app.js")
	if h.Get("X-Frame-Options") != "SAMEORIGIN" || h.Get("X-Always") != "yes" || h.Get("X-Powered-By") != "" {
		t.Errorf("set/unset: %v", h)
	}
	if h.Get("Vary") != "Accept-Encoding" {
		t.Errorf("merge не должен дублировать значение: %q", h.Get("Vary"))
	}
	if h.Get("Server") != "apache" {
		t.Errorf("edit: %q", h.Get("Server"))
	}
	if h.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("FilesMatch перебивает общий append: %q", h.Get("Cache-Control"))
	}
	if h.Get("X-Secret") != "" {
		t.Error("Files не по имени не должен срабатывать")
	}

	// Ошибочные ответы: только Header always.
	h = http.Header{}
	e.ApplyHeaders(h, 404, "")
	if h.Get("X-Always") != "yes" || h.Get("X-Frame-Options") != "" {
		t.Errorf("на 404 должны действовать только Header always: %v", h)
	}
	h = http.Header{}
	e.ApplyHeaders(h, 200, "secret.txt")
	if h.Get("X-Secret") != "1" {
		t.Errorf("Files по имени: %v", h)
	}
}

func TestHeaderInjectionIsStripped(t *testing.T) {
	cfg := Parse([]byte("Header set X-A \"a\\r\\nSet-Cookie: x=1\"\nHeader set X-B \"b\"\n"))
	e := Merge([]Dir{{URL: "/", Cfg: cfg}})
	h := http.Header{}
	e.ApplyHeaders(h, 200, "a.html")
	for k, v := range h {
		for _, s := range v {
			if strings.ContainsAny(s, "\r\n") {
				t.Fatalf("перенос строки в заголовке %s: %q", k, s)
			}
		}
	}
}

func TestExpires(t *testing.T) {
	cfg := Parse([]byte(`
ExpiresActive On
ExpiresByType image/png "access plus 1 month"
ExpiresByType text/css "access plus 1 week 2 days"
ExpiresDefault "access plus 5 minutes"
`))
	if len(cfg.Diags) != 0 {
		t.Fatalf("%+v", cfg.Diags)
	}
	e := Merge([]Dir{{URL: "/", Cfg: cfg}})
	now := time.Now()
	for mime, want := range map[string]time.Duration{
		"image/png":                30 * 24 * time.Hour,
		"text/css":                 9 * 24 * time.Hour,
		"text/html":                5 * time.Minute,
		"IMAGE/PNG":                30 * 24 * time.Hour,
		"application/octet-stream": 5 * time.Minute,
	} {
		if d, ok := e.MaxAge(mime, now, now); !ok || d != want {
			t.Errorf("%s: %v %v, ожидали %v", mime, d, ok, want)
		}
	}
	off := Merge([]Dir{{URL: "/", Cfg: Parse([]byte("ExpiresByType image/png \"access plus 1 day\"\n"))}})
	if _, ok := off.MaxAge("image/png", now, now); ok {
		t.Error("без ExpiresActive On сроки не действуют")
	}
	for _, s := range []string{"access plus 1 year", "modification plus 2 hours", "A3600", "M60", "now plus 3 days 4 hours"} {
		if _, ok := ParseExpires(s); !ok {
			t.Errorf("ParseExpires(%q) должен разбираться", s)
		}
	}
	for _, s := range []string{"", "plus 1 day", "access 1 day", "access plus one day", "access plus 1", "access plus -1 day", "access plus 1 fortnight"} {
		if _, ok := ParseExpires(s); ok {
			t.Errorf("ParseExpires(%q) не должен разбираться", s)
		}
	}
	if d, _ := ParseExpires("access plus 999999 years"); d.Dur > 10*365*24*time.Hour {
		t.Error("срок должен быть ограничен, иначе переполнение Duration")
	}
}

func TestAccessAndAuthScopes(t *testing.T) {
	cfg := Parse([]byte(`
<Files ".env">
Require all denied
</Files>
<FilesMatch "\.(bak|sql)$">
Order allow,deny
Deny from all
</FilesMatch>
AuthType Basic
AuthName "Private area"
AuthUserFile /home/user/.htpasswd
<Files "admin.html">
Require valid-user
</Files>
`))
	if len(cfg.Diags) != 0 {
		t.Fatalf("%+v", cfg.Diags)
	}
	e := Merge([]Dir{{URL: "/", Cfg: cfg}})
	for name, want := range map[string]bool{".env": true, "dump.sql": true, "a.bak": true, "index.html": false} {
		if e.Denied(name) != want {
			t.Errorf("Denied(%q) = %v, ожидали %v", name, !want, want)
		}
	}
	if e.Auth("index.html") != nil {
		t.Error("Require только для admin.html")
	}
	a := e.Auth("admin.html")
	if a == nil || a.Realm != "Private area" || !a.Any || a.UserFile != "/.htpasswd" {
		t.Fatalf("Auth: %+v", a)
	}
	// Дочерний каталог переопределяет родительский.
	child := Parse([]byte("Require all granted\n<Files \".env\">\nRequire all granted\n</Files>\n"))
	e = Merge([]Dir{{URL: "/", Cfg: cfg}, {URL: "/sub/", Cfg: child}})
	if e.Denied(".env") {
		t.Error("дочерний .htaccess переопределяет запрет родителя")
	}
}

func TestMimeAndEncoding(t *testing.T) {
	cfg := Parse([]byte(`
AddType application/x-custom .cust
AddEncoding gzip .gz
AddDefaultCharset windows-1251
<Files "data.bin">
ForceType text/plain
</Files>
`))
	e := Merge([]Dir{{URL: "/", Cfg: cfg}})
	for name, want := range map[string]string{
		"a.html":     "text/html; charset=windows-1251",
		"a.PNG":      "image/png",
		"a.cust":     "application/x-custom",
		"data.bin":   "text/plain; charset=windows-1251",
		"noext":      "",
		"app.js.gz":  "application/javascript; charset=windows-1251",
		"font.woff2": "font/woff2",
	} {
		if m, _ := e.Mime(name); m != want {
			t.Errorf("Mime(%q) = %q, ожидали %q", name, m, want)
		}
	}
	if _, enc := e.Mime("app.js.gz"); enc != "gzip" {
		t.Errorf("AddEncoding: %q", enc)
	}
	def := Merge([]Dir{{URL: "/", Cfg: Parse(nil)}})
	if m, _ := def.Mime("x.html"); m != "text/html; charset=utf-8" {
		t.Errorf("по умолчанию UTF-8: %q", m)
	}
	off := Merge([]Dir{{URL: "/", Cfg: Parse([]byte("AddDefaultCharset Off\n"))}})
	if m, _ := off.Mime("x.html"); m != "text/html" {
		t.Errorf("AddDefaultCharset Off: %q", m)
	}
}

func TestErrorDocumentsAndDirectoryIndex(t *testing.T) {
	cfg := Parse([]byte("ErrorDocument 404 /404.html\nErrorDocument 403 \"Нельзя\"\nErrorDocument 500 https://example.org/oops\nDirectoryIndex home.html\nOptions +Indexes\n"))
	e := Merge([]Dir{{URL: "/", Cfg: cfg}})
	if d := e.ErrorDocs[404]; d.Kind != "path" || d.Value != "/404.html" {
		t.Errorf("404: %+v", d)
	}
	if d := e.ErrorDocs[403]; d.Kind != "text" || d.Value != "Нельзя" {
		t.Errorf("403: %+v", d)
	}
	if d := e.ErrorDocs[500]; d.Kind != "url" {
		t.Errorf("500: %+v", d)
	}
	if !e.Indexes || e.DirectoryIndex[0] != "home.html" {
		t.Errorf("index: %v %v", e.Indexes, e.DirectoryIndex)
	}
	off := Merge([]Dir{{URL: "/", Cfg: Parse([]byte("DirectoryIndex disabled\n"))}})
	if len(off.DirectoryIndex) != 0 {
		t.Errorf("DirectoryIndex disabled: %v", off.DirectoryIndex)
	}
}

func TestTokenizerAndContinuation(t *testing.T) {
	cfg := Parse([]byte("Header set X-Long \"a b\" \\\n   \nHeader\tset\tX-Tab\tv\n\xef\xbb\xbfHeader set X-Bom 1\r\nHeader set X-Q 'single quoted'\n"))
	got := map[string]string{}
	for _, h := range cfg.Headers {
		got[h.Name] = h.Value
	}
	if got["X-Long"] != "a b" || got["X-Tab"] != "v" || got["X-Q"] != "single quoted" {
		t.Errorf("%v (замечания %+v)", got, cfg.Diags)
	}
	if bad := Parse([]byte("Header set X-A \"unterminated\n")); len(bad.Diags) == 0 || bad.Diags[0].Code != DiagSyntax {
		t.Errorf("незакрытая кавычка: %+v", bad.Diags)
	}
	if unb := Parse([]byte("</Files>\n")); len(unb.Diags) == 0 {
		t.Error("закрывающий тег без открывающего")
	}
	if unc := Parse([]byte("<Files \"a\">\nHeader set X 1\n")); len(unc.Diags) == 0 {
		t.Error("незакрытый блок")
	}
}

// Разбор не должен падать ни на каком входе.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "RewriteEngine On\nRewriteRule ^(.*)$ /$1 [L]", "<Files \"a\">\nRequire all denied\n</Files>",
		"Header set A \"b", "<", "</>", "<<<>>>", "RewriteCond %{", "RewriteRule ( x", "ErrorDocument 404", "Redirect", "\\\n\\\n",
		"Header edit A ( b", "ExpiresByType", "AddType", "Options +", "\x00\xff", strings.Repeat("(", 5000),
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		cfg := Parse([]byte(s))
		e := Merge([]Dir{{URL: "/", Cfg: cfg}})
		env := site("index.html").env("a=b")
		st := State{Path: "/x/y", Query: "a=b"}
		for _, set := range e.RewriteSets {
			set.Apply(st, env)
		}
		e.ApplyHeaders(http.Header{}, 200, "f")
		e.Denied("f")
		e.Auth("f")
		e.Mime("f.js")
		e.MatchRedirect("/x")
	})
}

func TestHtpasswd(t *testing.T) {
	// Известный вектор из документации Apache: пароль "myPassword", соль "r31.....".
	const known = "$apr1$r31.....$HqJZimcKQFAMYayBlzkrA/"
	if !VerifyPassword(known, "myPassword") {
		t.Fatalf("apr1 не совпал с эталоном Apache: получили %s", apr1("myPassword", "r31....."))
	}
	if VerifyPassword(known, "mypassword") || VerifyPassword(known, "") {
		t.Error("неверный пароль принят")
	}
	bc, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(string(bc), "s3cret") || VerifyPassword(string(bc), "other") {
		t.Error("bcrypt")
	}
	y := "$2y$" + string(bc)[4:] // htpasswd -B и PHP выдают $2y$
	if !VerifyPassword(y, "s3cret") {
		t.Error("$2y$ должен читаться как $2a$")
	}
	if !VerifyPassword("{SHA}W6ph5Mm5Pz8GgiULbPgzG37mj9g=", "password") { // sha1("password")
		t.Error("{SHA}")
	}
	for _, unsupported := range []string{"plainpassword", "rl3s8gZ1yBnZ2", "", "$1$salt$hash", "$apr1$", "$apr1$x"} {
		if VerifyPassword(unsupported, "plainpassword") {
			t.Errorf("формат %q не должен приниматься", unsupported)
		}
	}
	m := ParseHtpasswd([]byte("# c\nalice:$apr1$a$b\r\n\nbob:{SHA}x\nbroken\n:nouser\n"))
	if len(m) != 2 || m["alice"] == "" || m["bob"] == "" {
		t.Errorf("ParseHtpasswd: %v", m)
	}
}

package webgw_test

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vladhost/internal/runtimecfg"
	"vladhost/internal/webgw"
)

// rtFixture — сайт со средой выполнения. «PHP» — настоящий FastCGI-сервер из стандартной библиотеки на unix-сокете пула.
type rtFixture struct {
	t    *testing.T
	base string
	pub  string
	sock string
	h    *webgw.Handler
}

func newRuntimeSite(t *testing.T, cfg runtimecfg.Config, files map[string]string) *rtFixture {
	t.Helper()
	base := t.TempDir()
	sockDir, err := os.MkdirTemp("", "vhphp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	pub := filepath.Join(base, host, "public")
	if err := os.MkdirAll(pub, 0o755); err != nil {
		t.Fatal(err)
	}
	f := &rtFixture{t: t, base: base, pub: pub, sock: cfg.Socket(sockDir),
		h: webgw.New(webgw.Options{Root: base, BaseDomain: "vladinc.ru", PHPSocketDir: sockDir})}
	for name, body := range files {
		p := filepath.Join(pub, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtimecfg.Save(filepath.Join(base, host), cfg); err != nil {
		t.Fatal(err)
	}
	return f
}

// phpFunc играет роль PHP: получает переменные и тело запроса, возвращает сырой CGI-ответ (заголовки, пустая строка, тело).
type phpFunc func(params map[string]string, body []byte) string

// servePHP запускает настоящий FastCGI-сервер на сокете пула: читает записи протокола, как это делает PHP-FPM.
func (f *rtFixture) servePHP(handler phpFunc) {
	f.t.Helper()
	l, err := net.Listen("unix", f.sock)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go serveFCGIConn(c, handler)
		}
	}()
}

func serveFCGIConn(c net.Conn, handler phpFunc) {
	defer func() { _ = c.Close() }()
	var params, stdin []byte
	for {
		var h [8]byte
		if _, err := io.ReadFull(c, h[:]); err != nil {
			return
		}
		n := int(h[4])<<8 | int(h[5])
		buf := make([]byte, n+int(h[6]))
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		buf = buf[:n]
		switch h[1] {
		case 4:
			params = append(params, buf...)
		case 5:
			if n == 0 {
				out := handler(decodeParams(params), stdin)
				writeRec(c, 6, []byte(out))
				writeRec(c, 6, nil)
				writeRec(c, 3, make([]byte, 8))
				return
			}
			stdin = append(stdin, buf...)
		}
	}
}

func writeRec(c net.Conn, typ byte, data []byte) {
	for {
		n := min(len(data), 60000)
		_, _ = c.Write([]byte{1, typ, 0, 1, byte(n >> 8), byte(n), 0, 0})
		_, _ = c.Write(data[:n])
		data = data[n:]
		if len(data) == 0 {
			return
		}
	}
}

func decodeParams(b []byte) map[string]string {
	out := map[string]string{}
	rl := func() int {
		if b[0]&0x80 == 0 {
			n := int(b[0])
			b = b[1:]
			return n
		}
		n := int(b[0]&0x7f)<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
		b = b[4:]
		return n
	}
	for len(b) > 0 {
		kl, vl := rl(), rl()
		out[string(b[:kl])] = string(b[kl : kl+vl])
		b = b[kl+vl:]
	}
	return out
}

func (f *rtFixture) do(method, target, body string, hdr ...string) resp {
	f.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	req.Host = host
	req.RemoteAddr = "203.0.113.7:5555"
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	return resp{w, w.Body.String()}
}

var phpCfg = runtimecfg.Config{Runtime: runtimecfg.PHP, ID: 7, Version: "8.3"}

// echoPHP отвечает переменными окружения запроса — так видно, что именно шлюз передал интерпретатору.
func echoPHP(p map[string]string, body []byte) string {
	var b strings.Builder
	b.WriteString("Content-Type: text/plain\r\n\r\n")
	for _, k := range []string{"SCRIPT_FILENAME", "SCRIPT_NAME", "REQUEST_URI", "QUERY_STRING", "PATH_INFO", "REQUEST_METHOD", "HTTPS", "REMOTE_ADDR",
		"HTTP_HOST", "HTTP_X_CUSTOM", "HTTP_PROXY", "DOCUMENT_ROOT", "CONTENT_TYPE", "CONTENT_LENGTH", "SERVER_NAME"} {
		fmt.Fprintf(&b, "%s=%s\n", k, p[k])
	}
	fmt.Fprintf(&b, "BODY=%s\n", body)
	return b.String()
}

func TestPHPScriptReceivesEnvironment(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{"index.php": "<?php echo 1;", "app/run.php": "<?php"})
	f.servePHP(echoPHP)
	r := f.do("GET", "/app/run.php?a=1&b=2", "", "X-Custom", "v", "Proxy", "evil:1")
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.body)
	}
	for _, want := range []string{
		"SCRIPT_FILENAME=" + filepath.Join(f.pub, "app", "run.php"), "SCRIPT_NAME=/app/run.php", "REQUEST_URI=/app/run.php?a=1&b=2",
		"QUERY_STRING=a=1&b=2", "REQUEST_METHOD=GET", "HTTPS=on", "REMOTE_ADDR=203.0.113.7", "HTTP_HOST=" + host, "HTTP_X_CUSTOM=v",
		"DOCUMENT_ROOT=" + f.pub, "SERVER_NAME=" + host, "HTTP_PROXY=\n",
	} {
		if !strings.Contains(r.body, want) {
			t.Errorf("нет %q в ответе:\n%s", want, r.body)
		}
	}
}

func TestPHPIndexOfDirectoryAndDefaultIndexOrder(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{"index.php": "x", "index.html": "static home", "blog/index.php": "x"})
	f.servePHP(echoPHP)
	if r := f.do("GET", "/", ""); !strings.Contains(r.body, "SCRIPT_NAME=/index.php") {
		t.Fatalf("index.php раньше index.html: %s", r.body)
	}
	if r := f.do("GET", "/blog/", ""); !strings.Contains(r.body, "SCRIPT_NAME=/blog/index.php") {
		t.Fatalf("каталог: %s", r.body)
	}
	if r := f.do("GET", "/blog", ""); r.Code != 301 {
		t.Fatalf("каталог без слэша: %d", r.Code)
	}
}

func TestPHPPostBodyAndAnyMethod(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{"form.php": "x"})
	f.servePHP(echoPHP)
	r := f.do("POST", "/form.php", "name=vlad&x=1", "Content-Type", "application/x-www-form-urlencoded")
	for _, want := range []string{"REQUEST_METHOD=POST", "BODY=name=vlad&x=1", "CONTENT_TYPE=application/x-www-form-urlencoded", "CONTENT_LENGTH=13"} {
		if !strings.Contains(r.body, want) {
			t.Errorf("нет %q:\n%s", want, r.body)
		}
	}
	for _, m := range []string{"PUT", "DELETE", "PATCH"} {
		if r := f.do(m, "/form.php", "z"); !strings.Contains(r.body, "REQUEST_METHOD="+m) {
			t.Errorf("%s: %s", m, r.body)
		}
	}
}

func TestPHPPathInfo(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{"index.php": "x", "sub/api.php": "x"})
	f.servePHP(echoPHP)
	r := f.do("GET", "/index.php/users/42?x=1", "")
	if !strings.Contains(r.body, "SCRIPT_NAME=/index.php") || !strings.Contains(r.body, "PATH_INFO=/users/42") || !strings.Contains(r.body, "REQUEST_URI=/index.php/users/42?x=1") {
		t.Fatalf("%s", r.body)
	}
	if r := f.do("GET", "/sub/api.php/a/b", ""); !strings.Contains(r.body, "SCRIPT_NAME=/sub/api.php") || !strings.Contains(r.body, "PATH_INFO=/a/b") {
		t.Fatalf("%s", r.body)
	}
	// Скрипта нет — обычный 404, а не выполнение чего-то другого.
	if r := f.do("GET", "/missing.php/x", ""); r.Code != 404 {
		t.Fatalf("%d", r.Code)
	}
	if r := f.do("GET", "/nodir/index.php", ""); r.Code != 404 {
		t.Fatalf("%d", r.Code)
	}
}

func TestPHPHiddenAndProtectedScriptsAreNotRun(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{".hidden.php": "x", ".git/x.php": "x", "secret.php": "x", ".htaccess": "<Files \"secret.php\">\nRequire all denied\n</Files>\n"})
	f.servePHP(func(map[string]string, []byte) string { return "\r\nEXECUTED" })
	for _, p := range []string{"/.hidden.php", "/.git/x.php", "/secret.php"} {
		if r := f.do("GET", p, ""); r.Code == 200 || strings.Contains(r.body, "EXECUTED") {
			t.Errorf("%s: %d %s", p, r.Code, r.body)
		}
	}
}

func TestPHPResponseHeadersStatusAndCookies(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{"a.php": "x", "b.php": "x", "c.php": "x"})
	f.servePHP(func(p map[string]string, _ []byte) string {
		switch p["SCRIPT_NAME"] {
		case "/a.php":
			return "Status: 404 Not Found\r\nSet-Cookie: a=1; Path=/\r\nSet-Cookie: b=2; Path=/\r\nX-Powered-By: test\r\n\r\ncustom 404"
		case "/b.php":
			return "Location: /elsewhere\r\n\r\n"
		}
		return "Content-Type: application/json\r\n\r\n{\"ok\":true}"
	})
	a := f.do("GET", "/a.php", "")
	if a.Code != 404 || a.body != "custom 404" || len(a.Header().Values("Set-Cookie")) != 2 || a.Header().Get("X-Powered-By") != "test" {
		t.Fatalf("%d %q %v", a.Code, a.body, a.Header())
	}
	if b := f.do("GET", "/b.php", ""); b.Code != 302 || b.Header().Get("Location") != "/elsewhere" {
		t.Fatalf("%d %v", b.Code, b.Header())
	}
	if c := f.do("GET", "/c.php", ""); c.Code != 200 || c.Header().Get("Content-Type") != "application/json" || c.body != `{"ok":true}` {
		t.Fatalf("%d %q", c.Code, c.body)
	}
	if h := f.do("HEAD", "/c.php", ""); h.Code != 200 || h.body != "" {
		t.Fatalf("HEAD: %d %q", h.Code, h.body)
	}
}

func TestPHPPoolDownGivesServiceUnavailableAndNeverSource(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{"index.php": "<?php echo 'SECRET_SOURCE';"})
	r := f.do("GET", "/index.php", "")
	if r.Code != 503 || strings.Contains(r.body, "SECRET_SOURCE") {
		t.Fatalf("%d %s", r.Code, r.body)
	}
	if r := f.do("GET", "/", ""); r.Code != 503 || strings.Contains(r.body, "SECRET_SOURCE") {
		t.Fatalf("%d %s", r.Code, r.body)
	}
}

func TestPHPStaticFilesAreStillServedByGateway(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{"style.css": "body{}", "index.php": "x"})
	f.servePHP(func(map[string]string, []byte) string { return "\r\nEXECUTED" })
	if r := f.do("GET", "/style.css", ""); r.Code != 200 || r.body != "body{}" || !strings.HasPrefix(r.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("%d %q", r.Code, r.body)
	}
}

func TestPHPFrontControllerThroughHtaccess(t *testing.T) {
	ht := "RewriteEngine On\nRewriteCond %{REQUEST_FILENAME} !-f\nRewriteCond %{REQUEST_FILENAME} !-d\nRewriteRule . /index.php [L]\n"
	f := newRuntimeSite(t, phpCfg, map[string]string{"index.php": "x", ".htaccess": ht, "real.css": "css"})
	f.servePHP(echoPHP)
	r := f.do("GET", "/blog/hello-world/?utm=1", "")
	if r.Code != 200 || !strings.Contains(r.body, "SCRIPT_NAME=/index.php") || !strings.Contains(r.body, "REQUEST_URI=/blog/hello-world/?utm=1") {
		t.Fatalf("%d %s", r.Code, r.body)
	}
	if r := f.do("GET", "/real.css", ""); r.body != "css" {
		t.Fatalf("%q", r.body)
	}
}

func TestBrokenRuntimeFileMeansStatic(t *testing.T) {
	f := newRuntimeSite(t, phpCfg, map[string]string{"index.html": "home"})
	site := filepath.Join(f.base, host)
	for _, raw := range []string{"{", `{"runtime":"php","id":0,"version":"8.3"}`, `{"runtime":"php","id":1,"version":"../../etc"}`,
		`{"runtime":"node","id":1,"port":22}`, `{"runtime":"bash","id":1}`, ""} {
		if err := os.WriteFile(filepath.Join(site, runtimecfg.FileName), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if r := f.do("GET", "/", ""); r.body != "home" {
			t.Errorf("%q: %d %q", raw, r.Code, r.body)
		}
		if r := f.do("POST", "/", ""); r.Code != 405 {
			t.Errorf("%q: статика не принимает POST, получено %d", raw, r.Code)
		}
	}
}

func TestStaticSiteStillRefusesPost(t *testing.T) {
	s := newSite(t, map[string]string{"index.html": "x"})
	if r := s.do("POST", "/"); r.Code != 405 {
		t.Fatalf("%d", r.Code)
	}
}

// freePort ищет свободный порт в диапазоне приложений.
func freePort(t *testing.T) int {
	t.Helper()
	for p := runtimecfg.PortMin; p <= runtimecfg.PortMax; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			_ = l.Close()
			return p
		}
	}
	t.Skip("нет свободного порта в диапазоне приложений")
	return 0
}

func startApp(t *testing.T, h http.Handler) int {
	t.Helper()
	port := freePort(t)
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	return port
}

func TestNodeAppIsProxied(t *testing.T) {
	port := startApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = fmt.Fprintf(w, "%s %s host=%s xff=%s proto=%s body=%s", r.Method, r.URL.RequestURI(), r.Host, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Forwarded-Proto"), b)
	}))
	f := newRuntimeSite(t, runtimecfg.Config{Runtime: runtimecfg.Node, ID: 3, Port: port}, map[string]string{"index.html": "STATIC MUST NOT WIN"})
	r := f.do("POST", "/api/items?x=1", "payload", "X-Forwarded-For", "6.6.6.6")
	if r.Code != 200 || !strings.Contains(r.body, "POST /api/items?x=1") || !strings.Contains(r.body, "host="+host) ||
		!strings.Contains(r.body, "xff=203.0.113.7") || !strings.Contains(r.body, "proto=https") || !strings.Contains(r.body, "body=payload") {
		t.Fatalf("%d %s", r.Code, r.body)
	}
	if r := f.do("GET", "/", ""); strings.Contains(r.body, "STATIC") {
		t.Fatalf("приложение отвечает за все адреса: %s", r.body)
	}
	if r := f.do("PUT", "/x", "1"); r.Code != 200 {
		t.Fatalf("методы не ограничены: %d", r.Code)
	}
}

func TestAppDownGivesServiceUnavailable(t *testing.T) {
	f := newRuntimeSite(t, runtimecfg.Config{Runtime: runtimecfg.Python, ID: 4, Port: freePort(t)}, nil)
	if r := f.do("GET", "/", ""); r.Code != 503 {
		t.Fatalf("%d %s", r.Code, r.body)
	}
}

// Приложения открывают WebSocket: шлюз обязан пропускать смену протокола, а не ломать её.
func TestAppUpgradeWorksThroughGateway(t *testing.T) {
	port := startApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "echo" {
			http.Error(w, "no", 400)
			return
		}
		c, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
		_ = rw.Flush()
		line, _ := rw.ReadString('\n')
		_, _ = rw.WriteString("echo:" + line)
		_ = rw.Flush()
	}))
	f := newRuntimeSite(t, runtimecfg.Config{Runtime: runtimecfg.Node, ID: 5, Port: port}, nil)
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(srv.URL, "http://"), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n", host)
	br := bufio.NewReader(conn)
	status, _ := br.ReadString('\n')
	if !strings.Contains(status, "101") {
		t.Fatalf("статус: %q", status)
	}
	for {
		l, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if l == "\r\n" {
			break
		}
	}
	_, _ = fmt.Fprint(conn, "hello\n")
	got, _ := br.ReadString('\n')
	if got != "echo:hello\n" {
		t.Fatalf("после смены протокола: %q", got)
	}
}

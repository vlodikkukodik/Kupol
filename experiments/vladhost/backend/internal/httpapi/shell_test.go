package httpapi_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/runtimes"
	"vladhost/internal/shellaccess"
	"vladhost/internal/shellbroker"
	"vladhost/internal/shellclient"
)

// shellHelper играет исполнителя сред (runtime.sh): при включении доступа ставит метку, при выключении снимает.
type shellHelper struct {
	dir   string
	mu    sync.Mutex
	calls []string
}

func (h *shellHelper) Do(_ context.Context, r runtimes.Request, _ time.Duration) (runtimes.Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, r.Action)
	mark := filepath.Join(h.dir, "shell", fmt.Sprint(r.ID))
	switch r.Action {
	case runtimes.ActionShellOn:
		_ = os.MkdirAll(filepath.Join(h.dir, "sites-root", r.Host, "public"), 0o755)
		_ = os.MkdirAll(filepath.Join(h.dir, "sites-root", r.Host, "tmp"), 0o755)
		_ = os.WriteFile(mark, []byte(r.Host), 0o644)
	case runtimes.ActionShellOff:
		_ = os.Remove(mark)
	}
	return runtimes.Result{OK: true}, nil
}

type keyView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
}

type shellView struct {
	Enabled     bool   `json:"enabled"`
	Login       string `json:"login"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Fingerprint string `json:"host_fingerprint"`
	Keys        int    `json:"keys"`
}

// withShell поднимает настоящий посредник оболочек (с подменой systemd-run) и включает раздел.
func (e *env) withShell(panelOrigin string) (*shellaccess.Service, string) {
	e.t.Helper()
	dir, err := os.MkdirTemp("", "vhweb")
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sitesRoot := filepath.Join(dir, "sites-root")
	_ = os.MkdirAll(filepath.Join(dir, "shell"), 0o755)
	script := filepath.Join(dir, "systemd-run")
	_ = os.WriteFile(script, []byte("#!/bin/bash\nwhile [ \"${1:-}\" != \"--\" ]; do shift; done\nshift\nexec \"$@\"\n"), 0o755)
	stub := filepath.Join(dir, "systemctl")
	_ = os.WriteFile(stub, []byte("#!/bin/bash\n"), 0o755)
	broker := shellbroker.New(shellbroker.Config{Dir: dir, Sites: sitesRoot, PanelUID: os.Getuid(), SystemdRun: script, Systemctl: stub})
	sock := filepath.Join(dir, "b.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = l.Close() })
	go func() { _ = broker.Serve(l) }()

	client := shellclient.Client{Socket: sock}
	svc := shellaccess.New(e.db, e.sites, shellaccess.Config{Applier: &shellHelper{dir: dir}, Broker: client, BaseDomain: "vladinc.ru",
		SSHHost: "ssh.example.test", SSHPort: 2222, HostFingerprint: func() string { return "SHA256:hostkey" }})
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000, PanelOrigin: panelOrigin},
		httpapi.WithShell(svc, client))
	return svc, dir
}

func genKey(t *testing.T) string {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, _ := ssh.NewPublicKey(pub)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k))) + " me@laptop"
}

func TestShellHiddenWhenNotConfigured(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	site, _ := e.createSite(tok, "blog")
	for _, tc := range [][2]string{{"GET", "/api/ssh/keys"}, {"POST", "/api/ssh/keys"}, {"POST", "/api/ssh/keys/generate"}, {"DELETE", "/api/ssh/keys/1"},
		{"GET", fmt.Sprintf("/api/sites/%d/shell", site)}, {"PUT", fmt.Sprintf("/api/sites/%d/shell", site)}, {"POST", fmt.Sprintf("/api/sites/%d/terminal", site)}, {"GET", "/api/terminal/ws?ticket=x"}} {
		if w := e.do(tc[0], tc[1], map[string]any{}, tok); w.Code != 404 {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	if list := decode[struct {
		ShellAvailable bool `json:"shell_available"`
	}](t, e.do("GET", "/api/sites", nil, tok)); list.ShellAvailable {
		t.Fatal("раздел не должен быть доступен")
	}
}

func TestSSHKeysThroughAPI(t *testing.T) {
	e := newEnv(t)
	e.withShell("")
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	if w := e.do("GET", "/api/ssh/keys", nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
	empty := decode[struct {
		Keys    []keyView `json:"keys"`
		Max     int       `json:"max_keys"`
		HostFPR string    `json:"host_fingerprint"`
	}](t, e.do("GET", "/api/ssh/keys", nil, john))
	if empty.Keys == nil || len(empty.Keys) != 0 || empty.Max != 10 {
		t.Fatalf("%+v", empty)
	}

	// Ошибки — на двух языках, с привязкой к полю
	w := e.doLang("ru", "POST", "/api/ssh/keys", map[string]string{"name": "ноутбук", "public_key": "не ключ"}, john)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Не удалось разобрать ключ") || !strings.Contains(w.Body.String(), `"field":"public_key"`) {
		t.Fatalf("ru: %d %s", w.Code, w.Body)
	}
	if w := e.doLang("it", "POST", "/api/ssh/keys", map[string]string{"name": "x", "public_key": "non è una chiave"}, john); w.Code != 422 || !strings.Contains(w.Body.String(), "Impossibile leggere la chiave") {
		t.Fatalf("it: %d %s", w.Code, w.Body)
	}

	line := genKey(t)
	w = e.do("POST", "/api/ssh/keys", map[string]string{"name": "ноутбук", "public_key": line}, john)
	added := decode[struct{ Key keyView }](t, w).Key
	if w.Code != 201 || added.Name != "ноутбук" || added.Algorithm != "ssh-ed25519" || !strings.HasPrefix(added.Fingerprint, "SHA256:") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/ssh/keys", map[string]string{"name": "копия", "public_key": line}, mary); w.Code != 409 || !strings.Contains(w.Body.String(), "ssh_key_taken") {
		t.Fatalf("тот же ключ у другого аккаунта: %d %s", w.Code, w.Body)
	}

	// Генерация: закрытый ключ приходит один раз
	w = e.do("POST", "/api/ssh/keys/generate", map[string]string{"name": "из панели"}, john)
	gen := decode[struct {
		Key        keyView `json:"key"`
		PrivateKey string  `json:"private_key"`
	}](t, w)
	if w.Code != 201 || !strings.HasPrefix(gen.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	signer, err := ssh.ParsePrivateKey([]byte(gen.PrivateKey))
	if err != nil || ssh.FingerprintSHA256(signer.PublicKey()) != gen.Key.Fingerprint {
		t.Fatalf("закрытый ключ не соответствует: %v", err)
	}
	list := e.do("GET", "/api/ssh/keys", nil, john)
	if strings.Contains(list.Body.String(), "PRIVATE") || len(decode[struct{ Keys []keyView }](t, list).Keys) != 2 {
		t.Fatalf("%s", list.Body)
	}
	// Чужой ключ не удалить, свой — можно
	if w := e.do("DELETE", fmt.Sprintf("/api/ssh/keys/%d", added.ID), nil, mary); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/ssh/keys/%d", added.ID), nil, john); w.Code != 204 {
		t.Fatalf("%d", w.Code)
	}
	if got := decode[struct{ Keys []keyView }](t, e.do("GET", "/api/ssh/keys", nil, mary)).Keys; len(got) != 0 {
		t.Fatalf("список чужих ключей: %+v", got)
	}
}

func TestShellToggleStatusAndOwnership(t *testing.T) {
	e := newEnv(t)
	e.withShell("")
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	site, host := e.createSite(john, "blog")
	base := fmt.Sprintf("/api/sites/%d/shell", site)
	if list := decode[struct {
		ShellAvailable bool `json:"shell_available"`
	}](t, e.do("GET", "/api/sites", nil, john)); !list.ShellAvailable {
		t.Fatal("раздел должен быть доступен")
	}
	st := decode[struct{ Shell shellView }](t, e.do("GET", base, nil, john)).Shell
	if st.Enabled || st.Login != "blog.john" || st.Host != "ssh.example.test" || st.Port != 2222 || st.Fingerprint != "SHA256:hostkey" || st.Keys != 0 {
		t.Fatalf("%+v", st)
	}
	_ = host
	// Терминал закрыт, пока доступ не включён
	if w := e.do("POST", fmt.Sprintf("/api/sites/%d/terminal", site), nil, john); w.Code != 409 || !strings.Contains(w.Body.String(), "shell_disabled") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", base, map[string]bool{"enabled": true}, john); w.Code != 200 || !decode[struct{ Shell shellView }](t, w).Shell.Enabled {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", base, map[string]bool{"enabled": false}, john); w.Code != 200 || decode[struct{ Shell shellView }](t, w).Shell.Enabled {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	for _, tc := range [][2]string{{"GET", base}, {"PUT", base}, {"POST", fmt.Sprintf("/api/sites/%d/terminal", site)}} {
		if w := e.do(tc[0], tc[1], map[string]bool{"enabled": true}, mary); w.Code != 404 {
			t.Errorf("чужой %s %s: %d", tc[0], tc[1], w.Code)
		}
	}
}

func (e *env) enableShell(tok string, site int64) {
	e.t.Helper()
	if w := e.do("PUT", fmt.Sprintf("/api/sites/%d/shell", site), map[string]bool{"enabled": true}, tok); w.Code != 200 {
		e.t.Fatalf("включение: %d %s", w.Code, w.Body)
	}
}

func (e *env) ticket(tok string, site int64) string {
	e.t.Helper()
	w := e.do("POST", fmt.Sprintf("/api/sites/%d/terminal", site), nil, tok)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		e.t.Fatalf("билет: %d %s", w.Code, w.Body)
	}
	return decode[struct{ Ticket string }](e.t, w).Ticket
}

func dialWS(t *testing.T, srv *httptest.Server, ticket string, origin string) (*websocket.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/terminal/ws?ticket="+ticket, &websocket.DialOptions{HTTPHeader: h})
	return c, err
}

// readUntil читает данные терминала, пока не встретится текст, и возвращает всё прочитанное и управляющие кадры.
func readUntil(t *testing.T, c *websocket.Conn, want string, ctlWant string) (string, []termCtl) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out strings.Builder
	var ctls []termCtl
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			return out.String(), ctls
		}
		if typ == websocket.MessageText {
			var ctl termCtl
			_ = json.Unmarshal(data, &ctl)
			ctls = append(ctls, ctl)
			if ctlWant != "" && ctl.Type == ctlWant {
				return out.String(), ctls
			}
			continue
		}
		out.Write(data)
		if want != "" && strings.Contains(out.String(), want) {
			return out.String(), ctls
		}
	}
}

type termCtl struct {
	Type  string `json:"type"`
	Code  int    `json:"code"`
	Error string `json:"error"`
}

func TestWebTerminalSession(t *testing.T) {
	e := newEnv(t)
	e.withShell("")
	adm, _ := e.admin()
	john := e.user(adm, "john")
	site, _ := e.createSite(john, "blog")
	e.enableShell(john, site)
	srv := httptest.NewServer(e.r)
	defer srv.Close()

	tk := e.ticket(john, site)
	c, err := dialWS(t, srv, tk, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	ctx := context.Background()
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"resize","cols":120,"rows":40}`))
	time.Sleep(200 * time.Millisecond)
	_ = c.Write(ctx, websocket.MessageBinary, []byte("echo hello-web; stty size; exit 2\n"))
	out, ctls := readUntil(t, c, "", "exit")
	if !strings.Contains(out, "hello-web") || !strings.Contains(out, "40 120") {
		t.Fatalf("вывод терминала: %q", out)
	}
	if len(ctls) == 0 || ctls[len(ctls)-1].Type != "exit" || ctls[len(ctls)-1].Code != 2 {
		t.Fatalf("итог: %+v", ctls)
	}

	// Билет одноразовый
	if _, err := dialWS(t, srv, tk, ""); err == nil {
		t.Fatal("билет должен сгореть после первого использования")
	}
	if _, err := dialWS(t, srv, "выдуманный", ""); err == nil {
		t.Fatal("чужой билет")
	}
}

func TestWebTerminalTicketIsBoundToOwnerAndOrigin(t *testing.T) {
	e := newEnv(t)
	e.withShell("https://app.example.test")
	adm, _ := e.admin()
	john := e.user(adm, "john")
	site, _ := e.createSite(john, "blog")
	e.enableShell(john, site)
	srv := httptest.NewServer(e.r)
	defer srv.Close()
	// Подключение со страницы другого сайта (например, с поддомена пользователя) отклоняется
	if c, err := dialWS(t, srv, e.ticket(john, site), "https://evil.vladinc.ru"); err == nil {
		_ = c.CloseNow()
		t.Fatal("чужой Origin должен отклоняться")
	}
	c, err := dialWS(t, srv, e.ticket(john, site), "https://app.example.test")
	if err != nil {
		t.Fatalf("Origin панели: %v", err)
	}
	_ = c.CloseNow()
}

func TestDisablingClosesOpenTerminal(t *testing.T) {
	e := newEnv(t)
	e.withShell("")
	adm, _ := e.admin()
	john := e.user(adm, "john")
	site, _ := e.createSite(john, "blog")
	e.enableShell(john, site)
	srv := httptest.NewServer(e.r)
	defer srv.Close()
	c, err := dialWS(t, srv, e.ticket(john, site), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	_ = c.Write(context.Background(), websocket.MessageBinary, []byte("echo ready; sleep 60\n"))
	readUntil(t, c, "ready", "")
	if w := e.do("PUT", fmt.Sprintf("/api/sites/%d/shell", site), map[string]bool{"enabled": false}, john); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	done := make(chan struct{})
	go func() {
		readUntil(t, c, "", "exit")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("после отключения доступа открытый терминал должен закрыться")
	}
	// и новый билет уже не выдаётся
	if w := e.do("POST", fmt.Sprintf("/api/sites/%d/terminal", site), nil, john); w.Code != 409 {
		t.Fatalf("%d", w.Code)
	}
}

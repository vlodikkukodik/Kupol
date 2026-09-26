package httpapi_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"vladhost/internal/config"
	"vladhost/internal/httpapi"
	"vladhost/internal/runtimes"
)

// stubApplier играет исполнителя сред: запоминает заявки, отвечает «успешно».
type stubApplier struct {
	mu    sync.Mutex
	calls []runtimes.Request
	fail  bool
}

func (s *stubApplier) Do(_ context.Context, r runtimes.Request, _ time.Duration) (runtimes.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r)
	if s.fail && r.Action == runtimes.ActionApply {
		return runtimes.Result{Error: "pool"}, nil
	}
	return runtimes.Result{OK: true, State: "active", Output: "app started\nlistening"}, nil
}

type rtView struct {
	Runtime string `json:"runtime"`
	Version string `json:"version"`
	Command string `json:"command"`
	Port    int    `json:"port"`
	State   string `json:"state"`
	Caps    struct {
		PHP    []string `json:"php"`
		Node   string   `json:"node"`
		Python string   `json:"python"`
	} `json:"caps"`
}

func (e *env) withRuntimes(caps string) *stubApplier {
	e.t.Helper()
	dir := e.t.TempDir()
	if caps != "" {
		if err := os.WriteFile(filepath.Join(dir, "caps"), []byte(caps), 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
	stub := &stubApplier{}
	svc := runtimes.New(e.db, e.sites, stub, dir)
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000},
		httpapi.WithRuntimes(svc))
	return stub
}

func TestRuntimeHiddenWhenNothingInstalled(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	site, _ := e.createSite(tok, "blog")
	for _, tc := range [][2]string{{"GET", ""}, {"PUT", ""}, {"POST", "/restart"}, {"GET", "/logs"}} {
		if w := e.do(tc[0], fmt.Sprintf("/api/sites/%d/runtime%s", site, tc[1]), map[string]string{"runtime": "php"}, tok); w.Code != 404 {
			t.Errorf("%s %s: %d", tc[0], tc[1], w.Code)
		}
	}
	e.withRuntimes("") // файл caps пуст: сред нет
	if w := e.do("GET", fmt.Sprintf("/api/sites/%d/runtime", site), nil, tok); w.Code != 404 {
		t.Fatalf("без сред: %d", w.Code)
	}
	if list := decode[struct {
		RuntimeAvailable bool `json:"runtime_available"`
	}](t, e.do("GET", "/api/sites", nil, tok)); list.RuntimeAvailable {
		t.Fatal("в списке сайтов среды помечены недоступными")
	}
}

func TestRuntimeThroughAPI(t *testing.T) {
	e := newEnv(t)
	stub := e.withRuntimes("php=8.2,8.3\nnode=22.1.0\n")
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	site, _ := e.createSite(john, "blog")
	path := fmt.Sprintf("/api/sites/%d/runtime", site)

	if w := e.do("GET", path, nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}
	if list := decode[struct {
		RuntimeAvailable bool `json:"runtime_available"`
	}](t, e.do("GET", "/api/sites", nil, john)); !list.RuntimeAvailable {
		t.Fatal("среды должны быть доступны")
	}
	st := decode[struct{ Runtime rtView }](t, e.do("GET", path, nil, john)).Runtime
	if st.Runtime != "static" || len(st.Caps.PHP) != 2 || st.Caps.Node != "22.1.0" || st.Caps.Python != "" {
		t.Fatalf("исходное состояние: %+v", st)
	}

	// PHP
	w := e.do("PUT", path, map[string]string{"runtime": "php", "version": "8.3"}, john)
	php := decode[struct{ Runtime rtView }](t, w).Runtime
	if w.Code != 200 || php.Runtime != "php" || php.Version != "8.3" || php.State != "active" {
		t.Fatalf("PHP: %d %s", w.Code, w.Body)
	}
	// Node.js: порт выдаёт сервер
	w = e.do("PUT", path, map[string]string{"runtime": "node", "command": "node server.js"}, john)
	node := decode[struct{ Runtime rtView }](t, w).Runtime
	if w.Code != 200 || node.Runtime != "node" || node.Port < 20000 || node.Port > 29999 || node.Command != "node server.js" {
		t.Fatalf("Node: %d %s", w.Code, w.Body)
	}

	// Ошибки: на двух языках, с привязкой к полю
	w = e.doLang("ru", "PUT", path, map[string]string{"runtime": "php", "version": "7.0"}, john)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "Такой версии PHP на сервере нет") || !strings.Contains(w.Body.String(), `"field":"version"`) {
		t.Fatalf("версия ru: %d %s", w.Code, w.Body)
	}
	w = e.doLang("it", "PUT", path, map[string]string{"runtime": "python", "command": "python app.py"}, john)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "non è installato") {
		t.Fatalf("python it: %d %s", w.Code, w.Body)
	}
	if w := e.do("PUT", path, map[string]string{"runtime": "node", "command": "a\nb"}, john); w.Code != 422 {
		t.Fatalf("команда с переводом строки: %d", w.Code)
	}

	// Журнал и перезапуск
	logs := decode[struct{ Logs string }](t, e.do("GET", path+"/logs", nil, john)).Logs
	if logs != "app started\nlistening" {
		t.Fatalf("журнал: %q", logs)
	}
	if w := e.do("POST", path+"/restart", nil, john); w.Code != 200 {
		t.Fatalf("перезапуск: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", path+"/restart", nil, john); w.Code != 429 {
		t.Fatalf("частый перезапуск: %d", w.Code)
	}

	// Чужой сайт недоступен во всех операциях
	for _, tc := range [][2]string{{"GET", ""}, {"PUT", ""}, {"POST", "/restart"}, {"GET", "/logs"}} {
		if w := e.do(tc[0], path+tc[1], map[string]string{"runtime": "php", "version": "8.3"}, mary); w.Code != 404 {
			t.Errorf("чужой %s %s: %d", tc[0], tc[1], w.Code)
		}
	}

	// Отказ исполнителя не оставляет следов
	stub.fail = true
	e.do("PUT", path, map[string]string{"runtime": "static"}, john)
	stub.fail = true
	w = e.do("PUT", path, map[string]string{"runtime": "php", "version": "8.2"}, john)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "rt_apply_failed") {
		t.Fatalf("отказ исполнителя: %d %s", w.Code, w.Body)
	}
	st = decode[struct{ Runtime rtView }](t, e.do("GET", path, nil, john)).Runtime
	if st.Runtime != "static" {
		t.Fatalf("после отказа сайт остаётся статикой: %+v", st)
	}
}

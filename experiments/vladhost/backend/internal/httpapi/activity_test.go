package httpapi_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"vladhost/internal/activity"
	"vladhost/internal/config"
	"vladhost/internal/httpapi"
)

type activityBody struct {
	Events []struct {
		ID        int64  `json:"id"`
		Kind      string `json:"kind"`
		Category  string `json:"category"`
		Target    string `json:"target"`
		Count     int    `json:"count"`
		IP        string `json:"ip"`
		UserAgent string `json:"user_agent"`
	} `json:"events"`
	Next       int64    `json:"next"`
	Categories []string `json:"categories"`
}

func (e *env) withActivity() {
	e.t.Helper()
	e.r = httpapi.New(e.svc, e.sites, config.Config{JWTSecret: []byte(strings.Repeat("s", 32)), AuthPerMinute: 10000, AuthBurst: 10000}, httpapi.WithActivity(activity.New(e.db)))
}

func (e *env) activity(tok, query string) activityBody {
	e.t.Helper()
	w := e.do("GET", "/api/activity"+query, nil, tok)
	if w.Code != 200 {
		e.t.Fatalf("журнал: %d %s", w.Code, w.Body)
	}
	return decode[activityBody](e.t, w)
}

func kinds(b activityBody) []string {
	var out []string
	for _, ev := range b.Events {
		out = append(out, ev.Kind)
	}
	return out
}

func TestActivityHiddenWhenNotConfigured(t *testing.T) {
	e := newEnv(t)
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	if w := e.do("GET", "/api/activity", nil, tok); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
	// без журнала всё остальное работает
	if _, host := e.createSite(tok, "blog"); host == "" {
		t.Fatal("сайт не создан")
	}
}

func TestActivityRecordsAccountActions(t *testing.T) {
	e := newEnv(t)
	e.withActivity()
	adm, _ := e.admin()
	john, mary := e.user(adm, "john"), e.user(adm, "mary")
	if w := e.do("GET", "/api/activity", nil, ""); w.Code != 401 {
		t.Fatalf("без входа: %d", w.Code)
	}

	// вход и неудачные попытки: пишутся тому, чей аккаунт пробовали открыть; ответ для несуществующего аккаунта такой же
	if code, _, _ := e.login("john", "password123"); code != 200 {
		t.Fatalf("вход: %d", code)
	}
	for range 3 {
		if code, _, _ := e.login("john", "wrong-password"); code != 401 {
			t.Fatalf("неверный пароль: %d", code)
		}
	}
	wrong := e.do("POST", "/api/auth/login", map[string]string{"login": "john", "password": "x"}, "")
	ghost := e.do("POST", "/api/auth/login", map[string]string{"login": "no-such-user", "password": "x"}, "")
	if wrong.Code != ghost.Code || wrong.Body.String() != ghost.Body.String() {
		t.Fatalf("ответы различаются: %d %s / %d %s", wrong.Code, wrong.Body, ghost.Code, ghost.Body)
	}

	// действия над сайтом и файлами
	id, host := e.createSite(john, "blog")
	for i := range 4 {
		if c := e.put(id, john, fmt.Sprintf("p%d.html", i), "x"); c != 204 {
			t.Fatalf("файл: %d", c)
		}
	}
	if w := e.do("POST", "/api/sites", map[string]string{"slug": "second"}, john); w.Code < 400 {
		t.Fatalf("второй сайт по умолчанию нельзя: %d", w.Code)
	}
	if w := e.do("PUT", fmt.Sprintf("/api/sites/%d/file?path=../../etc/passwd", id), map[string]string{"content": "x"}, john); w.Code < 400 {
		t.Fatalf("выход за папку сайта должен отклоняться: %d", w.Code)
	}
	if w := e.do("PATCH", "/api/me", map[string]any{"notify_email": false}, john); w.Code != 200 {
		t.Fatalf("профиль: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/me/password", map[string]string{"current_password": "password123", "new_password": "another-pass-456"}, john); w.Code != 200 {
		t.Fatalf("пароль: %d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", fmt.Sprintf("/api/sites/%d", id), nil, john); w.Code != 204 {
		t.Fatalf("удаление сайта: %d %s", w.Code, w.Body)
	}

	// журнал: свежие сверху, объект и слияние правок, только успешные действия
	b := e.activity(john, "")
	// четыре неудачных входа с одного адреса слиты в одну строку; отклонённые запросы (второй сайт, выход за папку) в журнал не попали
	want := []string{"site.delete", "auth.password_change", "profile.update", "files.change", "site.create", "auth.login_failed", "auth.login", "auth.register"}
	if got := kinds(b); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("события: %v", got)
	}
	byKind := map[string]int{}
	for _, ev := range b.Events {
		byKind[ev.Kind]++
		if ev.IP != "192.0.2.1" {
			t.Errorf("%s: адрес %q", ev.Kind, ev.IP)
		}
	}
	if byKind["site.create"] != 1 || byKind["site.delete"] != 1 || byKind["auth.login"] != 1 || byKind["auth.register"] != 1 || byKind["files.change"] != 1 || byKind["profile.update"] != 1 || byKind["auth.password_change"] != 1 {
		t.Fatalf("%v", byKind)
	}
	for _, ev := range b.Events {
		switch ev.Kind {
		case "site.create", "site.delete", "files.change":
			if ev.Target != host {
				t.Errorf("%s: объект %q, ждали %q", ev.Kind, ev.Target, host)
			}
		}
		if ev.Kind == "files.change" && ev.Count != 4 {
			t.Errorf("правки файлов слиты в одну строку со счётчиком: %d", ev.Count)
		}
		if ev.Kind == "auth.login_failed" && ev.Count != 4 {
			t.Errorf("неудачные входы слиты: %d", ev.Count)
		}
		if ev.Category == "" {
			t.Errorf("%s без вида", ev.Kind)
		}
	}
	if b.Events[0].UserAgent != "" && len(b.Events[0].UserAgent) > 200 {
		t.Fatal("длина программы клиента ограничена")
	}

	// вид и страницы
	sec := e.activity(john, "?category=security")
	for _, ev := range sec.Events {
		if ev.Category != "security" {
			t.Fatalf("фильтр: %+v", ev)
		}
	}
	if len(sec.Categories) != 4 {
		t.Fatalf("виды: %v", sec.Categories)
	}
	if sites := e.activity(john, "?category=sites"); len(kinds(sites)) != 3 {
		t.Fatalf("%v", kinds(sites))
	}
	first := e.activity(john, "?limit=3")
	if len(first.Events) != 3 || first.Next == 0 {
		t.Fatalf("первая страница: %+v", first)
	}
	second := e.activity(john, fmt.Sprintf("?limit=3&before=%d", first.Next))
	if len(second.Events) == 0 || second.Events[0].ID >= first.Next {
		t.Fatalf("вторая страница: %+v", second)
	}
	if w := e.do("GET", "/api/activity?category=nope", nil, john); w.Code != 400 {
		t.Fatalf("неизвестный вид: %d", w.Code)
	}

	// чужой журнал не виден
	if m := e.activity(mary, ""); len(m.Events) != 1 || m.Events[0].Kind != "auth.register" {
		t.Fatalf("журнал другого пользователя: %v", kinds(m))
	}
}

func TestActivityClientAddressIsNotSpoofable(t *testing.T) {
	e := newEnv(t)
	e.withActivity()
	adm, _ := e.admin()
	tok := e.user(adm, "john")
	send := func(remote, forwarded string) {
		req := httptest.NewRequest("PATCH", "/api/me", strings.NewReader(`{"notify_email":true}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("User-Agent", "vh-test/1.0 (\n) "+strings.Repeat("x", 300))
		req.RemoteAddr = remote + ":5555"
		if forwarded != "" {
			req.Header.Set("X-Forwarded-For", forwarded)
		}
		w := httptest.NewRecorder()
		e.r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	// посторонний клиент напрямую подсовывает заголовок — верить ему нельзя
	send("198.51.100.20", "1.2.3.4")
	// свой nginx на этой же машине передаёт настоящий адрес клиента
	send("127.0.0.1", "203.0.113.9")
	ips := map[string]bool{}
	var agent string
	for _, ev := range e.activity(tok, "").Events {
		if ev.Kind == "profile.update" {
			ips[ev.IP] = true
			agent = ev.UserAgent
		}
	}
	if !ips["198.51.100.20"] || !ips["203.0.113.9"] || ips["1.2.3.4"] || len(ips) != 2 {
		t.Fatalf("адреса: %v", ips)
	}
	if len(agent) != 200 || !strings.HasPrefix(agent, "vh-test/1.0 ( ) xxx") {
		t.Fatalf("программа клиента обрезана до 200 знаков без переводов строк: %q", agent)
	}
}
